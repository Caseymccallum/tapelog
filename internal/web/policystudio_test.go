package web

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Caseymccallum/tapelog/internal/policy"
	"github.com/Caseymccallum/tapelog/internal/replay"
	"github.com/Caseymccallum/tapelog/internal/session"
	"github.com/Caseymccallum/tapelog/packs"
)

// studioPolicy is a fictional active policy used by these tests.
const studioPolicy = `# fictional workspace policy
version: 1
default: deny
metadata:
  owner: fictional
rules:
  - id: allow-reads
    tool: ["read_*"]
    action: allow
    reason: "reads are routine"
  - id: confirm-writes
    tool: ["write_*"]
    action: confirm
    reason: "writes need a human"
`

func writeStudioPolicy(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tapelog.yaml")
	if err := os.WriteFile(path, []byte(studioPolicy), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestStudioListsPoliciesAndNotesAMissingActiveFile(t *testing.T) {
	h := DirHandler(t.TempDir(), "")
	rw, data := requestJSON(t, h, "GET", "/api/policy/policies", nil)
	if rw.Code != 200 {
		t.Fatalf("status %d: %s", rw.Code, rw.Body.String())
	}
	var active any
	if err := json.Unmarshal(data["active"], &active); err != nil || active != nil {
		t.Fatalf("active should be null without --policy: %s", data["active"])
	}
	var packList []struct {
		ID, Kind, Name string
		Error          string
	}
	if err := json.Unmarshal(data["packs"], &packList); err != nil {
		t.Fatal(err)
	}
	if len(packList) != len(packs.All()) || len(packList) < 5 {
		t.Fatalf("packs: %d", len(packList))
	}
	for _, p := range packList {
		if p.Kind != "pack" || !strings.HasPrefix(p.ID, "pack:") || p.Error != "" {
			t.Fatalf("pack entry: %+v", p)
		}
	}
	var notes []string
	_ = json.Unmarshal(data["notes"], &notes)
	if !strings.Contains(strings.Join(notes, " "), "--policy") {
		t.Fatalf("the missing-policy note must explain --policy: %v", notes)
	}
}

func TestStudioInspectsTheActivePolicyWithoutTouchingIt(t *testing.T) {
	path := writeStudioPolicy(t)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	infoBefore, _ := os.Stat(path)
	h := DirHandlerWithPolicy(t.TempDir(), path, "")
	rw, data := requestJSON(t, h, "GET", "/api/policy/inspect?source=active", nil)
	if rw.Code != 200 {
		t.Fatalf("status %d: %s", rw.Code, rw.Body.String())
	}
	var analysis policy.Analysis
	if err := json.Unmarshal(data["analysis"], &analysis); err != nil {
		t.Fatal(err)
	}
	if len(analysis.Rules) != 2 || analysis.Rules[1].ActionLabel != "Require approval" {
		t.Fatalf("analysis: %+v", analysis.Rules)
	}
	if len(analysis.Unsupported) != 1 || !strings.Contains(analysis.Unsupported[0], "metadata") {
		t.Fatalf("unknown fields must be reported, not silently dropped: %+v", analysis.Unsupported)
	}
	var yamlOut string
	_ = json.Unmarshal(data["yaml"], &yamlOut)
	if !strings.Contains(yamlOut, "# fictional workspace policy") {
		t.Fatalf("the original source must be shown verbatim: %q", yamlOut)
	}
	after, _ := os.ReadFile(path)
	infoAfter, _ := os.Stat(path)
	if string(before) != string(after) || infoBefore.Size() != infoAfter.Size() || !infoBefore.ModTime().Equal(infoAfter.ModTime()) {
		t.Fatal("inspect must not modify the active policy file")
	}
}

func TestStudioPreviewExtendsAPolicyWithoutRewritingIt(t *testing.T) {
	path := writeStudioPolicy(t)
	before, _ := os.ReadFile(path)
	h := DirHandlerWithPolicy(t.TempDir(), path, "")
	rw, data := requestJSON(t, h, "POST", "/api/policy/preview", map[string]any{
		"base": "active",
		"rule": map[string]any{
			"tools":  []string{"files__write"},
			"action": "deny",
			"reason": "fictional proposal",
		},
	})
	if rw.Code != 200 {
		t.Fatalf("status %d: %s", rw.Code, rw.Body.String())
	}
	var candidate string
	_ = json.Unmarshal(data["candidate"], &candidate)
	for _, keep := range []string{"# fictional workspace policy", "metadata:\n  owner: fictional", "id: allow-reads", "id: confirm-writes"} {
		if !strings.Contains(candidate, keep) {
			t.Fatalf("candidate lost existing content %q:\n%s", keep, candidate)
		}
	}
	p, err := policy.Parse([]byte(candidate), "candidate")
	if err != nil {
		t.Fatalf("candidate must compile: %v\n%s", err, candidate)
	}
	if len(p.Rules) != 3 || p.Rules[0].Tool[0] != "files__write" || p.Rules[0].Action != "deny" {
		t.Fatalf("the new rule must be evaluated first: %+v", p.Rules)
	}
	var valid bool
	_ = json.Unmarshal(data["valid"], &valid)
	if !valid {
		t.Fatalf("valid: %s", data["valid"])
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("preview must not write to the active policy file")
	}
}

func TestStudioPreviewRefusesUnsafeOrUnsupportedShapes(t *testing.T) {
	inline := filepath.Join(t.TempDir(), "inline.yaml")
	if err := os.WriteFile(inline, []byte("version: 1\ndefault: deny\nrules: [{id: a, tool: x, action: allow}]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := DirHandlerWithPolicy(t.TempDir(), inline, "")
	rw, _ := requestJSON(t, h, "POST", "/api/policy/preview", map[string]any{
		"base": "active",
		"rule": map[string]any{"tools": []string{"y"}, "action": "deny"},
	})
	if rw.Code != 422 || !strings.Contains(rw.Body.String(), "inline") {
		t.Fatalf("inline rules must be refused: %d %s", rw.Code, rw.Body.String())
	}
	cases := map[string]map[string]any{
		"bad action":      {"rule": map[string]any{"tools": []string{"y"}, "action": "permit"}},
		"no tools":        {"rule": map[string]any{"action": "deny"}},
		"nested argument": {"base": "pack:starter", "rule": map[string]any{"tools": []string{"y"}, "action": "deny", "conditions": []map[string]any{{"arg": "a.b", "op": "equals", "value": "x", "kind": "string"}}}},
		"unknown policy":  {"base": "pack:does-not-exist", "rule": map[string]any{"tools": []string{"y"}, "action": "deny"}},
		"duplicate id":    {"base": "pack:starter", "rule": map[string]any{"id": "allow-reads", "tools": []string{"y"}, "action": "deny"}},
	}
	for name, body := range cases {
		rw, _ := requestJSON(t, h, "POST", "/api/policy/preview", body)
		if rw.Code != 422 {
			t.Fatalf("%s: expected 422, got %d %s", name, rw.Code, rw.Body.String())
		}
	}
	// A typo in the form is a bad request, not a policy decision.
	rw, _ = requestJSON(t, h, "POST", "/api/policy/preview", map[string]any{
		"rule": map[string]any{"tools": []string{"y"}, "action": "deny"}, "surprise": true,
	})
	if rw.Code != 400 || !strings.Contains(rw.Body.String(), "unknown field") {
		t.Fatalf("unknown fields must be refused: %d %s", rw.Code, rw.Body.String())
	}
	if rw, _ := requestJSON(t, h, "GET", "/api/policy/preview", nil); rw.Code != 405 {
		t.Fatalf("preview must be POST only, got %d", rw.Code)
	}
}

func TestStudioPreviewRefusesLimitsOnAnExtendedPolicy(t *testing.T) {
	path := writeStudioPolicy(t)
	h := DirHandlerWithPolicy(t.TempDir(), path, "")
	rw, _ := requestJSON(t, h, "POST", "/api/policy/preview", map[string]any{
		"base":   "active",
		"rule":   map[string]any{"tools": []string{"y"}, "action": "deny"},
		"limits": map[string]any{"max_calls": 10},
	})
	if rw.Code != 422 || !strings.Contains(rw.Body.String(), "limits") {
		t.Fatalf("limits on an extended policy must be refused: %d %s", rw.Code, rw.Body.String())
	}
	// A new candidate may set them, and they must compile.
	rw, data := requestJSON(t, h, "POST", "/api/policy/preview", map[string]any{
		"base":    "new",
		"default": "deny",
		"rule":    map[string]any{"tools": []string{"y"}, "action": "deny"},
		"limits":  map[string]any{"max_calls": 25, "max_per_minute": 5},
	})
	if rw.Code != 200 {
		t.Fatalf("status %d: %s", rw.Code, rw.Body.String())
	}
	var candidate string
	_ = json.Unmarshal(data["candidate"], &candidate)
	p, err := policy.Parse([]byte(candidate), "candidate")
	if err != nil {
		t.Fatalf("candidate with limits must compile: %v\n%s", err, candidate)
	}
	if p.Limits.MaxCalls != 25 || p.Limits.MaxPerMinute != 5 || p.Limits.MaxResponseBytes != 0 {
		t.Fatalf("limits: %+v\n%s", p.Limits, candidate)
	}
}

func TestStudioAPIAnswersWithAnExplicitUTF8Charset(t *testing.T) {
	h := DirHandlerWithPolicy(t.TempDir(), writeStudioPolicy(t), "")
	rw, _ := requestJSON(t, h, "GET", "/api/policy/inspect?source=active", nil)
	if got := rw.Header().Get("Content-Type"); !strings.Contains(got, "charset=utf-8") {
		t.Fatalf("inspect content type: %q", got)
	}
	rw, _ = requestJSON(t, h, "POST", "/api/policy/preview", map[string]any{"surprise": true})
	if got := rw.Header().Get("Content-Type"); !strings.Contains(got, "charset=utf-8") {
		t.Fatalf("error content type: %q", got)
	}
	// A candidate that inherits non-ASCII source must round-trip exactly, so a
	// client that ignores the charset cannot corrupt it silently.
	nonASCII := filepath.Join(t.TempDir(), "unicode.yaml")
	if err := os.WriteFile(nonASCII, []byte("# em dash — and a curly ‘quote’\nversion: 1\ndefault: deny\nrules:\n  - id: a\n    tool: t\n    action: deny\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	h = DirHandlerWithPolicy(t.TempDir(), nonASCII, "")
	rw, data := requestJSON(t, h, "POST", "/api/policy/preview", map[string]any{
		"base": "active",
		"rule": map[string]any{"tools": []string{"y"}, "action": "deny"},
	})
	if rw.Code != 200 {
		t.Fatalf("status %d: %s", rw.Code, rw.Body.String())
	}
	var candidate string
	_ = json.Unmarshal(data["candidate"], &candidate)
	if !strings.Contains(candidate, "em dash — and a curly ‘quote’") {
		t.Fatalf("non-ASCII comments must survive: %q", candidate)
	}
	if _, err := policy.Parse([]byte(candidate), "candidate"); err != nil {
		t.Fatalf("candidate with non-ASCII comments must compile: %v", err)
	}
}

// Untrusted request bodies stay bounded, and two JSON objects in one request
// are refused rather than silently parsed.
func TestStudioRejectsOversizedAndMalformedBodies(t *testing.T) {
	h := DirHandler(t.TempDir(), "")
	big := strings.Repeat("a", 40<<10)
	req := httptest.NewRequest("POST", "http://127.0.0.1:8923/api/policy/from-event",
		strings.NewReader(`{"file":"`+big+`"}`))
	req.Header.Set("Content-Type", "application/json")
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, req)
	if rw.Code != 400 || !strings.Contains(rw.Body.String(), "larger than") {
		t.Fatalf("oversized body: %d %s", rw.Code, rw.Body.String())
	}
	req = httptest.NewRequest("POST", "http://127.0.0.1:8923/api/policy/from-event",
		strings.NewReader(`{"file":"a.jsonl"}{"file":"b.jsonl"}`))
	rw = httptest.NewRecorder()
	h.ServeHTTP(rw, req)
	if rw.Code != 400 || !strings.Contains(rw.Body.String(), "one JSON object") {
		t.Fatalf("two objects: %d %s", rw.Code, rw.Body.String())
	}
}


// The Studio's what-if must agree with the authoritative engine, not a
// frontend or server-side approximation.
func TestStudioSimulationAgreesWithTheReplayEngine(t *testing.T) {
	dir := t.TempDir()
	rec := multiFixture(t, dir, "a.jsonl", "allow")
	h := DirHandler(dir, "")
	if !rec.Info.ChainOK || !rec.Info.Complete {
		t.Fatalf("fixture must be a usable recording: %+v", rec.Info)
	}
	rw, data := requestJSON(t, h, "POST", "/api/policy/preview", map[string]any{
		"base":     "new",
		"default":  "allow",
		"file":     "a.jsonl",
		"revision": rec.Info.Revision,
		"rule":     map[string]any{"tools": []string{"files__write"}, "action": "deny"},
	})
	if rw.Code != 200 {
		t.Fatalf("status %d: %s", rw.Code, rw.Body.String())
	}
	var got replay.Simulation
	if err := json.Unmarshal(data["simulation"], &got); err != nil {
		t.Fatalf("simulation missing: %v (%s)", err, data["simulation_error"])
	}
	if got.Total != 1 || got.Changed != 1 || got.NewlyDenied != 1 {
		t.Fatalf("simulation: %+v", got)
	}
	if got.Calls[0].RecordedVerdict != "allow" || got.Calls[0].CandidateVerdict != "deny" {
		t.Fatalf("call: %+v", got.Calls[0])
	}

	// Same policy, evaluated directly.
	var candidate string
	_ = json.Unmarshal(data["candidate"], &candidate)
	p, err := policy.Parse([]byte(candidate), "candidate")
	if err != nil {
		t.Fatal(err)
	}
	tape, err := workbenchTape(rec)
	if err != nil {
		t.Fatal(err)
	}
	want := replay.Simulate(tape, p, "")
	if want.Total != got.Total || want.Changed != got.Changed || want.NewlyDenied != got.NewlyDenied {
		t.Fatalf("studio %+v != engine %+v", got, want)
	}
	for i := range want.Calls {
		if want.Calls[i].CandidateRule != got.Calls[i].CandidateRule || want.Calls[i].RecordedVerdict != got.Calls[i].RecordedVerdict {
			t.Fatalf("call %d differs: %+v vs %+v", i, got.Calls[i], want.Calls[i])
		}
	}
	// A stale revision must be refused rather than silently simulated.
	rw, data = requestJSON(t, h, "POST", "/api/policy/preview", map[string]any{
		"base": "new", "default": "allow", "file": "a.jsonl", "revision": "stale",
		"rule": map[string]any{"tools": []string{"files__write"}, "action": "deny"},
	})
	if rw.Code != 200 {
		t.Fatalf("status %d: %s", rw.Code, rw.Body.String())
	}
	var stale bool
	_ = json.Unmarshal(data["simulation_stale"], &stale)
	if !stale || len(data["simulation"]) != 0 {
		t.Fatalf("a stale recording must not be simulated: stale=%v sim=%s", stale, data["simulation"])
	}
}

func TestStudioSimulateScoresAListedPolicy(t *testing.T) {
	dir := t.TempDir()
	rec := multiFixture(t, dir, "a.jsonl", "allow")
	h := DirHandler(dir, "")
	rw, data := requestJSON(t, h, "POST", "/api/policy/simulate", map[string]any{
		"source": "pack:starter", "file": "a.jsonl", "revision": rec.Info.Revision,
	})
	if rw.Code != 200 {
		t.Fatalf("status %d: %s", rw.Code, rw.Body.String())
	}
	var sim replay.Simulation
	if err := json.Unmarshal(data["simulation"], &sim); err != nil {
		t.Fatalf("simulation: %v", err)
	}
	if sim.Total != 1 {
		t.Fatalf("simulation: %+v", sim)
	}
	// A policy that is not on the list cannot be simulated by id.
	if rw, _ := requestJSON(t, h, "POST", "/api/policy/simulate", map[string]any{"source": "../etc/passwd", "file": "a.jsonl"}); rw.Code != 404 {
		t.Fatalf("unknown source must be 404, got %d", rw.Code)
	}
}

// remarshal rebuilds a decoded API payload so it can be decoded into a
// struct with the same field names.
func remarshal(t *testing.T, data map[string]json.RawMessage) []byte {
	t.Helper()
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// studioCallFixture writes one recording whose single call uses the given
// tool name and arguments. Used to prove that recorded content stays data.
func studioCallFixture(t *testing.T, dir, name, tool string, args map[string]any) *recording {
	t.Helper()
	path := filepath.Join(dir, name)
	w, err := session.NewWriter(path, name)
	if err != nil {
		t.Fatal(err)
	}
	add := func(kind session.EventType, payload any) *session.Event {
		e, err := w.Append(kind, payload)
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	add(session.EventSessionStart, map[string]any{"task": "fictional"})
	call := add(session.EventToolCall, map[string]any{"id": 1, "tool": tool, "args": args})
	add(session.EventPolicyDecision, map[string]any{"id": 1, "parent_seq": call.Seq, "verdict": "allow", "rule_id": "default", "reason": "no rule matched"})
	add(session.EventToolResult, map[string]any{"id": 1, "parent_seq": call.Seq, "result": map[string]any{"ok": true}})
	add(session.EventSessionEnd, map[string]any{"reason": "complete"})
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return decodeRecording(data, name)
}

func TestStudioFromEventProposesANarrowRuleAndLeavesTheRecordingAlone(t *testing.T) {
	dir := t.TempDir()
	rec := studioCallFixture(t, dir, "call.jsonl", "files__write", map[string]any{"path": "[REDACTED]", "mode": "rw"})
	before, err := os.ReadFile(filepath.Join(dir, "call.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	h := DirHandler(dir, "")
	var activity *activity
	for _, a := range rec.Activities {
		activity = a
	}
	if activity == nil {
		t.Fatal("fixture has no activity")
	}
	rw, data := requestJSON(t, h, "POST", "/api/policy/from-event", map[string]any{
		"file": "call.jsonl", "revision": rec.Info.Revision, "seq": activity.Seq,
	})
	if rw.Code != 200 {
		t.Fatalf("status %d: %s", rw.Code, rw.Body.String())
	}
	var got struct {
		Tool          string
		SuggestedAction string `json:"suggested_action"`
		RecordedVerdict string `json:"recorded_verdict"`
		Notes         []string
		ArgKeys       []struct {
			Key       string
			Kind      string
			Redacted  bool
			Matchable bool
		} `json:"arg_keys"`
	}
	if err := json.Unmarshal(remarshal(t, data), &got); err != nil {
		t.Fatal(err)
	}
	if got.Tool != "files__write" || got.SuggestedAction == "" || got.RecordedVerdict != "allow" {
		t.Fatalf("proposal: %+v", got)
	}
	if len(got.ArgKeys) != 2 || got.ArgKeys[0].Key != "mode" || got.ArgKeys[1].Key != "path" || !got.ArgKeys[1].Redacted || !got.ArgKeys[1].Matchable {
		t.Fatalf("argument keys: %+v", got.ArgKeys)
	}
	if !strings.Contains(strings.Join(got.Notes, " "), "redacted") {
		t.Fatalf("redaction must be disclosed: %v", got.Notes)
	}
	if !strings.Contains(strings.Join(got.Notes, " "), "single recorded call") {
		t.Fatalf("the one-event scope must be disclosed: %v", got.Notes)
	}
	after, _ := os.ReadFile(filepath.Join(dir, "call.jsonl"))
	if string(before) != string(after) {
		t.Fatal("from-event must not modify the recording")
	}
	// A stale revision is refused.
	rw, _ = requestJSON(t, h, "POST", "/api/policy/from-event", map[string]any{
		"file": "call.jsonl", "revision": "stale", "seq": activity.Seq,
	})
	if rw.Code != 409 {
		t.Fatalf("stale revision must be 409, got %d", rw.Code)
	}
	// An unknown event is refused.
	rw, _ = requestJSON(t, h, "POST", "/api/policy/from-event", map[string]any{"file": "call.jsonl", "seq": 999})
	if rw.Code != 404 {
		t.Fatalf("unknown event must be 404, got %d", rw.Code)
	}
}

func TestStudioFromEventRefusesUnsafeRecordedToolNames(t *testing.T) {
	// Wildcards would widen the rule beyond the recorded call, and control
	// characters would make the generated YAML ambiguous.
	for _, tool := range []string{"files__*", "files__w?ite", "a\nb", "tab\tsep"} {
		dir := t.TempDir()
		rec := studioCallFixture(t, dir, "call.jsonl", tool, map[string]any{})
		h := DirHandler(dir, "")
		var seq uint64
		for _, a := range rec.Activities {
			seq = a.Seq
		}
		rw, _ := requestJSON(t, h, "POST", "/api/policy/from-event", map[string]any{"file": "call.jsonl", "seq": seq})
		if rw.Code != 422 {
			t.Fatalf("%q must be refused, got %d %s", tool, rw.Code, rw.Body.String())
		}
	}
}



