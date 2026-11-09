package lab

// Conversation-scoped regression cases (M14).
//
// The point of a scope is to stop a claim from being satisfied by evidence from
// somewhere else. These tests are written so that removing the scope makes them
// pass for the wrong reason — the mutation run for them is in the M14 report.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Caseymccallum/tapelog/internal/session"
)

// scopeFixture records one complete, verified recording with three conversations
// worth of activity: conv-a reads a file, conv-b is denied a delete, and one
// call declares nothing at all.
//
//	seq 2  conv-a  read_file   allow  answered
//	seq 5  conv-b  delete_file deny   no result (by design)
//	seq 8  ""      send_http   allow  answered
func scopeFixture(t *testing.T) []byte {
	t.Helper()
	p := filepath.Join(t.TempDir(), "scope.jsonl")
	w, err := session.NewWriter(p, "fictional")
	if err != nil {
		t.Fatal(err)
	}
	add := func(typ session.EventType, v any) uint64 {
		e, err := w.Append(typ, v)
		if err != nil {
			t.Fatal(err)
		}
		return e.Seq
	}
	add(session.EventSessionStart, map[string]any{"harness": "fictional"})
	call := func(id int, tool, conv, verdict string, answered bool) {
		payload := map[string]any{"id": id, "tool": tool, "args": map[string]any{"p": "/fictional"}}
		if conv != "" {
			payload["conversation_id"] = conv
		}
		seq := add(session.EventToolCall, payload)
		add(session.EventPolicyDecision, map[string]any{"id": id, "parent_seq": seq, "verdict": verdict})
		if answered {
			add(session.EventToolResult, map[string]any{"id": id, "parent_seq": seq, "result": map[string]any{"text": "ok"}})
		}
	}
	call(1, "read_file", "conv-a", "allow", true)
	call(2, "delete_file", "conv-b", "deny", false)
	call(3, "send_http", "", "allow", true)
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

func scopedCase(conversation string, x Expectation) Case {
	return Case{
		Version: 1, Title: "fictional scoped case", Evidence: "scope.jsonl",
		Conversation: conversation,
		Expectations: []Expectation{x},
	}
}

// A witness in another conversation must not satisfy a scoped claim. Without the
// scope this PASSes, which is exactly the false positive the scope exists to
// prevent.
func TestScopeCannotBorrowAnotherConversationsWitness(t *testing.T) {
	data := scopeFixture(t)
	r := Evaluate(scopedCase("conv-a", Expectation{Kind: "attempted", Tool: "delete_file"}), data, "")
	if r.Checks[0].Status == "PASS" {
		t.Fatalf("a call in conv-b must not satisfy a conv-a claim: %+v", r.Checks[0])
	}
	// It must not FAIL either. The call was declared as conv-b, but a declared
	// id is a claim by the client, not an authenticated fact, so the recording
	// cannot prove the call was NOT conv-a's. The honest outcome is the existing
	// no_calls inconclusive, exactly as for an unscoped claim about a tool the
	// recording never shows.
	if r.Checks[0].Code != CodeNoCalls {
		t.Fatalf("want %s from the scoped view, got %q (%s)", CodeNoCalls, r.Checks[0].Code, r.Checks[0].Detail)
	}

	// The same claim with no scope is satisfied by conv-b's call, which shows the
	// assertion itself is sound and only the scope changes the answer.
	unscoped := Case{Version: 1, Title: "fictional unscoped case", Evidence: "scope.jsonl",
		Expectations: []Expectation{{Kind: "attempted", Tool: "delete_file"}}}
	if got := Evaluate(unscoped, data, ""); got.Status != "PASS" {
		t.Fatalf("the unscoped claim should be satisfied by the recorded call, got %s", got.Status)
	}
}

// An absence claim needs the whole conversation to be observable. An
// unattributed call of the same tool could be this conversation's, so the answer
// is INCONCLUSIVE, never PASS.
func TestScopeAbsenceIsBlockedByUnplaceableActivity(t *testing.T) {
	data := scopeFixture(t)
	r := Evaluate(scopedCase("conv-a", Expectation{Kind: "no_attempts", Tool: "send_http"}), data, "")
	if r.Status != "INCONCLUSIVE" {
		t.Fatalf("an absence claim must not conclude while calls cannot be placed, got %s", r.Status)
	}
	if r.Checks[0].Code != CodeConversationScopeUnknown {
		t.Fatalf("want code %s, got %q (%s)", CodeConversationScopeUnknown, r.Checks[0].Code, r.Checks[0].Detail)
	}
}

// A scope that no recorded call declares is an absent conversation, not an empty
// one: it cannot prove absence and it cannot prove presence.
func TestScopeOfAnAbsentConversationIsInconclusive(t *testing.T) {
	data := scopeFixture(t)
	r := Evaluate(scopedCase("conv-zzz", Expectation{Kind: "attempted", Tool: "read_file"}), data, "")
	if r.Status != "INCONCLUSIVE" || r.Checks[0].Code != CodeConversationAbsent {
		t.Fatalf("an absent conversation must be INCONCLUSIVE with %s, got %s %q", CodeConversationAbsent, r.Status, r.Checks[0].Code)
	}
}

// The deny-bypass invariant is about the recording's boundary as a whole. One
// conversation's slice cannot demonstrate it, and pretending otherwise would be
// a stronger claim than the evidence supports.
func TestScopeCannotDemonstrateARecordingInvariant(t *testing.T) {
	data := scopeFixture(t)
	r := Evaluate(scopedCase("conv-b", Expectation{Kind: "no_deny_bypassed"}), data, "")
	if r.Status != "INCONCLUSIVE" || r.Checks[0].Code != CodeConversationScopeInvariant {
		t.Fatalf("want INCONCLUSIVE/%s, got %s %q", CodeConversationScopeInvariant, r.Status, r.Checks[0].Code)
	}
}

// Scoping must not launder ambiguity: a result shared under one request id is
// still unattributable inside the scope.
func TestScopeKeepsAmbiguousCorrelationInconclusive(t *testing.T) {
	p := filepath.Join(t.TempDir(), "ambiguous.jsonl")
	w, err := session.NewWriter(p, "fictional")
	if err != nil {
		t.Fatal(err)
	}
	add := func(typ session.EventType, v any) {
		if _, err := w.Append(typ, v); err != nil {
			t.Fatal(err)
		}
	}
	add(session.EventSessionStart, map[string]any{"harness": "fictional"})
	add(session.EventToolCall, map[string]any{"id": 1, "tool": "read_file", "args": map[string]any{}, "conversation_id": "conv-a"})
	add(session.EventPolicyDecision, map[string]any{"id": 1, "parent_seq": 2, "verdict": "allow"})
	add(session.EventToolCall, map[string]any{"id": 1, "tool": "read_file", "args": map[string]any{}, "conversation_id": "conv-a"})
	add(session.EventPolicyDecision, map[string]any{"id": 1, "parent_seq": 4, "verdict": "allow"})
	add(session.EventToolResult, map[string]any{"id": 1, "is_error": false, "result": map[string]any{"text": "ok"}, "attribution_ambiguous": true})
	add(session.EventSessionEnd, map[string]any{"reason": "done"})
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	r := Evaluate(scopedCase("conv-a", Expectation{Kind: "answered", Tool: "read_file"}), data, "")
	if r.Status != "INCONCLUSIVE" || r.Checks[0].Code != CodeAmbiguous {
		t.Fatalf("a shared request id inside the scope must stay inconclusive, got %s %q", r.Status, r.Checks[0].Code)
	}
}

// The scope is a client-declared token, validated like every other id: the
// reserved remainder cannot be named and an oversized id is refused rather than
// truncated into a different claim.
func TestScopeValidationRejectsForgedOrOversizedIds(t *testing.T) {
	long := ""
	for i := 0; i < 200; i++ {
		long += "a"
	}
	for _, id := range []string{"(unattributed)", long, "has spaces"} {
		c := scopedCase(id, Expectation{Kind: "attempted", Tool: "read_file"})
		if err := c.Validate(); err == nil {
			t.Fatalf("conversation id %q must be rejected", id)
		}
	}
}

// A scope is not an eligibility bypass: a recording whose structure fails is
// still structurally invalid, whatever conversation the case names.
func TestScopeCannotBypassStructuralValidity(t *testing.T) {
	p := filepath.Join(t.TempDir(), "startless.jsonl")
	w, err := session.NewWriter(p, "fictional")
	if err != nil {
		t.Fatal(err)
	}
	// No session/start: the file has no verified boundary at all.
	if _, err := w.Append(session.EventToolCall, map[string]any{
		"id": 1, "tool": "read_file", "args": map[string]any{}, "conversation_id": "conv-a",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Append(session.EventToolResult, map[string]any{
		"id": 1, "parent_seq": 1, "result": map[string]any{"text": "ok"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	r := Evaluate(scopedCase("conv-a", Expectation{Kind: "attempted", Tool: "read_file"}), data, "")
	if r.Status != "INCONCLUSIVE" {
		t.Fatalf("a structurally invalid recording must stay inconclusive, got %s", r.Status)
	}
	if r.Checks[0].Code != CodeStructure && r.Checks[0].Code != CodeIncomplete {
		t.Fatalf("want a structure/incompleteness code, got %q (%s)", r.Checks[0].Code, r.Checks[0].Detail)
	}
}

// An id the analysis layer REJECTS is unattributed, so a scope must treat it as
// unplaceable rather than as placed in another conversation. Counting only the
// empty id let a scoped absence claim PASS while such a call sat in the
// recording (M14 review F1).
func TestScopeTreatsRejectedIdsAsUnplaceable(t *testing.T) {
	p := filepath.Join(t.TempDir(), "rejected-id.jsonl")
	w, err := session.NewWriter(p, "fictional")
	if err != nil {
		t.Fatal(err)
	}
	add := func(typ session.EventType, v any) uint64 {
		e, err := w.Append(typ, v)
		if err != nil {
			t.Fatal(err)
		}
		return e.Seq
	}
	add(session.EventSessionStart, map[string]any{"harness": "fictional"})
	// In scope, and answered.
	seq := add(session.EventToolCall, map[string]any{"id": 1, "tool": "read_file", "args": map[string]any{}, "conversation_id": "conv-a"})
	add(session.EventPolicyDecision, map[string]any{"id": 1, "parent_seq": seq, "verdict": "allow"})
	add(session.EventToolResult, map[string]any{"id": 1, "parent_seq": seq, "result": map[string]any{"text": "ok"}})
	// The same tool under an id the analysis layer rejects: it cannot be shown to
	// belong to any conversation, including this one.
	seq2 := add(session.EventToolCall, map[string]any{"id": 2, "tool": "send_http", "args": map[string]any{}, "conversation_id": "bad id"})
	add(session.EventPolicyDecision, map[string]any{"id": 2, "parent_seq": seq2, "verdict": "allow"})
	add(session.EventToolResult, map[string]any{"id": 2, "parent_seq": seq2, "result": map[string]any{"text": "ok"}})
	add(session.EventSessionEnd, map[string]any{"reason": "done"})
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	r := Evaluate(scopedCase("conv-a", Expectation{Kind: "no_attempts", Tool: "send_http"}), data, "")
	if r.Status == "PASS" {
		t.Fatalf("a call with a rejected id could belong to conv-a, so the absence claim must not PASS: %+v", r.Checks[0])
	}
	if r.Checks[0].Code != CodeConversationScopeUnknown {
		t.Fatalf("want %s, got %q (%s)", CodeConversationScopeUnknown, r.Checks[0].Code, r.Checks[0].Detail)
	}
}
