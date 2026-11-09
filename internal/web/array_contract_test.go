package web

// The order contract for workflowSafe (M14).
//
// An array of objects that reaches the CLI is either producer-ordered (the
// order is the information) or an unordered set (canonicalised so the output is
// byte-stable). The default is producer order, because losing order is a
// correctness bug while failing to canonicalise a set is cosmetic. What must
// never happen again is a NEW ordered array being quietly sorted because nobody
// remembered to add it to an exemption list: M12 and M13 each shipped that bug
// (groups, repeats, timeline). undeclaredArrays is the gate — every payload the
// workflows can emit is walked, and an array of objects with no declared
// meaning fails this test.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/Caseymccallum/tapelog/internal/session"
)

// contractLibrary writes a small attributed library: two conversations, a
// denial, a recorded failure, an unanswered call, an unattributed call and a
// shared request id (ambiguous correlation).
func contractLibrary(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	write := func(name string, days int, convs []string) {
		w, err := session.NewWriter(filepath.Join(dir, name), name)
		if err != nil {
			t.Fatal(err)
		}
		add := func(kind session.EventType, v any) uint64 {
			e, err := w.Append(kind, v)
			if err != nil {
				t.Fatal(err)
			}
			return e.Seq
		}
		add(session.EventSessionStart, map[string]any{"harness": "fictional", "task": "contract"})
		for i, conv := range convs {
			id := days*100 + i
			// The same tool AND arguments in every conversation, so the library
			// really does emit a recurrence array. Without it DetectRecurrence
			// returns an empty slice and the order-contract walk never sees that
			// array (M14 review F7: the gate had exactly that false negative).
			call := map[string]any{
				"id": id, "tool": "read_file", "args": map[string]any{"path": "/fictional/shared"},
			}
			if conv != "" {
				call["conversation_id"] = conv
			}
			seq := add(session.EventToolCall, call)
			verdict := "allow"
			if i == 1 {
				verdict = "deny"
			}
			add(session.EventPolicyDecision, map[string]any{
				"id": id, "parent_seq": seq, "verdict": verdict, "rule_id": "r-" + conv,
				"reason": "fictional decision",
			})
			switch {
			case verdict == "deny":
				// no result for a denied call
			case i == 2:
				add(session.EventToolResult, map[string]any{
					"id": id, "parent_seq": seq, "is_error": true,
					"result": map[string]any{"error": "fictional tool failure"},
				})
			default:
				add(session.EventToolResult, map[string]any{
					"id": id, "parent_seq": seq, "result": map[string]any{"text": "ok"},
				})
			}
		}
		add(session.EventSessionEnd, map[string]any{"reason": "fictional complete"})
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
	}
	write("a-first.jsonl", 1, []string{"conv-a", "conv-b", ""})
	write("b-second.jsonl", 2, []string{"conv-a", "conv-c", "conv-c"})
	return dir
}

func contractIndex(t *testing.T, dir string) *behaviourIndex {
	t.Helper()
	s := &server{dir: dir}
	idx, err := s.buildBehaviourIndex(behaviourFilter{})
	if err != nil {
		t.Fatal(err)
	}
	return idx
}

