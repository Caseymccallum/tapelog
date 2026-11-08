package web

// Behaviour-intelligence tests (M6). Every fixture is synthetic and every
// recorded value is fictional: these tests are about the semantics of the
// detectors, not about any real session.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Caseymccallum/tapelog/internal/replay"
	"github.com/Caseymccallum/tapelog/internal/scenario"
	"github.com/Caseymccallum/tapelog/internal/session"
)

const secretArg = "FICTIONAL-SECRET-ARG"
const secretResult = "FICTIONAL-SECRET-RESULT"

// bc is one recorded call in a synthetic behaviour fixture.
type bc struct {
	tool    string
	args    map[string]any
	verdict string // "" = no decision recorded
	rule    string
	result  any // nil = no recorded result (denied, unanswered or truncated)
	isError bool
	noTS    bool // drop the timestamp entirely
}

// writeBehaviourFile builds a valid, hash-chained session log by hand so a test
// can produce shapes the writer never would (missing timestamps, truncated
// recordings, several decisions per call).
func writeBehaviourFile(t testing.TB, dir, name string, calls []bc) string {
	t.Helper()
	return writeBehaviourFileOpts(t, dir, name, calls, true)
}

// writeBehaviourFileOpts writes a fixture and can omit the session/end event.
func writeBehaviourFileOpts(t testing.TB, dir, name string, calls []bc, complete bool) string {
	t.Helper()
	path := filepath.Join(dir, name)
	events := behaviourEvents(t, calls, complete)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	writeChain(t, f, name, events)
	return path
}

