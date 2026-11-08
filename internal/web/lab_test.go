package web

// Tests for the regression lab library: discovery, safe saving, running and the
// boundary between what the workbench may and may not touch.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Caseymccallum/tapelog/internal/lab"
	"github.com/Caseymccallum/tapelog/internal/session"
)

// labFixture writes one recording plus cases and a suite, and returns the
// directory. The recording has one answered call and one denied call, which is
// the shape that exposed the over-conservative eligibility rule.
func labFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "evidence.jsonl")
	w, err := session.NewWriter(path, "fictional-lab")
	if err != nil {
		t.Fatal(err)
	}
	add := func(kind session.EventType, v any) *session.Event {
		e, err := w.Append(kind, v)
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	add(session.EventSessionStart, map[string]any{"harness": "fixture", "task": "lab"})
	first := add(session.EventToolCall, map[string]any{"id": 1, "tool": "db__query", "args": map[string]any{"secret": "do-not-copy"}})
	add(session.EventPolicyDecision, map[string]any{"id": 1, "parent_seq": first.Seq, "verdict": "allow", "rule_id": "default"})
	add(session.EventToolResult, map[string]any{"id": 1, "parent_seq": first.Seq, "result": map[string]any{"ok": true}})
	second := add(session.EventToolCall, map[string]any{"id": 2, "tool": "git__push", "args": map[string]any{"force": true}})
	add(session.EventPolicyDecision, map[string]any{"id": 2, "parent_seq": second.Seq, "verdict": "deny", "rule_id": "no-force-push"})
	add(session.EventSessionEnd, map[string]any{"reason": "done"})
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("answered.json", `{"version":1,"title":"Lookup was answered","description":"The lookup returned a result.","evidence":"evidence.jsonl","expectations":[{"kind":"answered","tool":"db__query"}]}`)
	write("denied.json", `{"version":1,"title":"Force push denied","evidence":"evidence.jsonl","expectations":[{"kind":"denied","tool":"git__push"}]}`)
	write("suite.json", `{"version":1,"title":"Nightly","cases":["answered.json","denied.json"]}`)
	write("notes.json", `{"unrelated":"value"}`)
	return dir
}

