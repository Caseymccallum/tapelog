package replay

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Caseymccallum/tapelog/internal/session"
)

func buildRecording(t *testing.T, events []struct {
	Kind    session.EventType
	Payload any
}) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "rec.jsonl")
	w, err := session.NewWriter(path, "ambiguity-test")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range events {
		if _, err := w.Append(e.Kind, e.Payload); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

type ev = struct {
	Kind    session.EventType
	Payload any
}

// Two calls outstanding under one request id: the transport carries nothing
// that says which response answers which call, so the recording is marked
// ambiguous rather than paired by assumption.
func TestOverlappingDuplicateIDsAreAmbiguous(t *testing.T) {
	lines := []string{
		`{"type":"session/start","session_id":"s","payload":{}}`,
		`{"type":"tools/call","session_id":"s","payload":{"id":1,"tool":"read_file","args":{"path":"/a"}}}`,
		`{"type":"tools/call","session_id":"s","payload":{"id":1,"tool":"read_file","args":{"path":"/b"}}}`,
		`{"type":"tools/result","session_id":"s","payload":{"id":1,"is_error":false,"result":{"content":[{"type":"text","text":"FIRST"}]}}}`,
		`{"type":"tools/result","session_id":"s","payload":{"id":1,"is_error":false,"result":{"content":[{"type":"text","text":"SECOND"}]}}}`,
		`{"type":"session/end","session_id":"s","payload":{}}`,
	}
	tape, err := LoadInline(strings.NewReader(strings.Join(lines, "\n") + "\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(tape.AmbiguousIDs) != 1 || tape.AmbiguousIDs[0] != "1" {
		t.Fatalf("want id 1 marked ambiguous, got %v", tape.AmbiguousIDs)
	}
}

// Legal sequential reuse — the first call was answered before the second
// began — is unambiguous and must keep working.
func TestSequentialReuseIsNotAmbiguous(t *testing.T) {
	lines := []string{
		`{"type":"session/start","session_id":"s","payload":{}}`,
		`{"type":"tools/call","session_id":"s","payload":{"id":1,"tool":"read_file","args":{"path":"/a"}}}`,
		`{"type":"tools/result","session_id":"s","payload":{"id":1,"is_error":false,"result":{"content":[{"type":"text","text":"FIRST"}]}}}`,
		`{"type":"tools/call","session_id":"s","payload":{"id":1,"tool":"read_file","args":{"path":"/b"}}}`,
		`{"type":"tools/result","session_id":"s","payload":{"id":1,"is_error":false,"result":{"content":[{"type":"text","text":"SECOND"}]}}}`,
		`{"type":"session/end","session_id":"s","payload":{}}`,
	}
	tape, err := LoadInline(strings.NewReader(strings.Join(lines, "\n") + "\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(tape.AmbiguousIDs) != 0 {
		t.Fatalf("sequential reuse must not be ambiguous, got %v", tape.AmbiguousIDs)
	}
	if len(tape.Interactions) != 2 {
		t.Fatalf("both calls must be served, got %d", len(tape.Interactions))
	}
}

// Replay refuses an ambiguous recording by default: serving it would present
// a guess as the recorded outcome. The flag is the explicit escape hatch.
func TestReplayRefusesAmbiguousRecordingByDefault(t *testing.T) {
	path := buildRecording(t, []ev{
		{session.EventSessionStart, map[string]any{"task": "fictional"}},
		{session.EventToolCall, map[string]any{"id": 1, "tool": "read_file", "args": map[string]any{"path": "/a"}}},
		{session.EventToolCall, map[string]any{"id": 1, "tool": "read_file", "args": map[string]any{"path": "/b"}}},
		{session.EventToolResult, map[string]any{"id": 1, "is_error": false, "result": map[string]any{"text": "FIRST"}}},
		{session.EventToolResult, map[string]any{"id": 1, "is_error": false, "result": map[string]any{"text": "SECOND"}}},
		{session.EventSessionEnd, map[string]any{"reason": "complete"}},
	})

	if _, _, err := LoadVerifiedAttributable(path, false, false, false); err == nil {
		t.Fatal("an ambiguous recording must be refused by default")
	} else if !strings.Contains(err.Error(), "--allow-ambiguous-ids") {
		t.Fatalf("the refusal must name the escape hatch, got %v", err)
	}

	tape, _, err := LoadVerifiedAttributable(path, false, false, true)
	if err != nil {
		t.Fatalf("the escape hatch must load it: %v", err)
	}
	if len(tape.AmbiguousIDs) != 1 {
		t.Fatalf("the tape must still record the ambiguity, got %v", tape.AmbiguousIDs)
	}
	if len(tape.Interactions) != 2 {
		t.Fatalf("both calls must remain present, got %d", len(tape.Interactions))
	}
}

// A normal recording with unique ids is unaffected.
func TestReplayAcceptsUnambiguousRecording(t *testing.T) {
	path := buildRecording(t, []ev{
		{session.EventSessionStart, map[string]any{"task": "fictional"}},
		{session.EventToolCall, map[string]any{"id": 1, "tool": "read_file", "args": map[string]any{"path": "/a"}}},
		{session.EventToolResult, map[string]any{"id": 1, "is_error": false, "result": map[string]any{"text": "A"}}},
		{session.EventSessionEnd, map[string]any{"reason": "complete"}},
	})
	tape, _, err := LoadVerifiedAttributable(path, false, false, false)
	if err != nil {
		t.Fatalf("unambiguous recording must load: %v", err)
	}
	if len(tape.AmbiguousIDs) != 0 || len(tape.Interactions) != 1 {
		t.Fatalf("unexpected tape: %+v", tape)
	}
	// sanity: the stored result is JSON we can read back
	var probe map[string]any
	if err := json.Unmarshal(tape.Interactions[0].Result, &probe); err != nil {
		t.Fatalf("result must be valid JSON: %v", err)
	}
}
