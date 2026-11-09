package replay

import (
	"path/filepath"
	"testing"

	"github.com/Caseymccallum/tapelog/internal/session"
)

// writeTwoCallsUnderOneID records the shape the independent review used to make
// the Regression Lab return a false PASS: two calls share request id 1 while
// both are outstanding, the first allowed and the second denied. withParent
// controls whether the decisions carry the parent_seq link the recorder always
// writes; without it the pairing is genuinely unknown.
func writeTwoCallsUnderOneID(t *testing.T, withParent bool) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "shared-id.jsonl")
	w, err := session.NewWriter(path, "shared-id-test")
	if err != nil {
		t.Fatal(err)
	}
	mustAppend := func(kind session.EventType, payload map[string]any) uint64 {
		t.Helper()
		e, err := w.Append(kind, payload)
		if err != nil {
			t.Fatal(err)
		}
		return e.Seq
	}
	mustAppend(session.EventSessionStart, map[string]any{"harness": "shared-id-test"})

	callA := mustAppend(session.EventToolCall, map[string]any{
		"id": 1, "tool": "read_file", "args": map[string]any{"path": "/fictional/a.md"}})
	allow := map[string]any{"id": 1, "verdict": "allow", "rule_id": "default", "reason": "no rule matched"}
	if withParent {
		allow["parent_seq"] = callA
	}
	mustAppend(session.EventPolicyDecision, allow)

	callB := mustAppend(session.EventToolCall, map[string]any{
		"id": 1, "tool": "delete_file", "args": map[string]any{"path": "/fictional/a.md"}})
	deny := map[string]any{"id": 1, "verdict": "deny", "rule_id": "no-deletes", "reason": "deletes are forbidden"}
	if withParent {
		deny["parent_seq"] = callB
	}
	mustAppend(session.EventPolicyDecision, deny)

	mustAppend(session.EventToolResult, map[string]any{
		"id": 1, "is_error": false, "parent_seq": callA,
		"result": map[string]any{"content": []any{map[string]any{"type": "text", "text": "# alpha"}}}})
	mustAppend(session.EventSessionEnd, map[string]any{"reason": "client disconnect"})
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

// A decision belongs to the call its parent_seq names. Before this test, the
// loader matched by request id alone and gave the second call's deny to the
// first call, so a lab case claiming "read_file was denied" PASSed on a
// recording where read_file was allowed.
func TestDecisionAttachesByParentSeqNotRequestID(t *testing.T) {
	tape, err := Load(writeTwoCallsUnderOneID(t, true))
	if err != nil {
		t.Fatal(err)
	}
	byTool := map[string]Interaction{}
	for _, it := range append(append([]Interaction{}, tape.Interactions...), tape.Unanswered...) {
		byTool[it.Tool] = it
	}
	if got := byTool["read_file"].Verdict; got != "allow" {
		t.Fatalf("read_file verdict = %q, want allow (the deny belongs to delete_file)", got)
	}
	if got := byTool["delete_file"].Verdict; got != "deny" {
		t.Fatalf("delete_file verdict = %q, want deny", got)
	}
}

// Without parent_seq, a decision for a shared id is a guess. The guess must not
// survive into the tape: an empty verdict lets the lab say "cannot be tied"
// instead of reading the guess as a recorded decision.
func TestUnattributableDecisionIsNotGuessed(t *testing.T) {
	tape, err := Load(writeTwoCallsUnderOneID(t, false))
	if err != nil {
		t.Fatal(err)
	}
	if len(tape.AmbiguousIDs) == 0 {
		t.Fatal("expected the shared request id to be reported as ambiguous")
	}
	for _, it := range append(append([]Interaction{}, tape.Interactions...), tape.Unanswered...) {
		if it.Verdict != "" {
			t.Fatalf("%s kept the guessed verdict %q", it.Tool, it.Verdict)
		}
	}
}
