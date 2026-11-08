package lab

import (
	"encoding/json"
	"fmt"
	"github.com/Caseymccallum/tapelog/internal/session"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T, result any, complete, duplicate bool) []byte {
	t.Helper()
	p := filepath.Join(t.TempDir(), "e.jsonl")
	w, err := session.NewWriter(p, "fictional")
	if err != nil {
		t.Fatal(err)
	}
	add := func(typ session.EventType, v any) *session.Event {
		e, err := w.Append(typ, v)
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	add(session.EventSessionStart, map[string]any{"harness": "fixture"})
	c := add(session.EventToolCall, map[string]any{"id": 1, "tool": "db__query", "args": map[string]any{"secret": "do-not-copy"}})
	add(session.EventPolicyDecision, map[string]any{"id": 1, "parent_seq": c.Seq, "verdict": "allow"})
	if result != nil {
		add(session.EventToolResult, map[string]any{"id": 1, "parent_seq": c.Seq, "is_error": true, "result": result})
	}
	if duplicate {
		add(session.EventToolCall, map[string]any{"id": 1, "tool": "db__query", "args": map[string]any{}})
	}
	if complete {
		add(session.EventSessionEnd, map[string]any{"reason": "done"})
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
func testCase(kind string) Case {
	return Case{Version: 1, Title: "fictional", Evidence: "e.jsonl", Expectations: []Expectation{{Kind: kind, Tool: "db__query", Text: "bad"}}}
}

// call is one recorded call in a mixed-outcome fixture.
type call struct {
	tool    string
	verdict string // "" records no decision
	result  any    // nil records no result
}

// mixedFixture builds a complete, unambiguous recording from a call list, so a
// test can put an answered call and a denied call side by side.
func mixedFixture(t *testing.T, calls []call) []byte {
	t.Helper()
	p := filepath.Join(t.TempDir(), "e.jsonl")
	w, err := session.NewWriter(p, "fictional")
	if err != nil {
		t.Fatal(err)
	}
	add := func(typ session.EventType, v any) *session.Event {
		e, err := w.Append(typ, v)
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	add(session.EventSessionStart, map[string]any{"harness": "fixture"})
	for i, c := range calls {
		id := i + 1
		e := add(session.EventToolCall, map[string]any{"id": id, "tool": c.tool, "args": map[string]any{"n": i}})
		if c.verdict != "" {
			add(session.EventPolicyDecision, map[string]any{"id": id, "parent_seq": e.Seq, "verdict": c.verdict})
		}
		if c.result != nil {
			add(session.EventToolResult, map[string]any{"id": id, "parent_seq": e.Seq, "result": c.result})
		}
	}
	add(session.EventSessionEnd, map[string]any{"reason": "done"})
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// TestSuiteContainsABrokenCase proves one unusable case cannot hide the rest of a
// suite: it becomes its own inconclusive entry and the others still run.
func TestSuiteContainsABrokenCase(t *testing.T) {
	dir := t.TempDir()
	good := mixedFixture(t, []call{{tool: "db__query", verdict: "allow", result: map[string]any{"ok": true}}})
	if err := os.WriteFile(filepath.Join(dir, "good.jsonl"), good, 0o600); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("case-good.json", `{"version":1,"title":"good","evidence":"good.jsonl","expectations":[{"kind":"answered","tool":"db__query"}]}`)
	write("case-broken.json", `{"version":1,"title":"broken","evidence":"good.jsonl","expectations":[{"kind":"nonsense"}]}`)
	write("suite.json", `{"version":1,"title":"mixed","cases":["case-missing.json","case-broken.json","case-good.json"]}`)

	raw, err := os.ReadFile(filepath.Join(dir, "suite.json"))
	if err != nil {
		t.Fatal(err)
	}
	suite, err := ParseSuite(raw)
	if err != nil {
		t.Fatal(err)
	}
	out, err := RunSuite(dir, suite)
	if err != nil {
		t.Fatalf("a broken case must not abort the suite: %v", err)
	}
	if out.Pass != 1 || out.Invalid != 2 || out.Inconclusive != 2 {
		t.Fatalf("counts = pass %d invalid %d inconclusive %d", out.Pass, out.Invalid, out.Inconclusive)
	}
	if out.Status != "INCONCLUSIVE" {
		t.Fatalf("status = %s", out.Status)
	}
	if out.Cases[0].Checks[0].Code != CodeInvalidCase {
		t.Errorf("missing case code = %q", out.Cases[0].Checks[0].Code)
	}
	if out.Cases[2].Status != "PASS" {
		t.Errorf("the good case must still run, got %s", out.Cases[2].Status)
	}
}

// TestUnreadableEvidenceStillExplainsItself keeps a per-expectation reason on the
// path where the recording cannot be opened at all.
func TestUnreadableEvidenceStillExplainsItself(t *testing.T) {
	c := testCase("result_never_contains")
	r, err := RunCase(t.TempDir(), c, "")
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != "INCONCLUSIVE" || len(r.Checks) != 1 {
		t.Fatalf("result = %+v", r)
	}
	if r.Checks[0].Code != CodeUnreadable {
		t.Fatalf("code = %q", r.Checks[0].Code)
	}
	if strings.TrimSpace(r.Checks[0].Detail) == "" {
		t.Fatal("an unreadable recording must say why")
	}
}

// TestEveryInconclusiveOutcomeCarriesACode is a guard against prose-only reasons:
// the interface groups by code, so no inconclusive result may omit one.
func TestEveryInconclusiveOutcomeCarriesACode(t *testing.T) {
	data := fixture(t, nil, true, false)
	kinds := []Expectation{
		{Kind: "answered", Tool: "db__query"},
		{Kind: "denied", Tool: "db__query"},
		{Kind: "result_contains", Tool: "db__query", Text: "x"},
		{Kind: "result_never_contains", Tool: "db__query", Text: "x"},
		{Kind: "attempted", Tool: "absent__tool"},
		{Kind: "no_deny_bypassed"},
		{Kind: "sequence", Sequence: []string{"db__query", "db__query"}},
	}
	for _, x := range kinds {
		c := Case{Version: 1, Title: "coded", Evidence: "e.jsonl", Expectations: []Expectation{x}}
		if err := c.Validate(); err != nil {
			t.Fatalf("%s: %v", x.Kind, err)
		}
		r := Evaluate(c, data, "")
		if r.Status != "INCONCLUSIVE" {
			continue
		}
		for _, ch := range r.Checks {
			if ch.Status == "INCONCLUSIVE" && ch.Code == "" {
				t.Errorf("%s produced an inconclusive check with no code: %+v", x.Kind, ch)
			}
		}
	}
}

// existential claim is satisfied by one eligible witness, and a universal claim
// needs every result. Conflating the two produced false INCONCLUSIVE outcomes.
func TestEligibilityFollowsTheClaimShape(t *testing.T) {
	// One answered call, three denied attempts: the pattern a boundary produces
	// when an agent retries a forbidden operation and only one attempt succeeded.
	mixed := mixedFixture(t, []call{
		{tool: "git__push", verdict: "allow", result: map[string]any{"ok": true}},
		{tool: "git__push", verdict: "deny"},
		{tool: "git__push", verdict: "deny"},
		{tool: "git__push", verdict: "deny"},
	})
	allowThenDeny := mixedFixture(t, []call{
		{tool: "db__query", verdict: "allow", result: map[string]any{"error": "timeout"}},
		{tool: "db__query", verdict: "deny"},
	})
	clean := mixedFixture(t, []call{
		{tool: "db__query", verdict: "allow", result: map[string]any{"ok": true}},
	})
	redactedOnly := mixedFixture(t, []call{
		{tool: "db__query", verdict: "allow", result: map[string]any{"token": session.Redacted}},
	})
	noDenials := mixedFixture(t, []call{
		{tool: "db__query", verdict: "allow", result: map[string]any{"ok": true}},
	})
	twoAnsweredPlusDenied := mixedFixture(t, []call{
		{tool: "db__query", verdict: "allow", result: map[string]any{"ok": true}},
		{tool: "files__read", verdict: "allow", result: map[string]any{"ok": true}},
		{tool: "git__push", verdict: "deny"},
	})
	tests := []struct {
		name string
		data []byte
		c    Case
		want string
		code string
	}{
		{"answered is existential", mixed,
			Case{Expectations: []Expectation{{Kind: "answered", Tool: "git__push"}}}, "PASS", ""},
		{"answered with no result at all", clean,
			Case{Expectations: []Expectation{{Kind: "answered", Tool: "absent__tool"}}}, "INCONCLUSIVE", CodeNoCalls},
		{"denied sees the denied attempts", mixed,
			Case{Expectations: []Expectation{{Kind: "denied", Tool: "git__push"}}}, "PASS", ""},
		{"result_contains finds its witness", allowThenDeny,
			Case{Expectations: []Expectation{{Kind: "result_contains", Tool: "db__query", Text: "timeout"}}}, "PASS", ""},
		{"result_contains without a witness is a failure", clean,
			Case{Expectations: []Expectation{{Kind: "result_contains", Tool: "db__query", Text: "nonexistent"}}}, "FAIL", ""},
		{"result_contains cannot search a redacted body", redactedOnly,
			Case{Expectations: []Expectation{{Kind: "result_contains", Tool: "db__query", Text: "secret"}}}, "INCONCLUSIVE", CodeRedacted},
		{"absence survives a denied sibling", allowThenDeny,
			Case{Expectations: []Expectation{{Kind: "result_never_contains", Tool: "db__query", Text: "timeout"}}}, "INCONCLUSIVE", CodeNoResults},
		{"absence needs every result", clean,
			Case{Expectations: []Expectation{{Kind: "result_never_contains", Tool: "db__query", Text: "timeout"}}}, "PASS", ""},
		{"no_attempts counts the boundary", mixed,
			Case{Expectations: []Expectation{{Kind: "no_attempts", Tool: "git__push"}}}, "FAIL", ""},
		{"sequence needs enough results", clean,
			Case{Expectations: []Expectation{{Kind: "sequence", Sequence: []string{"db__query", "db__query"}}}}, "INCONCLUSIVE", CodeShortSequence},
		{"sequence tolerates a denied sibling", twoAnsweredPlusDenied,
			Case{Expectations: []Expectation{{Kind: "sequence", Sequence: []string{"db__query", "files__read"}}}}, "PASS", ""},
		{"invariant without any denial is vacuous", noDenials,
			Case{Expectations: []Expectation{{Kind: "no_deny_bypassed"}}}, "INCONCLUSIVE", CodeNoDenials},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.c.Version, tt.c.Title, tt.c.Evidence = 1, "mixed", "e.jsonl"
			if err := tt.c.Validate(); err != nil {
				t.Fatal(err)
			}
			r := Evaluate(tt.c, tt.data, "")
			if r.Status != tt.want {
				t.Fatalf("status = %s, want %s (%+v)", r.Status, tt.want, r.Checks)
			}
			if got := r.Checks[0].Code; got != tt.code {
				t.Fatalf("code = %q, want %q (%s)", got, tt.code, r.Checks[0].Detail)
			}
		})
	}
}

func TestEligibilityDoesNotConvertMissingEvidenceToPass(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		c    Case
		want string
	}{
		{"absence pass", fixture(t, "fine", true, false), testCase("result_never_contains"), "PASS"},
		{"absence fail", fixture(t, "bad", true, false), testCase("result_never_contains"), "FAIL"},
		{"redacted", fixture(t, "[REDACTED]", true, false), testCase("result_never_contains"), "INCONCLUSIVE"},
		{"no result", fixture(t, nil, true, false), testCase("result_never_contains"), "INCONCLUSIVE"},
		{"incomplete", fixture(t, "fine", false, false), testCase("result_never_contains"), "INCONCLUSIVE"},
		{"duplicate ID", fixture(t, "fine", true, true), testCase("result_never_contains"), "INCONCLUSIVE"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.c.Validate(); err != nil {
				t.Fatal(err)
			}
			r := Evaluate(tt.c, tt.data, "")
			if r.Status != tt.want {
				t.Fatalf("got %+v", r)
			}
		})
	}
}
func TestCaseRevisionAndStrictParsing(t *testing.T) {
	data := fixture(t, "fine", true, false)
	c := testCase("result_never_contains")
	c.EvidenceSHA256 = strings.Repeat("0", 64)
	if r := Evaluate(c, data, ""); r.Status != "INCONCLUSIVE" {
		t.Fatal(r)
	}
	if r := Evaluate(c, data, "candidate.jsonl"); r.Status != "PASS" {
		t.Fatal(r)
	}
	if _, err := ParseCase([]byte(`{"version":1,"title":"x","evidence":"../x.jsonl","expectations":[{"kind":"no_attempts","tool":"x"}]}`)); err == nil {
		t.Fatal("accepted traversal")
	}
	if _, err := ParseCase([]byte(`{"version":1,"title":"x","evidence":"x.jsonl","unknown":1,"expectations":[{"kind":"no_attempts","tool":"x"}]}`)); err == nil {
		t.Fatal("accepted unknown field")
	}
	if _, err := ParseSuite([]byte(`{"version":1,"title":"x","cases":["x.json","x.json"]}`)); err == nil {
		t.Fatal("accepted duplicate suite entry")
	}
}
func TestSuiteOutcomesAndLimits(t *testing.T) {
	dir := t.TempDir()
	data := fixture(t, "bad", true, false)
	if err := os.WriteFile(filepath.Join(dir, "e.jsonl"), data, 0600); err != nil {
		t.Fatal(err)
	}
	pass := Case{Version: 1, Title: "pass", Evidence: "e.jsonl", Expectations: []Expectation{{Kind: "answered", Tool: "db__query"}}}
	fail := testCase("result_never_contains")
	missing := Case{Version: 1, Title: "missing", Evidence: "missing.jsonl", Expectations: []Expectation{{Kind: "answered", Tool: "db__query"}}}
	for i, c := range []Case{pass, fail, missing} {
		raw, _ := json.Marshal(c)
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("%d.json", i)), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	out, err := RunSuite(dir, Suite{Version: 1, Title: "suite", Cases: []string{"0.json", "1.json", "2.json"}})
	if err != nil || out.Status != "FAIL" || out.Pass != 1 || out.Fail != 1 || out.Inconclusive != 1 {
		t.Fatal(out, err)
	}
}

func TestDamagedOrIrrelevantEvidenceIsInconclusive(t *testing.T) {
	data := fixture(t, "fine", true, false)
	damaged := []byte(strings.Replace(string(data), "fine", "forged", 1))
	c := testCase("result_never_contains")
	if r := Evaluate(c, damaged, ""); r.Status != "INCONCLUSIVE" {
		t.Fatalf("damaged chain accepted: %+v", r)
	}
	c.Expectations[0].Tool = "other__tool"
	if r := Evaluate(c, data, ""); r.Status != "INCONCLUSIVE" {
		t.Fatalf("missing target accepted: %+v", r)
	}
}