func behaviourEvents(t testing.TB, calls []bc, complete bool) []session.Event {
	t.Helper()
	start := time.Date(2026, 2, 1, 9, 0, 0, 0, time.UTC)
	var events []session.Event
	payload := func(v any) json.RawMessage {
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	at := func(i int) string {
		if i < 0 {
			return start.Format(time.RFC3339Nano)
		}
		return start.Add(time.Duration(i) * time.Second).Format(time.RFC3339Nano)
	}
	events = append(events, session.Event{Type: session.EventSessionStart, TS: at(-1),
		Payload: payload(map[string]any{"harness": "fictional", "task": "synthetic behaviour"})})
	events = append(events, session.Event{Type: session.EventToolsList, TS: at(0),
		Payload: payload(map[string]any{"tools": []any{}})})
	for i, c := range calls {
		id := i + 1
		ts := at(i + 1)
		if c.noTS {
			ts = ""
		}
		args := map[string]any{"target": secretArg}
		for k, v := range c.args {
			args[k] = v
		}
		events = append(events, session.Event{Type: session.EventToolCall, TS: ts,
			Payload: payload(map[string]any{"id": id, "tool": c.tool, "args": args})})
		// writeChain assigns seq = position+1, so the call's seq is the current
		// event count and both dependants can point at it directly.
		callSeq := uint64(len(events))
		if c.verdict != "" {
			events = append(events, session.Event{Type: session.EventPolicyDecision, TS: ts,
				Payload: payload(map[string]any{
					"id": id, "parent_seq": callSeq, "verdict": c.verdict,
					"rule_id": c.rule, "reason": "fictional recorded explanation",
				})})
		}
		if c.result != nil {
			events = append(events, session.Event{Type: session.EventToolResult, TS: ts,
				Payload: payload(map[string]any{
					"id": id, "parent_seq": callSeq,
					"is_error": c.isError, "result": c.result,
				})})
		}
	}
	if complete {
		events = append(events, session.Event{Type: session.EventSessionEnd, TS: at(len(calls) + 1),
			Payload: payload(map[string]any{"reason": "synthetic complete"})})
	}
	return events
}

// writeChain assigns seq, prev_hash and hash so session.Verify accepts the file.
func writeChain(t testing.TB, f *os.File, sessionID string, events []session.Event) {
	t.Helper()
	prev := ""
	for i := range events {
		e := events[i]
		e.Version = session.SchemaVersion
		e.Seq = uint64(i + 1)
		e.SessionID = sessionID
		e.PrevHash = prev
		h, err := e.ComputeHash()
		if err != nil {
			t.Fatal(err)
		}
		e.Hash = h
		prev = h
		line, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write(append(line, '\n')); err != nil {
			t.Fatal(err)
		}
	}
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// behaviourRecording writes a fixture and returns the decoded recording the
// workbench would see.
func behaviourRecording(t testing.TB, dir, name string, calls []bc) *recording {
	t.Helper()
	writeBehaviourFile(t, dir, name, calls)
	return decodeFile(t, dir, name)
}

// decodeFile decodes a fixture that already exists on disk.
func decodeFile(t testing.TB, dir, name string) *recording {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return decodeRecording(data, name)
}

// writeUndatedFile writes a fixture whose events carry no timestamp at all, as
// a very old or hand-edited recording might.
func writeUndatedFile(t testing.TB, dir, name string, calls []bc, complete bool) string {
	t.Helper()
	path := filepath.Join(dir, name)
	events := behaviourEvents(t, calls, complete)
	for i := range events {
		events[i].TS = ""
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	writeChain(t, f, name, events)
	return path
}

// behaviourDir writes several fixtures into one directory and returns a
// directory-mode server over it.
func behaviourServer(t testing.TB, files map[string][]bc) (*server, string) {
	t.Helper()
	dir := t.TempDir()
	for name, calls := range files {
		writeBehaviourFile(t, dir, name, calls)
	}
	return &server{dir: dir}, dir
}

func findingKinds(fs []repeatFinding) []string {
	out := make([]string, 0, len(fs))
	for _, f := range fs {
		out = append(out, f.Kind)
	}
	return out
}

func mustJSON(t testing.TB, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func errorResult(message string) map[string]any {
	return map[string]any{"error": map[string]any{"code": -32000, "message": message}}
}

// loadTape is the engine's own parse of a fixture, used to pin cross-package
// agreement rather than re-deriving it.
func loadTape(t testing.TB, dir, name string) *replay.Tape {
	t.Helper()
	tape, err := replay.Load(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return tape
}

// --- Phase 2: deterministic detectors -------------------------------------

// TestBehaviourRetryLoopNeedsIdenticalResults is the central calibration test.
// Repeated identical arguments alone are not a loop: the same arguments with
// differing error detail stay "repeated failure", because a bounded retry
// against a flaky server looks exactly like that.
func TestBehaviourRetryLoopNeedsIdenticalResults(t *testing.T) {
	dir := t.TempDir()
	identical := []bc{
		{tool: "db__query", args: map[string]any{"q": "select 1"}, verdict: "allow", result: errorResult("connection refused by peer"), isError: true},
		{tool: "db__query", args: map[string]any{"q": "select 1"}, verdict: "allow", result: errorResult("connection refused by peer"), isError: true},
		{tool: "db__query", args: map[string]any{"q": "select 1"}, verdict: "allow", result: errorResult("connection refused by peer"), isError: true},
	}
	findings := analyseRecording(behaviourRecording(t, dir, "identical.jsonl", identical)).Repeats
	if len(findings) != 1 || findings[0].Kind != kindPossibleRetryLoop {
		t.Fatalf("identical error results should be a possible retry loop, got %s", describe(findings))
	}
	if findings[0].Confidence != "possible" {
		t.Errorf("a retry loop is an interpretation, confidence = %q", findings[0].Confidence)
	}
	if !strings.Contains(findings[0].Interpretation, "interpretation") {
		t.Errorf("interpretation must say it is not proof: %q", findings[0].Interpretation)
	}
	if findings[0].Count != 3 || len(findings[0].Seqs) != 3 {
		t.Errorf("finding must carry all three calls: %+v", findings[0])
	}

	varying := []bc{
		{tool: "db__query", args: map[string]any{"q": "select 1"}, verdict: "allow", result: errorResult("timeout after 3s"), isError: true},
		{tool: "db__query", args: map[string]any{"q": "select 1"}, verdict: "allow", result: errorResult("timeout after 7s"), isError: true},
		{tool: "db__query", args: map[string]any{"q": "select 1"}, verdict: "allow", result: errorResult("timeout after 11s"), isError: true},
	}
	findings = analyseRecording(behaviourRecording(t, dir, "varying.jsonl", varying)).Repeats
	if len(findings) != 1 || findings[0].Kind != kindRepeatedFailure {
		t.Fatalf("differing error results must not be called a loop, got %s", describe(findings))
	}
	if findings[0].IdenticalResults {
		t.Error("results differ, so identical_results must be false")
	}
}

// TestBehaviourLegitimatePollingReadsAsActivityNotALoop is the requested
// counter-example: a tool that repeats by design must not be labelled a loop.
func TestBehaviourLegitimatePollingReadsAsActivityNotALoop(t *testing.T) {
	dir := t.TempDir()
	var calls []bc
	for i := 0; i < 6; i++ {
		calls = append(calls, bc{
			tool: "jobs__status", args: map[string]any{"id": "job-1"}, verdict: "allow",
			result: map[string]any{"state": fmt.Sprintf("running-%d", i)},
		})
	}
	findings := analyseRecording(behaviourRecording(t, dir, "polling.jsonl", calls)).Repeats
	if len(findings) != 1 || findings[0].Kind != kindRepeatedActivity {
		t.Fatalf("polling must be reported as repeated activity, got %s", describe(findings))
	}
	if attention(findings[0].Kind) {
		t.Error("plain repetition must not be counted as attention-worthy")
	}
	if !strings.Contains(findings[0].Interpretation, "polling") {
		t.Errorf("interpretation must name the legitimate case: %q", findings[0].Interpretation)
	}
	if findings[0].MedianGapMS == nil || *findings[0].MedianGapMS <= 0 {
		t.Error("timestamps exist, so a cadence should be reported")
	}
}

// TestBehaviourRepeatedDenialAndInsufficientEvidence covers the remaining
// classifications, including the one that says there is nothing to conclude.
func TestBehaviourRepeatedDenialAndInsufficientEvidence(t *testing.T) {
	dir := t.TempDir()
	denied := []bc{
		{tool: "git__push", args: map[string]any{"force": true}, verdict: "deny", rule: "no-force-push"},
		{tool: "git__push", args: map[string]any{"force": true}, verdict: "deny", rule: "no-force-push"},
		{tool: "git__push", args: map[string]any{"force": true}, verdict: "deny", rule: "no-force-push"},
	}
	findings := analyseRecording(behaviourRecording(t, dir, "denied.jsonl", denied)).Repeats
	if len(findings) != 1 || findings[0].Kind != kindRepeatedDenial {
		t.Fatalf("three identical denials must be a repeated denial, got %s", describe(findings))
	}
	if !attention(findings[0].Kind) {
		t.Error("repeated denials are worth attention")
	}

	findings = analyseRecording(behaviourRecording(t, dir, "two.jsonl", denied[:2])).Repeats
	if len(findings) != 1 || findings[0].Kind != kindInsufficient {
		t.Fatalf("two identical denials must be insufficient evidence, got %s", describe(findings))
	}

	silent := []bc{
		{tool: "queue__pop", args: map[string]any{"lane": "a"}},
		{tool: "queue__pop", args: map[string]any{"lane": "a"}},
		{tool: "queue__pop", args: map[string]any{"lane": "a"}},
	}
	findings = analyseRecording(behaviourRecording(t, dir, "silent.jsonl", silent)).Repeats
	if len(findings) != 1 || findings[0].Kind != kindInsufficient {
		t.Fatalf("unanswered identical calls must be insufficient evidence, got %s", describe(findings))
	}
	if !strings.Contains(findings[0].Interpretation, "cannot show whether") {
		t.Errorf("unexpected interpretation: %q", findings[0].Interpretation)
	}
}

// TestBehaviourAlternatingSequenceDetected pins the block detector: A,B,A,B,A,B
// is a shape neither the single-call group detector nor a consecutive run can
// express.
func TestBehaviourAlternatingSequenceDetected(t *testing.T) {
	dir := t.TempDir()
	var calls []bc
	for i := 0; i < 3; i++ {
		calls = append(calls,
			bc{tool: "files__read", args: map[string]any{"path": "/tmp/a"}, verdict: "allow", result: map[string]any{"body": "a"}},
			bc{tool: "files__write", args: map[string]any{"path": "/tmp/a"}, verdict: "allow", result: map[string]any{"ok": true}},
		)
	}
	findings := analyseRecording(behaviourRecording(t, dir, "alternating.jsonl", calls)).Repeats
	var alternating *repeatFinding
	for i := range findings {
		if findings[i].Kind == kindAlternating {
			alternating = &findings[i]
		}
	}
	if alternating == nil {
		t.Fatalf("an A,B,A,B,A,B pattern must be reported, got %s", describe(findings))
	}
	if alternating.BlockLength != 2 || alternating.Repeats != 3 || alternating.Count != 6 {
		t.Errorf("unexpected shape: %+v", *alternating)
	}
	if len(alternating.Tools) != 2 {
		t.Errorf("both tools must be named: %+v", alternating.Tools)
	}
	for _, f := range findings {
		if f.Kind == kindRepeatedActivity && f.Count == 3 {
			t.Errorf("calls already explained by the block finding were reported again: %+v", f)
		}
	}
}

// TestBehaviourDoesNotReportOrdinaryUse keeps the detectors quiet on a session
// that simply uses tools once each.
func TestBehaviourDoesNotReportOrdinaryUse(t *testing.T) {
	dir := t.TempDir()
	calls := []bc{
		{tool: "files__read", args: map[string]any{"path": "/tmp/a"}, verdict: "allow", result: map[string]any{"ok": true}},
		{tool: "files__read", args: map[string]any{"path": "/tmp/b"}, verdict: "allow", result: map[string]any{"ok": true}},
		{tool: "git__status", args: map[string]any{}, verdict: "allow", result: map[string]any{"clean": true}},
	}
	if findings := analyseRecording(behaviourRecording(t, dir, "single.jsonl", calls)).Repeats; len(findings) != 0 {
		t.Fatalf("distinct calls must produce no findings, got %s", describe(findings))
	}
}

func describe(fs []repeatFinding) string {
	var b strings.Builder
	for _, f := range fs {
		fmt.Fprintf(&b, "%s %q count=%d confidence=%s; ", f.Kind, f.Tool, f.Count, f.Confidence)
	}
	return b.String()
}

// --- Phase 3: recurring failure groups ------------------------------------

// TestBehaviourFailureSignaturesGroupRealProblemsOnly checks both directions:
// the same problem groups across volatile detail, and unrelated problems that
// merely share a word stay apart.
func TestBehaviourFailureSignaturesGroupRealProblemsOnly(t *testing.T) {
	same := []string{
		`{"error":{"code":-32000,"message":"relation \"users\" does not exist"}}`,
		`{"error":{"code":-32000,"message":"relation \"orders\" does not exist"}}`,
		`{"error":{"code":-32000,"message":"relation \"users\" does not exist (line 3)"}}`,
	}
	first := signatureFor("db__query", "pg", json.RawMessage(same[0]))
	for _, body := range same[1:] {
		got := signatureFor("db__query", "pg", json.RawMessage(body))
		if got.ID != first.ID {
			t.Errorf("same problem must share a group:\n %q\n %q", body, first.Label)
		}
	}
	if first.Code != "-32000" {
		t.Errorf("recorded error code must be part of the signature, got %q", first.Code)
	}

	// A different tool, a different code and a different message are all
	// separate groups even though the word "exist" is shared.
	other := []struct{ tool, server, body string }{
		{"db__query", "pg", `{"error":{"code":-32000,"message":"role \"app\" does not exist"}}`},
		{"db__query", "pg", `{"error":{"code":-32602,"message":"relation \"users\" does not exist"}}`},
		{"files__read", "pg", `{"error":{"code":-32000,"message":"relation \"users\" does not exist"}}`},
		{"db__query", "other-server", `{"error":{"code":-32000,"message":"relation \"users\" does not exist"}}`},
	}
	for _, o := range other {
		if signatureFor(o.tool, o.server, json.RawMessage(o.body)).ID == first.ID {
			t.Errorf("%s/%s must not join the group: %q", o.tool, o.server, o.body)
		}
	}

	// Volatile detail inside a message must not create a new group per value.
	for _, msg := range []string{"timeout after 3s", "timeout after 11s", "timeout after 42.5s"} {
		body := fmt.Sprintf(`{"message":%q}`, msg)
		if signatureFor("net__get", "", json.RawMessage(body)).ID != signatureFor("net__get", "", json.RawMessage(`{"message":"timeout after 1s"}`)).ID {
			t.Errorf("changing number must not split the group: %q", normaliseMessage(msg))
		}
	}
	if normaliseMessage("timeout after 3s") != "timeout after <n>s" {
		t.Errorf("numbers are replaced with a visible placeholder, got %q", normaliseMessage("timeout after 3s"))
	}
}

// TestBehaviourSignatureLabelsNeverCarryRecordedValues is the privacy check:
// arguments are never copied into the analysis, and a label only carries the
// normalised shape of a recorded message.
func TestBehaviourSignatureLabelsNeverCarryRecordedValues(t *testing.T) {
	dir := t.TempDir()
	message := func(n int) map[string]any {
		return map[string]any{"error": map[string]any{
			"code": 403, "message": fmt.Sprintf("denied for token \"%s\" at 12:0%d", secretResult, n),
		}}
	}
	calls := []bc{
		{tool: "vault__read", args: map[string]any{"key": secretArg}, verdict: "allow", result: message(1), isError: true},
		{tool: "vault__read", args: map[string]any{"key": secretArg}, verdict: "allow", result: message(2), isError: true},
	}
	a := analyseRecording(behaviourRecording(t, dir, "secret.jsonl", calls))
	body := mustJSON(t, a.Sigs) + mustJSON(t, a.Repeats)
	if strings.Contains(body, secretArg) {
		t.Error("recorded argument values leaked into the analysis")
	}
	if strings.Contains(body, secretResult) {
		t.Errorf("the quoted value should have been normalised out of the label: %s", body)
	}
	if len(a.Sigs) != 1 {
		t.Fatalf("both failures are the same problem, got %d groups", len(a.Sigs))
	}
	for _, sig := range a.Sigs {
		if !strings.Contains(sig.Label, "<text>") || !strings.Contains(sig.Label, "<n>") {
			t.Errorf("label should show its placeholders: %q", sig.Label)
		}
	}
}

// TestBehaviourSignatureFragmentIsALiteralSubstring proves the fragment handed
// to `tapelog test` really appears in the recorded result, because the engine
// does a literal substring match.
func TestBehaviourSignatureFragmentIsALiteralSubstring(t *testing.T) {
	cases := []string{
		"relation \"users\" does not exist",
		"permission denied for /var/lib/app/data.db",
		"connection refused by 10.0.0.4:5432",
		"unexpected token at position 42",
		"rate limit exceeded for https://api.example.test/v1/items",
		"Error: ENOENT: no such file or directory",
	}
	for _, message := range cases {
		fragment := signatureFragment(message)
		if fragment == "" {
			continue // too short before the first volatile token: refused on purpose
		}
		if !strings.Contains(message, fragment) {
			t.Errorf("fragment %q is not a literal substring of %q", fragment, message)
		}
	}
	if fragment := signatureFragment("id 12345"); fragment != "" {
		t.Errorf("a message that is only a value must not produce a fragment, got %q", fragment)
	}
}

// --- Phase 1/4: overview, counts and rates --------------------------------

// TestBehaviourOverviewKeepsCallsDecisionsAndEventsApart is the honesty test for
// the headline numbers: a decision is not a call and a call is not an event.
func TestBehaviourOverviewKeepsCallsDecisionsAndEventsApart(t *testing.T) {
	srv, _ := behaviourServer(t, map[string][]bc{
		"one.jsonl": {
			{tool: "files__read", args: map[string]any{"path": "/tmp/a"}, verdict: "allow", result: map[string]any{"ok": true}},
			{tool: "files__read", args: map[string]any{"path": "/tmp/b"}, verdict: "allow", result: errorResult("no such file or directory"), isError: true},
			{tool: "git__push", args: map[string]any{"force": true}, verdict: "deny", rule: "no-force-push"},
		},
		"two.jsonl": {
			{tool: "files__read", args: map[string]any{"path": "/tmp/a"}, verdict: "allow", result: map[string]any{"ok": true}},
			{tool: "queue__pop", args: map[string]any{"lane": "a"}},
		},
	})
	rw, _ := requestJSON(t, newMux(srv), "GET", "/api/behaviour", nil)
	if rw.Code != 200 {
		t.Fatalf("status %d: %s", rw.Code, rw.Body.String())
	}
	var ov behaviourOverview
	if err := json.Unmarshal(rw.Body.Bytes(), &ov); err != nil {
		t.Fatal(err)
	}
	c := ov.Counts
	if c.Sessions != 2 || c.Calls != 5 {
		t.Errorf("sessions/calls = %d/%d, want 2/5", c.Sessions, c.Calls)
	}
	if c.Results != 3 || c.Failures != 1 || c.Successes != 2 {
		t.Errorf("results=%d failures=%d successes=%d, want 3/1/2", c.Results, c.Failures, c.Successes)
	}
	if c.Denies != 1 || c.DenyDecisions != 1 {
		t.Errorf("denies=%d deny_decisions=%d, want 1/1", c.Denies, c.DenyDecisions)
	}
	if c.Unanswered != 2 {
		t.Errorf("unanswered = %d, want 2 (the denied call and the silent one)", c.Unanswered)
	}
	if c.Discovery != 2 {
		t.Errorf("discovery = %d, want one tools/list per recording", c.Discovery)
	}
	if c.SignatureGroups != 1 {
		t.Errorf("signature groups = %d, want 1", c.SignatureGroups)
	}
	// Calls must not be implied to equal events: discovery and boundaries exist.
	if c.Events <= c.Calls {
		t.Errorf("events (%d) should exceed calls (%d)", c.Events, c.Calls)
	}
	if len(ov.Definitions) == 0 {
		t.Error("the response must explain its own terms")
	}
	if len(ov.Tools) == 0 || ov.Tools[0].Tool != "files__read" {
		t.Errorf("most-used tool should lead the list: %+v", ov.Tools)
	}
	if ov.Tools[0].ErrorRate == nil || *ov.Tools[0].ErrorRate < 0.33 || *ov.Tools[0].ErrorRate > 0.34 {
		t.Errorf("files__read error rate = %v, want 1/3", ov.Tools[0].ErrorRate)
	}
	if len(ov.Denied) != 1 || ov.Denied[0].Rule != "no-force-push" {
		t.Errorf("denied groups: %+v", ov.Denied)
	}
	if len(ov.Failures) != 1 || ov.Failures[0].Occurrences != 1 || ov.Failures[0].Sessions != 1 {
		t.Errorf("failure groups: %+v", ov.Failures)
	}
	if len(ov.Trend) != 2 {
		t.Errorf("trend rows = %d, want one per recording", len(ov.Trend))
	}
	body := rw.Body.String()
	if strings.Contains(body, secretArg) {
		t.Error("recorded argument values must never reach this API")
	}
}

// TestBehaviourRatesAreUndefinedWithoutADenominator makes sure an empty
// recording is not reported as 0% errors.
func TestBehaviourRatesAreUndefinedWithoutADenominator(t *testing.T) {
	srv, _ := behaviourServer(t, map[string][]bc{
		"denied-five.jsonl": {
			{tool: "git__push", args: map[string]any{"force": true}, verdict: "deny", rule: "no-force-push"},
			{tool: "git__push", args: map[string]any{"force": true}, verdict: "deny", rule: "no-force-push"},
			{tool: "git__push", args: map[string]any{"force": true}, verdict: "deny", rule: "no-force-push"},
		},
	})
	rw, _ := requestJSON(t, newMux(srv), "GET", "/api/behaviour", nil)
	var ov behaviourOverview
	if err := json.Unmarshal(rw.Body.Bytes(), &ov); err != nil {
		t.Fatal(err)
	}
	if len(ov.Trend) != 1 {
		t.Fatalf("trend rows = %d", len(ov.Trend))
	}
	if ov.Trend[0].ErrorRate != nil {
		t.Errorf("no results means no error rate, got %v", *ov.Trend[0].ErrorRate)
	}
	if ov.Trend[0].DenialRate == nil || *ov.Trend[0].DenialRate != 1 {
		t.Errorf("denial rate = %v, want 1", ov.Trend[0].DenialRate)
	}
	joined := strings.Join(ov.Notes, " ")
	if !strings.Contains(joined, "undefined") {
		t.Errorf("notes must say why the rate is missing: %q", joined)
	}
}

// --- Phase 9: incomplete, undated and changing recordings ------------------

// TestBehaviourHandlesIncompleteAndUndatedRecordings documents what the
// interface says when the evidence is thin, and that nothing crashes.
func TestBehaviourHandlesIncompleteAndUndatedRecordings(t *testing.T) {
	dir := t.TempDir()
	writeUndatedFile(t, dir, "truncated.jsonl", []bc{
		{tool: "files__read", args: map[string]any{"path": "/tmp/a"}, verdict: "allow", result: map[string]any{"ok": true}, noTS: true},
		{tool: "files__read", args: map[string]any{"path": "/tmp/b"}, verdict: "allow", result: map[string]any{"ok": true}, noTS: true},
	}, false)
	srv := &server{dir: dir}

	rw, _ := requestJSON(t, newMux(srv), "GET", "/api/behaviour", nil)
	if rw.Code != 200 {
		t.Fatalf("status %d: %s", rw.Code, rw.Body.String())
	}
	var ov behaviourOverview
	if err := json.Unmarshal(rw.Body.Bytes(), &ov); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(ov.Notes, " ")
	if !strings.Contains(joined, "session/end") {
		t.Errorf("a missing end must be stated: %q", joined)
	}
	if ov.Counts.Calls != 2 {
		t.Errorf("undated calls are still calls, got %d", ov.Counts.Calls)
	}

	// A date window cannot include a recording whose time is unknown, and the
	// response must say so rather than silently dropping it.
	rw, _ = requestJSON(t, newMux(srv), "GET", "/api/behaviour?from=2026-01-01&to=2026-12-31", nil)
	if err := json.Unmarshal(rw.Body.Bytes(), &ov); err != nil {
		t.Fatal(err)
	}
	if ov.Counts.Sessions != 0 || ov.Library.Undated != 1 {
		t.Errorf("sessions=%d undated=%d, want 0/1", ov.Counts.Sessions, ov.Library.Undated)
	}
	if !strings.Contains(strings.Join(ov.Notes, " "), "no usable timestamp") {
		t.Errorf("undated exclusion must be explained: %q", ov.Notes)
	}
}

// TestBehaviourCacheFollowsTheFile keeps the index honest when a recording is
// still being written.
func TestBehaviourCacheFollowsTheFile(t *testing.T) {
	dir := t.TempDir()
	srv := &server{dir: dir}
	writeBehaviourFile(t, dir, "live.jsonl", []bc{
		{tool: "files__read", args: map[string]any{"path": "/tmp/a"}, verdict: "allow", result: map[string]any{"ok": true}},
	})
	first, err := srv.buildBehaviourIndex(behaviourFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if first.Analysed != 1 {
		t.Fatalf("analysed = %d, want 1", first.Analysed)
	}
	second, err := srv.buildBehaviourIndex(behaviourFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if second.Cached != 1 {
		t.Errorf("a repeat request should reuse the cached snapshot, cached = %d", second.Cached)
	}
	// Replace the file with a longer one; the size changes so the cache misses.
	writeBehaviourFile(t, dir, "live.jsonl", []bc{
		{tool: "files__read", args: map[string]any{"path": "/tmp/a"}, verdict: "allow", result: map[string]any{"ok": true}},
		{tool: "files__read", args: map[string]any{"path": "/tmp/b"}, verdict: "allow", result: map[string]any{"ok": true}},
	})
	third, err := srv.buildBehaviourIndex(behaviourFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if third.Cached != 0 {
		t.Error("a changed recording must be re-analysed")
	}
	if len(third.Sessions) != 1 || len(third.Sessions[0].Calls) != 2 {
		t.Errorf("the new call should be visible: %+v", third.Sessions)
	}
}

// TestBehaviourIndexIsBounded proves the file cap is enforced and reported.
func TestBehaviourIndexIsBounded(t *testing.T) {
	dir := t.TempDir()
	payload := []bc{{tool: "files__read", args: map[string]any{"path": "/tmp/a"}, verdict: "allow", result: map[string]any{"ok": true}}}
	for i := 0; i < maxAnalysisFiles+3; i++ {
		writeBehaviourFile(t, dir, fmt.Sprintf("s%03d.jsonl", i), payload)
	}
	srv := &server{dir: dir}
	idx, err := srv.buildBehaviourIndex(behaviourFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if idx.Analysed != maxAnalysisFiles || !idx.Truncated {
		t.Errorf("analysed = %d truncated = %v, want %d/true", idx.Analysed, idx.Truncated, maxAnalysisFiles)
	}
	if len(idx.Skipped) == 0 {
		t.Error("skipped recordings must be reported")
	}
	ov := aggregateOverview(idx)
	if !strings.Contains(strings.Join(ov.Notes, " "), "at most") {
		t.Errorf("the file cap must be explained: %q", ov.Notes)
	}
}

// TestBehaviourArgumentIdentityMatchesTheEngine pins the cross-package
// agreement: "identical arguments" must mean what `tapelog test` means.
func TestBehaviourArgumentIdentityMatchesTheEngine(t *testing.T) {
	dir := t.TempDir()
	name := "fingerprint.jsonl"
	args := map[string]any{"b": 2, "a": 1, "nested": map[string]any{"y": true, "x": "z"}}
	writeBehaviourFile(t, dir, name, []bc{{tool: "files__read", args: args, verdict: "allow", result: map[string]any{"ok": true}}})
	a := analyseRecording(decodeFile(t, dir, name))
	tape := loadTape(t, dir, name)
	if len(a.Calls) != 1 || len(tape.Interactions) != 1 {
		t.Fatalf("calls=%d interactions=%d", len(a.Calls), len(tape.Interactions))
	}
	if !strings.HasPrefix(tape.Interactions[0].ArgsHash, a.Calls[0].ArgsKey) {
		t.Errorf("analysis digest %q is not the engine's argument hash %q", a.Calls[0].ArgsKey, tape.Interactions[0].ArgsHash)
	}
}

// --- Phase 5: reference comparison ----------------------------------------

// TestBehaviourDeltaNamesDifferencesWithoutJudgingThem checks the reference
// comparison reports what is new, what is gone, and says it is not a verdict.
func TestBehaviourDeltaNamesDifferencesWithoutJudgingThem(t *testing.T) {
	dir := t.TempDir()
	writeBehaviourFile(t, dir, "reference.jsonl", []bc{
		{tool: "files__read", args: map[string]any{"path": "/tmp/a"}, verdict: "allow", result: map[string]any{"ok": true}},
		{tool: "git__push", args: map[string]any{"force": false}, verdict: "allow", result: map[string]any{"ok": true}},
	})
	writeBehaviourFile(t, dir, "current.jsonl", []bc{
		{tool: "files__read", args: map[string]any{"path": "/tmp/a"}, verdict: "allow", result: errorResult("no such file or directory"), isError: true},
		{tool: "git__push", args: map[string]any{"force": true}, verdict: "deny", rule: "no-force-push"},
		{tool: "git__push", args: map[string]any{"force": true}, verdict: "deny", rule: "no-force-push"},
		{tool: "git__push", args: map[string]any{"force": true}, verdict: "deny", rule: "no-force-push"},
	})
	srv := &server{dir: dir}
	rw, _ := requestJSON(t, newMux(srv), "GET", "/api/behaviour/delta?a=reference.jsonl&b=current.jsonl", nil)
	if rw.Code != 200 {
		t.Fatalf("status %d: %s", rw.Code, rw.Body.String())
	}
	var d behaviourDelta
	if err := json.Unmarshal(rw.Body.Bytes(), &d); err != nil {
		t.Fatal(err)
	}
	if len(d.NewFailures) != 1 || d.NewFailures[0].Tool != "files__read" || d.NewFailures[0].Current != 1 {
		t.Errorf("the new failure must be named: %+v", d.NewFailures)
	}
	if len(d.NewDenials) != 1 || d.NewDenials[0].Rule != "no-force-push" || d.NewDenials[0].Calls != 3 {
		t.Errorf("the new denial must be named: %+v", d.NewDenials)
	}
	if len(d.NewRepeats) != 1 || d.NewRepeats[0].Kind != kindRepeatedDenial {
		t.Errorf("the new repeated denial must be named: %+v", d.NewRepeats)
	}
	if len(d.NewRepeats) == 1 && d.NewRepeats[0].Current != 1 {
		t.Errorf("the current side must carry its own count, got %d", d.NewRepeats[0].Current)
	}
	if len(d.Rates) != 4 {
		t.Fatalf("rates = %d, want four metrics", len(d.Rates))
	}
	for _, r := range d.Rates {
		if r.Metric == "" {
			t.Error("a rate must be labelled")
		}
	}
	if d.Rates[0].Reference == nil || *d.Rates[0].Reference != 0 {
		t.Errorf("reference error rate = %v, want 0 of 2 results", d.Rates[0].Reference)
	}
	if d.Rates[0].Current == nil || *d.Rates[0].Current != 1 {
		t.Errorf("current error rate = %v, want 1 of 1 result", d.Rates[0].Current)
	}
	if d.Rates[0].ReferenceN != 2 || d.Rates[0].CurrentN != 1 {
		t.Errorf("denominators = %d/%d, want 2/1", d.Rates[0].ReferenceN, d.Rates[0].CurrentN)
	}
	// A per-minute rate must not be formatted as a proportion by the interface.
	for _, r := range d.Rates {
		if r.Unit != "ratio" && r.Unit != "per_minute" {
			t.Errorf("%s: unit %q is not one the interface knows how to format", r.Metric, r.Unit)
		}
	}
	if d.Rates[3].Unit != "per_minute" {
		t.Errorf("calls per minute must be a rate, not a ratio: %q", d.Rates[3].Unit)
	}
	if !strings.Contains(strings.Join(d.Caveats, " "), "not a regression") {
		t.Errorf("the comparison must refuse to call a difference a regression: %q", d.Caveats)
	}
	if d.Reference.File != "reference.jsonl" || d.Current.File != "current.jsonl" {
		t.Errorf("sides are mixed up: %q vs %q", d.Reference.File, d.Current.File)
	}
}

// TestBehaviourDeltaRefusesIncompleteRequests keeps the comparison honest about
// what it can and cannot do.
func TestBehaviourDeltaRefusesIncompleteRequests(t *testing.T) {
	dir := t.TempDir()
	writeBehaviourFile(t, dir, "one.jsonl", []bc{
		{tool: "files__read", args: map[string]any{"path": "/tmp/a"}, verdict: "allow", result: map[string]any{"ok": true}},
	})
	srv := &server{dir: dir}
	cases := []struct{ path, want string }{
		{"/api/behaviour/delta", "both a reference recording"},
		{"/api/behaviour/delta?a=one.jsonl&b=one.jsonl", "two different recordings"},
		{"/api/behaviour/delta?a=one.jsonl&b=../one.jsonl", "invalid"},
		{"/api/behaviour?from=yesterday", "from must be a date"},
		{"/api/behaviour?from=2026-05-01&to=2026-01-01", "before the start date"},
		{"/api/behaviour/signature", "id is required"},
		{"/api/behaviour/signature?id=doesnotexist", "no analysed recording"},
	}
	for _, c := range cases {
		rw, _ := requestJSON(t, newMux(srv), "GET", c.path, nil)
		if rw.Code < 400 {
			t.Errorf("%s: status %d, want an error", c.path, rw.Code)
			continue
		}
		if !strings.Contains(rw.Body.String(), c.want) {
			t.Errorf("%s: body %q does not mention %q", c.path, rw.Body.String(), c.want)
		}
	}
}

// TestBehaviourRepeatsAndSignatureAPIs covers the two drill-down surfaces the
// interface uses: one recording's findings, and one group's occurrences.
func TestBehaviourRepeatsAndSignatureAPIs(t *testing.T) {
	failure := bc{tool: "db__query", args: map[string]any{"q": "select 1"}, verdict: "allow",
		result: errorResult("relation \"users\" does not exist"), isError: true}
	srv, _ := behaviourServer(t, map[string][]bc{
		"a.jsonl": {failure, failure, failure},
		"b.jsonl": {failure},
	})

	rw, _ := requestJSON(t, newMux(srv), "GET", "/api/behaviour/repeats?file=a.jsonl", nil)
	if rw.Code != 200 {
		t.Fatalf("repeats status %d: %s", rw.Code, rw.Body.String())
	}
	var repeats struct {
		Findings   []repeatFinding `json:"findings"`
		Thresholds map[string]any  `json:"thresholds"`
		Session    SessionInfo     `json:"session"`
		Scope      string          `json:"scope"`
	}
	if err := json.Unmarshal(rw.Body.Bytes(), &repeats); err != nil {
		t.Fatal(err)
	}
	if len(repeats.Findings) != 1 || repeats.Findings[0].Kind != kindPossibleRetryLoop {
		t.Errorf("findings: %s", describe(repeats.Findings))
	}
	if repeats.Thresholds["repeat_count"] != float64(minRepeatCount) {
		t.Errorf("the API must publish its threshold, got %v", repeats.Thresholds)
	}
	if repeats.Session.File != "a.jsonl" {
		t.Errorf("session = %+v", repeats.Session)
	}
	if strings.Contains(rw.Body.String(), secretArg) {
		t.Error("the repeats API must not return argument values")
	}

	// Drill into the group from the overview's own identifier.
	_, ovRaw := requestJSON(t, newMux(srv), "GET", "/api/behaviour", nil)
	var ov behaviourOverview
	if err := json.Unmarshal(ovRaw["failures"], &ov.Failures); err != nil {
		t.Fatal(err)
	}
	if len(ov.Failures) != 1 {
		t.Fatalf("failure groups = %d", len(ov.Failures))
	}
	group := ov.Failures[0]
	if group.Sessions != 2 || group.Occurrences != 4 {
		t.Errorf("group = occurrences %d over %d sessions, want 4/2", group.Occurrences, group.Sessions)
	}
	rw, _ = requestJSON(t, newMux(srv), "GET", "/api/behaviour/signature?id="+group.ID, nil)
	if rw.Code != 200 {
		t.Fatalf("signature status %d: %s", rw.Code, rw.Body.String())
	}
	var detail struct {
		Signature   failureSignature `json:"signature"`
		Occurrences []occurrence     `json:"occurrences"`
		Total       int              `json:"total"`
		Sessions    []string         `json:"sessions"`
	}
	if err := json.Unmarshal(rw.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if detail.Total != 4 || len(detail.Occurrences) != 4 || len(detail.Sessions) != 2 {
		t.Errorf("detail = %d total, %d occurrences, %d sessions", detail.Total, len(detail.Occurrences), len(detail.Sessions))
	}
	if detail.Signature.Criteria == "" {
		t.Error("a group must explain its matching criteria")
	}
	if detail.Signature.Fragment == "" || !strings.Contains(detail.Signature.Label, "<text>") {
		t.Errorf("unexpected signature: %+v", detail.Signature)
	}
	for _, o := range detail.Occurrences {
		if o.Seq == 0 || o.File == "" {
			t.Errorf("every occurrence must point at a recorded call: %+v", o)
		}
	}
}

// --- Phase 7: from a finding to something enforceable ---------------------

// TestBehaviourCanTurnARecordedErrorIntoAnAbsenceAssertion closes the loop:
// the recurring failure the workbench found can become a real `tapelog test`
// assertion, and the generated test fails against the recording that motivated
// it. That failure is the point — it states the behaviour you want next time.
func TestBehaviourCanTurnARecordedErrorIntoAnAbsenceAssertion(t *testing.T) {
	dir := t.TempDir()
	rec := behaviourRecording(t, dir, "failing.jsonl", []bc{
		{tool: "db__query", args: map[string]any{"q": "select 1"}, verdict: "allow",
			result: errorResult("relation \"users\" does not exist"), isError: true},
	})
	a := analyseRecording(rec)
	if len(a.Sigs) != 1 {
		t.Fatalf("expected one failure group, got %d", len(a.Sigs))
	}
	var failing *activity
	for _, act := range rec.Activities {
		if act.Status == "failed" {
			failing = act
		}
	}
	if failing == nil {
		t.Fatal("no failing call in the fixture")
	}
	sc, err := buildScenario(rec, failing.Seq, "error_absent", "failing.jsonl")
	if err != nil {
		t.Fatalf("buildScenario: %v", err)
	}
	if sc.Assert[0].ResultNeverContains == nil {
		t.Fatalf("expected a result_never_contains assertion, got %+v", sc.Assert[0])
	}
	text := sc.Assert[0].ResultNeverContains.Text
	if text == "" || !strings.Contains(`relation "users" does not exist`, text) {
		t.Errorf("the assertion text must be a literal phrase from the recording, got %q", text)
	}
	if strings.Contains(text, secretArg) {
		t.Error("the generated assertion must not copy argument values")
	}
	tape := loadTape(t, dir, "failing.jsonl")
	if fails := scenario.Run(sc, tape, nil); len(fails) != 1 {
		t.Fatalf("the generated test must fail on the recording that motivated it, got %+v", fails)
	} else if !strings.Contains(fails[0].Detail, "recorded result") {
		t.Errorf("failure detail = %q", fails[0].Detail)
	}

	// A call with no recorded error cannot produce this assertion.
	ok := behaviourRecording(t, dir, "fine.jsonl", []bc{
		{tool: "files__read", args: map[string]any{"path": "/tmp/a"}, verdict: "allow", result: map[string]any{"ok": true}},
	})
	var good *activity
	for _, act := range ok.Activities {
		good = act
	}
	if _, err := buildScenario(ok, good.Seq, "error_absent", "fine.jsonl"); err == nil {
		t.Error("a successful call must not produce an absence assertion")
	}
	// An error whose message is only values has no stable phrase to assert.
	opaque := behaviourRecording(t, dir, "opaque.jsonl", []bc{
		{tool: "files__read", args: map[string]any{"path": "/tmp/a"}, verdict: "allow",
			result: errorResult("42"), isError: true},
	})
	var blank *activity
	for _, act := range opaque.Activities {
		blank = act
	}
	if _, err := buildScenario(opaque, blank.Seq, "error_absent", "opaque.jsonl"); err == nil {
		t.Error("an error with no stable phrase must be refused, not guessed")
	}
}

// TestBehaviourStylesUseThemeTokens guards light/dark parity. Every colour the
// M6 styles need must come from the existing custom properties; a hard-coded
// hex value would look correct in one theme and wrong in the other.
func TestBehaviourStylesUseThemeTokens(t *testing.T) {
	css, err := staticFS.ReadFile("static/app.css")
	if err != nil {
		t.Fatal(err)
	}
	start := strings.Index(string(css), "M6 behaviour intelligence")
	if start < 0 {
		t.Fatal("the M6 style block is missing")
	}
	block := string(css)[start:]
	hexColour := regexp.MustCompile(`#[0-9a-fA-F]{3,8}\b`)
	for _, line := range strings.Split(block, "\n") {
		if hexColour.MatchString(line) {
			t.Errorf("hard-coded colour in the M6 styles (use a custom property): %s", strings.TrimSpace(line))
		}
		if strings.Contains(line, "rgb(") || strings.Contains(line, "rgba(") {
			t.Errorf("hard-coded colour function in the M6 styles: %s", strings.TrimSpace(line))
		}
		if strings.Contains(line, "!important") {
			t.Errorf("M6 styles must not need !important: %s", strings.TrimSpace(line))
		}
	}
	// The block must define the light/dark-sensitive surfaces it uses.
	for _, token := range []string{"var(--panel)", "var(--line)", "var(--muted)", "var(--accent)", "var(--red)", "var(--amber)", "var(--blue)"} {
		if !strings.Contains(block, token) {
			t.Errorf("expected the M6 styles to use %s", token)
		}
	}
}

// BenchmarkAnalyseRecording measures the cost the overview pays per recording.
// It is the number behind the analysis budget: the index is bounded by files and
// bytes, and this is what one of those bytes buys.
func BenchmarkAnalyseRecording(b *testing.B) {
	calls := make([]bc, 0, 2000)
	for i := 0; i < 2000; i++ {
		calls = append(calls, bc{
			tool:    fmt.Sprintf("svc__tool%02d", i%40),
			args:    map[string]any{"index": i, "mode": "benchmark"},
			verdict: "allow", rule: "default", result: map[string]any{"ok": true},
		})
	}
	rec := behaviourRecording(b, b.TempDir(), "bench.jsonl", calls)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if got := analyseRecording(rec); len(got.Calls) != 2000 {
			b.Fatalf("calls = %d", len(got.Calls))
		}
	}
}

// BenchmarkDetectRepeats isolates the detectors, which run once per recording
// whether or not anybody looks at them.
func BenchmarkDetectRepeats(b *testing.B) {
	calls := make([]callRecord, 0, 2000)
	for i := 0; i < 2000; i++ {
		calls = append(calls, callRecord{
			Seq: uint64(i + 1), Tool: fmt.Sprintf("svc__tool%02d", i%40),
			ArgsKey: fmt.Sprintf("%016x", i%500), ResultKey: fmt.Sprintf("%016x", i), Status: "success",
		})
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = detectRepeats(calls)
	}
}

// TestBehaviourSingleFileMode proves the overview works when the workbench was
// opened on one log file rather than a directory.
func TestBehaviourSingleFileMode(t *testing.T) {
	dir := t.TempDir()
	path := writeBehaviourFile(t, dir, "only.jsonl", []bc{
		{tool: "files__read", args: map[string]any{"path": "/tmp/a"}, verdict: "allow", result: map[string]any{"ok": true}},
	})
	srv := &server{logPath: path}
	rw, _ := requestJSON(t, newMux(srv), "GET", "/api/behaviour", nil)
	if rw.Code != 200 {
		t.Fatalf("status %d: %s", rw.Code, rw.Body.String())
	}
	var ov behaviourOverview
	if err := json.Unmarshal(rw.Body.Bytes(), &ov); err != nil {
		t.Fatal(err)
	}
	if ov.Counts.Sessions != 1 || ov.Counts.Calls != 1 || ov.Library.Files != 1 {
		t.Errorf("single-file overview = %+v", ov.Counts)
	}
	// Comparison needs two recordings, and the API must say so rather than fail obscurely.
	rw, _ = requestJSON(t, newMux(srv), "GET", "/api/behaviour/delta?a=x.jsonl&b=y.jsonl", nil)
	if rw.Code != 400 || !strings.Contains(rw.Body.String(), "directory mode") {
		t.Errorf("status %d body %q", rw.Code, rw.Body.String())
	}
}
