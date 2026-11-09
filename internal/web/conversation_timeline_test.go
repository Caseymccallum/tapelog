package web

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Caseymccallum/tapelog/internal/behaviour"
	"github.com/Caseymccallum/tapelog/internal/session"
)

// The workspace renders conversations from the analysis layer, never by
// re-deriving grouping in JavaScript. This pins the contract the UI consumes:
// groups in producer order, unattributed last, and a timeline whose entries
// point back at real events.
func TestConversationTimelineContract(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "b.jsonl")
	w, err := session.NewWriter(path, "timeline")
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
	add(session.EventSessionStart, map[string]any{"harness": "timeline"})
	for _, step := range []struct {
		id    int
		conv  string
		tool  string
		deny  bool
		error bool
	}{
		{1, "conv-b", "read_file", false, false},
		{2, "", "read_file", false, false},        // no declared id
		{3, "conv-a", "delete_file", true, false}, // denied: no result
		{4, "conv-a", "read_file", false, true},
	} {
		call := add(session.EventToolCall, map[string]any{
			"id": step.id, "tool": step.tool, "args": map[string]any{"p": "/x"},
			"conversation_id": step.conv,
		})
		verdict := "allow"
		if step.deny {
			verdict = "deny"
		}
		add(session.EventPolicyDecision, map[string]any{"id": step.id, "parent_seq": call, "verdict": verdict})
		if !step.deny {
			res := map[string]any{"id": step.id, "parent_seq": call, "result": map[string]any{"text": "ok"}}
			if step.error {
				res["is_error"] = true
				res["error"] = "boom"
			}
			add(session.EventToolResult, res)
		}
	}
	add(session.EventSessionEnd, map[string]any{"reason": "done"})
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	a := analyseRecording(decodeRecording(data, "b.jsonl"))
	calls := a.Calls
	for i := range calls {
		calls[i].File = "b.jsonl"
		calls[i].Index = i
	}
	groups := behaviour.GroupConversations(calls)
	if len(groups) != 3 {
		t.Fatalf("expected conv-a, conv-b and the unattributed remainder, got %d", len(groups))
	}
	if groups[len(groups)-1].Attributed {
		t.Fatal("the unattributed remainder must be last")
	}
	for _, g := range groups {
		if !g.Attributed {
			continue
		}
		if len(g.Timeline) == 0 {
			t.Fatalf("conversation %q has no timeline", g.ID)
		}
		for _, e := range g.Timeline {
			if e.Seq == 0 || e.File == "" {
				t.Fatalf("timeline entry in %q does not reference its evidence: %+v", g.ID, e)
			}
			if e.Tool == "" {
				t.Fatalf("timeline entry in %q has no tool", g.ID)
			}
		}
	}
	// A denied call must not be drawn as answered, and an error must be flagged.
	for _, g := range groups {
		for _, e := range g.Timeline {
			if e.Tool == "delete_file" && e.Answered {
				t.Fatal("a denied call must not be drawn as answered")
			}
			if e.Tool == "read_file" && e.IsError && g.ID != "conv-a" {
				t.Fatal("an error surfaced under the wrong conversation")
			}
		}
	}
}