// Every array of objects in every payload the workflows can produce must have a
// declared meaning. A failure here lists the paths that need a decision, which
// is exactly the reminder that used to be missing.
func TestArrayOrderContract(t *testing.T) {
	dir := contractLibrary(t)
	idx := contractIndex(t, dir)
	if idx.Analysed != 2 {
		t.Fatalf("contract library should hold two analysed recordings, got %d", idx.Analysed)
	}
	ov := aggregateOverview(idx)
	// The walk can only prove what the fixture contains. Guard the arrays the
	// gate exists for, so a fixture that stops emitting one is a failure rather
	// than a silently narrower contract (M14 review F7).
	if len(ov.Recurrence) == 0 {
		t.Fatal("the fixture emits no recurrence: the contract walk cannot see that array")
	}
	if len(ov.Conversations) < 2 || len(ov.Conversations[0].Timeline) == 0 || len(ov.Conversations[0].ToolCalls) == 0 {
		t.Fatalf("the fixture emits no populated conversation: %+v", ov.Conversations)
	}
	if len(ov.Trend) == 0 || len(ov.Tools) == 0 || len(ov.Failures) == 0 {
		t.Fatal("the fixture emits an empty payload group: trend/tools/failures must be non-empty for the gate to mean anything")
	}

	// The compare delta, built the way BehaviourWorkflowCompare builds it.
	aa := idx.Sessions[0]
	bb := idx.Sessions[1]
	delta := compareBehaviour(aa, bb)

	payloads := map[string]any{
		"overview": ov,
		"delta":    delta,
	}
	// The M14 conversation-first payloads: one conversation, and a comparison.
	// They carry the same arrays at new depths (a summary nested under
	// "summary" or "comparison.a"), which is exactly what a name-keyed
	// exemption list could not distinguish.
	conv, err := ConversationWorkflow(dir, nil, "conv-a", 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	payloads["workflow-conversation"] = conv.Result
	cmp, err := CompareConversationsWorkflow(dir, nil, "conv-a", "conv-b")
	if err != nil {
		t.Fatal(err)
	}
	payloads["workflow-compare-conversations"] = cmp.Result
	// The recording comparison emits its own arrays (changed_repeats, tool_usage).
	recCmp, err := BehaviourWorkflowCompare(dir, "a-first.jsonl", "b-second.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	payloads["workflow-compare-recordings"] = recCmp.Result
	// The workflow payloads after the per-kind key pruning, which is what a CLI
	// consumer actually receives.
	for _, kind := range []string{"summary", "repeats", "failures", "conversations"} {
		raw, err := workflowSafe(ov)
		if err != nil {
			t.Fatal(err)
		}
		m := raw.(map[string]any)
		pruneForKind(m, kind)
		payloads["workflow-"+kind] = m
	}
	var all []string
	seen := map[string]bool{}
	for name, payload := range payloads {
		missing, err := undeclaredArrays(payload)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, p := range missing {
			if !seen[p] {
				seen[p] = true
				all = append(all, p+" ("+name+")")
			}
		}
	}
	sort.Strings(all)
	if len(all) > 0 {
		t.Fatalf("arrays of objects with no declared order contract: %v\n"+
			"Declare each path in arrayContracts as orderProducer or orderCanonical.", all)
	}
}

// The default is producer order: an array with no contract keeps the order it
// was produced in. This is the inversion that removes the footgun.
func TestUndeclaredArrayKeepsProducerOrder(t *testing.T) {
	type row struct {
		Label string `json:"label"`
		Seq   int    `json:"seq"`
	}
	type wrap struct {
		Unlisted []row `json:"unlisted"`
	}
	in := wrap{Unlisted: []row{{Label: "z", Seq: 9}, {Label: "a", Seq: 2}, {Label: "m", Seq: 5}}}
	v, err := workflowSafe(in)
	if err != nil {
		t.Fatal(err)
	}
	rows := v.(map[string]any)["unlisted"].([]any)
	want := []float64{9, 2, 5}
	for i, w := range want {
		if got := rows[i].(map[string]any)["seq"].(float64); got != w {
			t.Fatalf("an undeclared array must keep producer order, got %v", rows)
		}
	}
}

// A declared unordered set is canonicalised, so output built from map iteration
// stays byte-identical between runs.
func TestDeclaredCanonicalArrayIsSorted(t *testing.T) {
	type row struct {
		File   string `json:"file"`
		Reason string `json:"reason"`
	}
	type wrap struct {
		Skipped []row `json:"skipped"`
	}
	in := wrap{Skipped: []row{{File: "z.jsonl"}, {File: "a.jsonl"}}}
	v, err := workflowSafe(in)
	if err != nil {
		t.Fatal(err)
	}
	rows := v.(map[string]any)["skipped"].([]any)
	if rows[0].(map[string]any)["file"] != "a.jsonl" {
		t.Fatalf("a canonical array must be sorted, got %v", rows)
	}
}

// The contract must be complete for payloads built from real files too, not
// only for hand-made structs: this is the same walk over a fixture library's
// raw JSON, which is what a consumer reads.
func TestArrayOrderContractSurvivesJSON(t *testing.T) {
	dir := contractLibrary(t)
	idx := contractIndex(t, dir)
	raw, err := json.Marshal(aggregateOverview(idx))
	if err != nil {
		t.Fatal(err)
	}
	var generic any
	if err := json.Unmarshal(raw, &generic); err != nil {
		t.Fatal(err)
	}
	missing, err := undeclaredArrays(generic)
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) > 0 {
		t.Fatalf("undeclared arrays on the JSON wire: %v", missing)
	}
	// Sanity: the fixture really does carry what the contract exists for.
	if _, err := os.Stat(filepath.Join(dir, "a-first.jsonl")); err != nil {
		t.Fatal(err)
	}
}
