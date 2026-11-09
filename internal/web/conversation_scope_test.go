package web

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Caseymccallum/tapelog/internal/session"
)

// The product-level claim: with attribution present, repetition is measured per
// conversation. Four conversations that each call the same tool twice must not
// produce a session-scoped finding of eight occurrences, and the finding must
// say which scope it used.
func TestRepeatsAreScopedToConversations(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.jsonl")
	w, err := session.NewWriter(path, "scope")
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
	add(session.EventSessionStart, map[string]any{"harness": "scope"})
	for c := 1; c <= 4; c++ {
		conv := "conv-" + string(rune('0'+c))
		for i := 0; i < 2; i++ {
			call := add(session.EventToolCall, map[string]any{
				"id": c*10 + i, "tool": "read_file",
				"args": map[string]any{"path": "/x"}, "conversation_id": conv,
			})
			add(session.EventPolicyDecision, map[string]any{"id": c*10 + i, "parent_seq": call, "verdict": "allow"})
			add(session.EventToolResult, map[string]any{"id": c*10 + i, "parent_seq": call, "result": map[string]any{"text": "ok"}})
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
	a := analyseRecording(decodeRecording(data, "a.jsonl"))
	if !a.Attributed {
		t.Fatal("the recording declares conversation ids")
	}
	for _, f := range a.Repeats {
		if f.Count > 2 {
			t.Fatalf("a finding counts %d occurrences; each conversation has 2 calls (session-scoped merge)", f.Count)
		}
	}
}