func TestLabIndexListsCasesAndSuitesAndShowsBrokenOnes(t *testing.T) {
	dir := labFixture(t)
	if err := os.WriteFile(filepath.Join(dir, "broken.json"), []byte(`{"version":1,"title":"","expectations":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "suite-missing.json"), []byte(`{"version":1,"title":"Gaps","cases":["answered.json","absent.json"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	rw, _ := requestJSON(t, newMux(&server{dir: dir}), "GET", "/api/lab", nil)
	if rw.Code != 200 {
		t.Fatalf("status %d: %s", rw.Code, rw.Body.String())
	}
	var index labIndex
	if err := json.Unmarshal(rw.Body.Bytes(), &index); err != nil {
		t.Fatal(err)
	}
	if len(index.Cases) != 3 {
		t.Fatalf("cases = %d, want 3 (two valid plus one invalid)", len(index.Cases))
	}
	if len(index.Suites) != 2 {
		t.Fatalf("suites = %d, want 2", len(index.Suites))
	}
	var broken, ok bool
	for _, c := range index.Cases {
		switch c.File {
		case "broken.json":
			broken = c.Invalid != ""
		case "answered.json":
			ok = c.Invalid == "" && c.EvidencePresent && c.Title == "Lookup was answered"
		}
		if c.File == "notes.json" {
			t.Error("unrelated JSON must not be listed as a case")
		}
	}
	if !broken {
		t.Error("an invalid case must be listed with its reason, not hidden")
	}
	if !ok {
		t.Error("a valid case must be listed with its evidence present")
	}
	for _, s := range index.Suites {
		if s.File == "suite-missing.json" && len(s.Missing) != 1 {
			t.Errorf("missing suite members must be reported: %+v", s)
		}
	}
}

func TestLabCaseGetAndEvidenceState(t *testing.T) {
	dir := labFixture(t)
	rw, _ := requestJSON(t, newMux(&server{dir: dir}), "GET", "/api/lab/case?file=answered.json", nil)
	if rw.Code != 200 {
		t.Fatalf("status %d: %s", rw.Code, rw.Body.String())
	}
	var resp struct {
		Case     labCaseEntry `json:"case"`
		JSON     string       `json:"json"`
		Evidence struct {
			File    string `json:"file"`
			Present bool   `json:"present"`
		} `json:"evidence"`
		Valid bool `json:"valid"`
	}
	if err := json.Unmarshal(rw.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Valid || resp.Case.Title != "Lookup was answered" || !resp.Evidence.Present {
		t.Fatalf("unexpected payload: %+v", resp)
	}
	if !strings.Contains(resp.JSON, `"expectations"`) {
		t.Error("the raw definition must be returned so the case can be edited")
	}
	if strings.Contains(rw.Body.String(), "do-not-copy") {
		t.Error("recorded argument values must never reach the lab API")
	}
	for _, bad := range []string{"../answered.json", "sub/answered.json", "evidence.jsonl", ""} {
		rw, _ := requestJSON(t, newMux(&server{dir: dir}), "GET", "/api/lab/case?file="+bad, nil)
		if rw.Code == 200 {
			t.Errorf("%q must not be readable as a case", bad)
		}
	}
}

func TestLabSaveCreatesRefusesOverwriteAndRespectsRevisions(t *testing.T) {
	dir := labFixture(t)
	h := newMux(&server{dir: dir})
	body := map[string]any{
		"file": "new-case.json",
		"case": map[string]any{"version": 1, "title": "New case", "evidence": "evidence.jsonl",
			"expectations": []map[string]any{{"kind": "answered", "tool": "db__query"}}},
	}
	rw, resp := requestJSON(t, h, "POST", "/api/lab/case", body)
	if rw.Code != 200 {
		t.Fatalf("create status %d: %s", rw.Code, rw.Body.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "new-case.json")); err != nil {
		t.Fatal("the case was not written")
	}
	// Saving again without a revision is refused: never silently overwrite.
	rw, _ = requestJSON(t, h, "POST", "/api/lab/case", body)
	if rw.Code != 409 {
		t.Fatalf("second create = %d, want 409 (%s)", rw.Code, rw.Body.String())
	}
	var revision string
	_ = json.Unmarshal(resp["revision"], &revision)
	stale := map[string]any{"file": "new-case.json", "replaces": "0000", "case": body["case"]}
	rw, _ = requestJSON(t, h, "POST", "/api/lab/case", stale)
	if rw.Code != 409 {
		t.Fatalf("stale replace = %d, want 409", rw.Code)
	}
	edited := map[string]any{"version": 1, "title": "New case (edited)", "evidence": "evidence.jsonl",
		"expectations": []map[string]any{{"kind": "answered", "tool": "db__query"}}}
	rw, _ = requestJSON(t, h, "POST", "/api/lab/case", map[string]any{"file": "new-case.json", "replaces": revision, "case": edited})
	if rw.Code != 200 {
		t.Fatalf("replace status %d: %s", rw.Code, rw.Body.String())
	}
	saved, err := os.ReadFile(filepath.Join(dir, "new-case.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(saved), "edited") {
		t.Error("the replacement was not written")
	}
	rw, _ = requestJSON(t, h, "POST", "/api/lab/case", map[string]any{"file": "bad.json",
		"case": map[string]any{"version": 1, "title": "", "evidence": "evidence.jsonl", "expectations": []map[string]any{}}})
	if rw.Code != 422 {
		t.Fatalf("invalid case = %d, want 422", rw.Code)
	}
	if _, err := os.Stat(filepath.Join(dir, "bad.json")); err == nil {
		t.Error("an invalid case must not be written")
	}
}

func TestLabWritesStayInsideTheDirectory(t *testing.T) {
	dir := labFixture(t)
	h := newMux(&server{dir: dir})
	bad := map[string]any{"file": "../escape.json",
		"case": map[string]any{"version": 1, "title": "Escape", "evidence": "evidence.jsonl",
			"expectations": []map[string]any{{"kind": "answered", "tool": "db__query"}}}}
	rw, _ := requestJSON(t, h, "POST", "/api/lab/case", bad)
	if rw.Code == 200 {
		t.Fatal("a traversal filename must be refused")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "escape.json")); err == nil {
		t.Fatal("a file was written outside the evidence directory")
	}
	// A directory pretending to be a case is refused (this works without the
	// symlink privilege Windows usually withholds).
	if err := os.Mkdir(filepath.Join(dir, "folder.json"), 0o700); err != nil {
		t.Fatal(err)
	}
	rw, _ = requestJSON(t, h, "GET", "/api/lab/case?file=folder.json", nil)
	if rw.Code == 200 {
		t.Fatal("a directory must not be served as a case")
	}
	// A symlinked case file is not a lab document either, where symlinks exist.
	link := filepath.Join(dir, "linked.json")
	if err := os.Symlink(filepath.Join(dir, "answered.json"), link); err == nil {
		rw, _ = requestJSON(t, h, "GET", "/api/lab/case?file=linked.json", nil)
		if rw.Code == 200 {
			t.Fatal("a symlink must not be served as a case")
		}
	}
}

func TestLabRunReportsOutcomesAndAgreesWithTheEngine(t *testing.T) {
	dir := labFixture(t)
	h := newMux(&server{dir: dir})
	for _, c := range []struct{ file, want string }{
		{"answered.json", "PASS"},
		{"denied.json", "PASS"},
	} {
		rw, _ := requestJSON(t, h, "POST", "/api/lab/run", map[string]any{"file": c.file})
		if rw.Code != 200 {
			t.Fatalf("%s status %d: %s", c.file, rw.Code, rw.Body.String())
		}
		var resp struct {
			Result  lab.Result `json:"result"`
			Command string     `json:"command"`
		}
		if err := json.Unmarshal(rw.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		if resp.Result.Status != c.want {
			t.Fatalf("%s = %s, want %s (%+v)", c.file, resp.Result.Status, c.want, resp.Result.Checks)
		}
		if !strings.Contains(resp.Command, "tapelog lab run") {
			t.Errorf("the run must name the CLI equivalent, got %q", resp.Command)
		}
	}
	// An absence assertion over denied-only calls is inconclusive, with a code.
	if err := os.WriteFile(filepath.Join(dir, "absent.json"), []byte(`{"version":1,"title":"Absent tool","evidence":"evidence.jsonl","expectations":[{"kind":"result_never_contains","tool":"git__push","text":"denied"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	rw, body := requestJSON(t, h, "POST", "/api/lab/run", map[string]any{"file": "absent.json"})
	if rw.Code != 200 {
		t.Fatalf("status %d: %s", rw.Code, rw.Body.String())
	}
	var result lab.Result
	_ = json.Unmarshal(body["result"], &result)
	if result.Status != "INCONCLUSIVE" || result.Checks[0].Code == "" {
		t.Fatalf("result = %+v", result)
	}
	// The engine and the web API agree exactly on the same file.
	saved, _ := os.ReadFile(filepath.Join(dir, "absent.json"))
	parsed, err := lab.ParseCase(saved)
	if err != nil {
		t.Fatal(err)
	}
	direct, err := lab.RunCase(dir, parsed, "")
	if err != nil {
		t.Fatal(err)
	}
	if direct.Status != result.Status || direct.Checks[0].Code != result.Checks[0].Code {
		t.Errorf("CLI and web disagree: %+v vs %+v", direct, result)
	}
}

func TestLabSuiteRunCountsAndNeverPassesABrokenCase(t *testing.T) {
	dir := labFixture(t)
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("absent.json", `{"version":1,"title":"Absent","evidence":"evidence.jsonl","expectations":[{"kind":"result_never_contains","tool":"git__push","text":"denied"}]}`)
	write("broken.json", `{"version":1,"title":"Broken","evidence":"evidence.jsonl","expectations":[{"kind":"unsupported"}]}`)
	write("mixed.json", `{"version":1,"title":"Mixed","cases":["answered.json","absent.json","broken.json","gone.json"]}`)
	rw, body := requestJSON(t, newMux(&server{dir: dir}), "POST", "/api/lab/suite/run", map[string]any{"file": "mixed.json"})
	if rw.Code != 200 {
		t.Fatalf("status %d: %s", rw.Code, rw.Body.String())
	}
	var resp struct {
		Suite lab.SuiteResult `json:"suite"`
	}
	if err := json.Unmarshal(body["suite"], &resp.Suite); err != nil {
		t.Fatal(err)
	}
	if resp.Suite.Pass != 1 || resp.Suite.Invalid != 2 || resp.Suite.Inconclusive != 3 {
		t.Fatalf("counts = %+v", resp.Suite)
	}
	if resp.Suite.Status != "INCONCLUSIVE" {
		t.Fatalf("overall status = %s", resp.Suite.Status)
	}
	if len(resp.Suite.Cases) != 4 {
		t.Fatalf("every case must have an outcome, got %d", len(resp.Suite.Cases))
	}
	for _, c := range resp.Suite.Cases {
		if c.Status == "PASS" && c.Title != "Lookup was answered" && c.Title != "answered" {
			t.Errorf("%q must not be reported as a pass: %+v", c.Title, c)
		}
	}
}

func TestLabSaveSuiteProtectsExistingFiles(t *testing.T) {
	dir := labFixture(t)
	h := newMux(&server{dir: dir})
	body := map[string]any{
		"file":  "new-suite.json",
		"suite": map[string]any{"version": 1, "title": "New suite", "cases": []string{"answered.json", "gone.json"}},
	}
	rw, resp := requestJSON(t, h, "POST", "/api/lab/suite", body)
	if rw.Code != 200 {
		t.Fatalf("create status %d: %s", rw.Code, rw.Body.String())
	}
	// A suite that names a case which is not there is saved but reported, so the
	// author sees the gap immediately instead of at run time.
	var missing []string
	_ = json.Unmarshal(resp["missing"], &missing)
	if len(missing) != 1 || missing[0] != "gone.json" {
		t.Fatalf("missing = %v", missing)
	}
	// Overwriting needs the revision that was read.
	rw, _ = requestJSON(t, h, "POST", "/api/lab/suite", body)
	if rw.Code != 409 {
		t.Fatalf("second create = %d, want 409", rw.Code)
	}
	var revision string
	_ = json.Unmarshal(resp["revision"], &revision)
	edited := map[string]any{"version": 1, "title": "New suite (edited)", "cases": []string{"answered.json"}}
	rw, _ = requestJSON(t, h, "POST", "/api/lab/suite", map[string]any{"file": "new-suite.json", "replaces": revision, "suite": edited})
	if rw.Code != 200 {
		t.Fatalf("replace status %d: %s", rw.Code, rw.Body.String())
	}
	saved, err := os.ReadFile(filepath.Join(dir, "new-suite.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(saved), "edited") {
		t.Error("the suite replacement was not written")
	}
	// A suite referencing a case outside the directory never reaches the disk.
	rw, _ = requestJSON(t, h, "POST", "/api/lab/suite", map[string]any{"file": "evil-suite.json",
		"suite": map[string]any{"version": 1, "title": "Evil", "cases": []string{"../answered.json"}}})
	if rw.Code != 422 {
		t.Fatalf("traversal suite = %d, want 422", rw.Code)
	}
}

func TestLabCaseNeedsDirectoryMode(t *testing.T) {
	rw, _ := requestJSON(t, newMux(&server{logPath: filepath.Join(t.TempDir(), "x.jsonl")}), "GET", "/api/lab", nil)
	if rw.Code != 400 || !strings.Contains(rw.Body.String(), "directory mode") {
		t.Fatalf("status %d body %s", rw.Code, rw.Body.String())
	}
}


