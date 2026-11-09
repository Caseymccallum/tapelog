package replay

import (
	"bytes"
	"strings"
	"testing"
)

// A long-lived recording may reuse JSON-RPC request ids (a client that
// restarts its counter each conversation). Matching must be FIFO per id:
// with a last-write-wins map the earlier call was dropped and its result
// could be paired with a later request — replay would serve the wrong
// recorded outcome for a tool call.
func TestDuplicateIDsPairInOrder(t *testing.T) {
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
	if len(tape.Unanswered) != 0 {
		t.Fatalf("both calls were answered; got %d unanswered", len(tape.Unanswered))
	}
	if len(tape.Interactions) != 2 {
		t.Fatalf("both calls must be retained (no call may be dropped), got %d interactions", len(tape.Interactions))
	}
	first, second := tape.Interactions[0], tape.Interactions[1]
	if !bytes.Contains(first.Args, []byte("/a")) || !bytes.Contains(first.Result, []byte("FIRST")) {
		t.Fatalf("first call must keep its own args and result: args=%s result=%s", first.Args, first.Result)
	}
	if !bytes.Contains(second.Args, []byte("/b")) || !bytes.Contains(second.Result, []byte("SECOND")) {
		t.Fatalf("second call must keep its own args and result: args=%s result=%s", second.Args, second.Result)
	}
}

// Distinct ids must still match exactly as before (no regression from the
// FIFO change).
func TestDistinctIDsStillMatch(t *testing.T) {
	lines := []string{
		`{"type":"session/start","session_id":"s","payload":{}}`,
		`{"type":"tools/call","session_id":"s","payload":{"id":1,"tool":"read_file","args":{"path":"/a"}}}`,
		`{"type":"tools/call","session_id":"s","payload":{"id":2,"tool":"read_file","args":{"path":"/b"}}}`,
		`{"type":"tools/result","session_id":"s","payload":{"id":2,"is_error":false,"result":{"content":[{"type":"text","text":"B"}]}}}`,
		`{"type":"tools/result","session_id":"s","payload":{"id":1,"is_error":false,"result":{"content":[{"type":"text","text":"A"}]}}}`,
		`{"type":"session/end","session_id":"s","payload":{}}`,
	}
	tape, err := LoadInline(strings.NewReader(strings.Join(lines, "\n") + "\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(tape.Interactions) != 2 {
		t.Fatalf("want 2 interactions, got %d", len(tape.Interactions))
	}
	byPath := map[string]string{}
	for _, it := range tape.Interactions {
		if bytes.Contains(it.Args, []byte("/a")) {
			byPath["/a"] = string(it.Result)
		}
		if bytes.Contains(it.Args, []byte("/b")) {
			byPath["/b"] = string(it.Result)
		}
	}
	if !strings.Contains(byPath["/a"], "A") || !strings.Contains(byPath["/b"], "B") {
		t.Fatalf("results must pair with their own request id, got %v", byPath)
	}
}
