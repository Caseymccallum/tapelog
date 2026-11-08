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
