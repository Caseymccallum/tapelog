package lab

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Caseymccallum/tapelog/internal/session"
)

func TestCompleteBoundaryAbsentToolMatrixAndLegacyCompatibility(t *testing.T) {
	complete := mixedFixture(t, []call{{tool: "other", verdict: "allow", result: "ok"}})
	empty := mixedFixture(t, nil)
	lines := bytes.Split(bytes.TrimSpace(complete), []byte("\n"))
	incomplete := append(bytes.Join(lines[:len(lines)-1], []byte("\n")), '\n')
	zero := 0
	checks := []struct {
		x    Expectation
		want string
	}{
		{Expectation{Kind: "attempted", Tool: "absent"}, "FAIL"},
		{Expectation{Kind: "answered", Tool: "absent"}, "FAIL"},
		{Expectation{Kind: "denied", Tool: "absent"}, "FAIL"},
		{Expectation{Kind: "result_contains", Tool: "absent", Text: "timeout"}, "FAIL"},
		{Expectation{Kind: "result_never_contains", Tool: "absent", Text: "timeout"}, "INCONCLUSIVE"},
		{Expectation{Kind: "no_attempts", Tool: "absent"}, "PASS"},
		{Expectation{Kind: "attempt_ceiling", Tool: "absent", Maximum: &zero}, "PASS"},
		{Expectation{Kind: "sequence", Sequence: []string{"absent", "other"}}, "FAIL"},
	}
	for _, tt := range checks {
		t.Run(tt.x.Kind, func(t *testing.T) {
			c := Case{Version: 1, Title: "matrix", Evidence: "e.jsonl", Eligibility: "complete-boundary", Expectations: []Expectation{tt.x}}
			if err := c.Validate(); err != nil {
				t.Fatal(err)
			}
			for _, data := range [][]byte{complete, empty} {
				if got := Evaluate(c, data, ""); got.Status != tt.want {
					t.Fatalf("complete: %+v, want %s", got, tt.want)
				}
			}
			if got := Evaluate(c, incomplete, ""); got.Status != "INCONCLUSIVE" || got.Checks[0].Code != CodeIncomplete {
				t.Fatalf("incomplete: %+v", got)
			}
			c.Eligibility = ""
			if got := Evaluate(c, complete, ""); got.Status != "INCONCLUSIVE" {
				t.Fatalf("legacy semantics changed: %+v", got)
			}
		})
	}
}

func TestCompleteBoundaryWitnessesAndMissingEvidence(t *testing.T) {
	tests := []struct {
		name  string
		calls []call
		x     Expectation
		want  string
	}{
		{"deny witness despite missing sibling decision", []call{{tool: "x", verdict: "deny"}, {tool: "x"}}, Expectation{Kind: "denied", Tool: "x"}, "PASS"},
		{"answered missing decision cannot disprove denial", []call{{tool: "x", result: "ok"}}, Expectation{Kind: "denied", Tool: "x"}, "INCONCLUSIVE"},
		{"known allow disproves denial", []call{{tool: "x", verdict: "allow", result: "ok"}}, Expectation{Kind: "denied", Tool: "x"}, "FAIL"},
		{"answered needs a result", []call{{tool: "x", verdict: "allow"}}, Expectation{Kind: "answered", Tool: "x"}, "INCONCLUSIVE"},
		{"contains unknown sibling cannot fail", []call{{tool: "x", result: "ok"}, {tool: "x"}}, Expectation{Kind: "result_contains", Tool: "x", Text: "timeout"}, "INCONCLUSIVE"},
		{"positive witness", []call{{tool: "x", result: "timeout"}, {tool: "x"}}, Expectation{Kind: "result_contains", Tool: "x", Text: "timeout"}, "PASS"},
		{"known counterexample", []call{{tool: "x", result: "timeout"}, {tool: "x"}}, Expectation{Kind: "result_never_contains", Tool: "x", Text: "timeout"}, "FAIL"},
		{"redaction is not absence", []call{{tool: "x", result: session.Redacted}}, Expectation{Kind: "result_never_contains", Tool: "x", Text: "timeout"}, "INCONCLUSIVE"},
		{"unknown result is not absence", []call{{tool: "x", result: "ok"}, {tool: "x"}}, Expectation{Kind: "result_never_contains", Tool: "x", Text: "timeout"}, "INCONCLUSIVE"},
		{"clean results prove text absence", []call{{tool: "x", result: "ok"}}, Expectation{Kind: "result_never_contains", Tool: "x", Text: "timeout"}, "PASS"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := Case{Version: 1, Title: "witness", Evidence: "e.jsonl", Eligibility: "complete-boundary", Expectations: []Expectation{tt.x}}
			if got := Evaluate(c, mixedFixture(t, tt.calls), ""); got.Status != tt.want {
				t.Fatalf("%+v, want %s", got, tt.want)
			}
		})
	}
	_, err := ParseCase([]byte(`{"version":1,"title":"unknown mode","evidence":"e.jsonl","eligibility":"invented","expectations":[{"kind":"attempted","tool":"x"}]}`))
	if err == nil || !strings.Contains(err.Error(), "eligibility") {
		t.Fatal("unknown mode accepted", err)
	}
}
