package web

// M14 review F5: opening a conversation must not hide the recording-level
// evidence warnings. A conversation is the same evidence as the library, sliced,
// so a broken chain, a missing session/end, unpaired results or unknown-source
// decisions must be visible where the conversation is read — not only on the
// overview.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Caseymccallum/tapelog/internal/session"
)

func writeConversationFixture(t *testing.T, dir, name, conv string, complete bool) string {
	t.Helper()
	path := filepath.Join(dir, name)
	w, err := session.NewWriter(path, name)
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
	add(session.EventSessionStart, map[string]any{"harness": "fictional"})
	seq := add(session.EventToolCall, map[string]any{
		"id": 1, "tool": "read_file", "args": map[string]any{"path": "/fictional"}, "conversation_id": conv,
	})
	add(session.EventPolicyDecision, map[string]any{"id": 1, "parent_seq": seq, "verdict": "allow"})
	add(session.EventToolResult, map[string]any{"id": 1, "parent_seq": seq, "result": map[string]any{"text": "ok"}})
	if complete {
		add(session.EventSessionEnd, map[string]any{"reason": "fictional complete"})
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func evidenceFor(t *testing.T, dir, conv string) string {
	t.Helper()
	r, err := ConversationWorkflow(dir, nil, conv, 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	m := r.Result.(map[string]any)
	lines, _ := m["evidence"].([]any)
	var out []string
	for _, line := range lines {
		if s, ok := line.(string); ok {
			out = append(out, s)
		}
	}
	return strings.Join(out, "\n")
}

func TestConversationCarriesRecordingLevelWarnings(t *testing.T) {
	dir := t.TempDir()
	writeConversationFixture(t, dir, "incomplete.jsonl", "conv-live", false)
	tampered := writeConversationFixture(t, dir, "tampered.jsonl", "conv-tampered", true)
	// Corrupt one event's recorded hash: structure stays whole, chain does not.
	data, err := os.ReadFile(tampered)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) < 3 {
		t.Fatal("fixture too short to corrupt")
	}
	if idx := strings.Index(lines[2], `"hash":"`); idx >= 0 {
		hashStart := idx + len(`"hash":"`)
		flipped := []byte(lines[2])
		if flipped[hashStart] == 'a' {
			flipped[hashStart] = 'b'
		} else {
			flipped[hashStart] = 'a'
		}
		lines[2] = string(flipped)
	}
	if err := os.WriteFile(tampered, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if got := evidenceFor(t, dir, "conv-live"); !strings.Contains(got, "no session/end") {
		t.Fatalf("an incomplete recording's conversation must say so, got %q", got)
	}
	if got := evidenceFor(t, dir, "conv-tampered"); !strings.Contains(got, "hash-chain") {
		t.Fatalf("a broken chain's conversation must say so, got %q", got)
	}
	// A healthy conversation carries no recording-level warning.
	healthyDir := t.TempDir()
	writeConversationFixture(t, healthyDir, "healthy.jsonl", "conv-ok", true)
	got := evidenceFor(t, healthyDir, "conv-ok")
	if strings.Contains(got, "hash-chain") || strings.Contains(got, "no session/end") {
		t.Fatalf("a healthy recording's conversation must not carry a warning, got %q", got)
	}
}
