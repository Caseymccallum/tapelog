package lab

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Caseymccallum/tapelog/internal/session"

	"github.com/Caseymccallum/tapelog/internal/replay"
)

// An ambiguous recording still supports claims that do not depend on pairing
// a result to a call, and must refuse the ones that do.
func TestAmbiguousEvidenceIsClaimSpecific(t *testing.T) {
	tp := &replay.Tape{
		SessionID:    "s",
		AmbiguousIDs: []string{"1"},
		Interactions: []replay.Interaction{
			{Order: 1, RequestID: "1", Tool: "read_file", Args: []byte(`{"path":"/a"}`), Result: []byte(`{"text":"ok"}`), Verdict: "allow", Ambiguous: true},
			{Order: 2, RequestID: "1", Tool: "read_file", Args: []byte(`{"path":"/b"}`), Result: []byte(`{"text":"ok"}`), Verdict: "allow", Ambiguous: true},
		},
	}
	cases := []struct {
		kind     string
		tool     string
		text     string
		wantCode string   // "" means eligible
		wantAny  []string // allowed alternatives
	}{
		{kind: "attempted", tool: "read_file", wantCode: ""},
		{kind: "attempt_ceiling", tool: "read_file", wantCode: ""},
		// Decisions are produced synchronously by their own call, so a
		// "was this tool denied" claim stays eligible under ambiguity.
		{kind: "denied", tool: "read_file", wantCode: ""},
		// Pairing-dependent claims must not be decided:
		{kind: "answered", tool: "read_file", wantCode: CodeAmbiguous},
		{kind: "result_contains", tool: "read_file", text: "ok", wantCode: CodeAmbiguous},
		{kind: "result_never_contains", tool: "read_file", text: "absent", wantCode: CodeAmbiguous},
	}
	for _, c := range cases {
		x := Expectation{Kind: c.kind, Tool: c.tool, Text: c.text}
		code, reason := eligibility(x, tp)
		if c.wantCode == "" && code != "" {
			t.Fatalf("%s: want eligible, got %q (%s)", c.kind, code, reason)
		}
		if c.wantCode != "" && code != c.wantCode {
			t.Fatalf("%s: want code %q, got %q (%s)", c.kind, c.wantCode, code, reason)
		}
	}
}

// A recording where every answer is unambiguous must be unaffected, including
// when another id elsewhere in the same recording was ambiguous.
func TestUnambiguousInteractionsStayEligible(t *testing.T) {
	tp := &replay.Tape{
		SessionID:    "s",
		AmbiguousIDs: []string{"1"},
		Interactions: []replay.Interaction{
			{Order: 1, RequestID: "1", Tool: "read_file", Result: []byte(`{"text":"shared"}`), Verdict: "allow", Ambiguous: true},
			{Order: 2, RequestID: "1", Tool: "read_file", Result: []byte(`{"text":"shared"}`), Verdict: "allow", Ambiguous: true},
			{Order: 3, RequestID: "9", Tool: "read_file", Result: []byte(`{"text":"unique"}`), Verdict: "allow"},
		},
	}
	if code, reason := eligibility(Expectation{Kind: "answered", Tool: "read_file"}, tp); code != "" {
		t.Fatalf("an unambiguous answer makes the claim eligible, got %q (%s)", code, reason)
	}
	if code, reason := eligibility(Expectation{Kind: "result_never_contains", Tool: "read_file", Text: "absent"}, tp); code != CodeAmbiguous {
		t.Fatalf("absence still cannot be proven while an ambiguous answer exists, got %q (%s)", code, reason)
	}
}

// The no_deny_bypassed invariant depends on pairing a result with the denied
// call, so an ambiguous recording cannot demonstrate it.
func TestNoDenyBypassedRefusesAmbiguousEvidence(t *testing.T) {
	tp := &replay.Tape{
		SessionID:    "s",
		AmbiguousIDs: []string{"1"},
		Interactions: []replay.Interaction{
			{Order: 1, RequestID: "1", Tool: "read_file", Verdict: "deny", Result: []byte(`{}`), Ambiguous: true},
		},
	}
	code, reason := eligibility(Expectation{Kind: "no_deny_bypassed"}, tp)
	if code != CodeAmbiguous {
		t.Fatalf("want %q, got %q (%s)", CodeAmbiguous, code, reason)
	}
}

// End to end: a recording that reuses a request id must load (not be rejected
// as damaged), be marked ambiguous, and still decide the claims that do not
// depend on pairing. This is the path M11.2 claimed and, before the fix,
// never reached.
func TestRepeatedIDRecordingLoadsAndStaysClaimSpecific(t *testing.T) {
	// Two calls outstanding under one id: genuinely ambiguous.
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
	add(session.EventToolCall, map[string]any{"id": 1, "tool": "db__query", "args": map[string]any{"secret": "do-not-copy"}})
	add(session.EventToolCall, map[string]any{"id": 1, "tool": "db__query", "args": map[string]any{}})
	add(session.EventPolicyDecision, map[string]any{"id": 1, "verdict": "allow"})
	add(session.EventToolResult, map[string]any{"id": 1, "is_error": true, "result": "bad words here"})
	add(session.EventSessionEnd, map[string]any{"reason": "done"})
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	tape, err := inspect(data)
	if err != nil {
		t.Fatalf("a repeated request id must not reject the recording: %v", err)
	}
	if len(tape.AmbiguousIDs) == 0 {
		t.Fatal("the recording must be marked ambiguous")
	}
	if code, reason := eligibility(Expectation{Kind: "answered", Tool: "db__query"}, tape); code != CodeAmbiguous {
		t.Fatalf("answered must be ambiguous, got %q (%s)", code, reason)
	}
	if code, reason := eligibility(Expectation{Kind: "result_never_contains", Tool: "db__query", Text: "bad"}, tape); code != CodeAmbiguous {
		t.Fatalf("absence must be ambiguous, got %q (%s)", code, reason)
	}
	if code, reason := eligibility(Expectation{Kind: "attempted", Tool: "db__query"}, tape); code != "" {
		t.Fatalf("a counting claim must stay eligible, got %q (%s)", code, reason)
	}
}
