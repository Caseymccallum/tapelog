package replay

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Caseymccallum/tapelog/internal/session"
)

// buildBlobTapelog writes a log whose args AND result are blob
// references resolved from the on-disk store (spec §6.2).
func buildBlobTapelog(t *testing.T) (string, *session.BlobStore) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "session.jsonl")
	blobs := session.NewBlobStore(session.DefaultBlobDir(path))
	w, err := session.NewWriter(path, "blob-test")
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	bigArgs := json.RawMessage(`{"body":"` + strings.Repeat("a", 256) + `"}`)
	bigResult := json.RawMessage(`{"text":"` + strings.Repeat("b", 256) + `"}`)
	argsRef, err := blobs.Offload(bigArgs, 64)
	if err != nil {
		t.Fatal(err)
	}
	resultRef, err := blobs.Offload(bigResult, 64)
	if err != nil {
		t.Fatal(err)
	}
	w.Append(session.EventSessionStart, session.SessionStartPayload{Harness: "test"})
	w.Append(session.EventToolCall, session.ToolCallPayload{
		ID: json.RawMessage(`1`), Tool: "send_http", Args: argsRef,
	})
	w.Append(session.EventPolicyDecision, session.PolicyDecisionPayload{ID: json.RawMessage(`1`), Verdict: "allow", RuleID: "ok", Reason: "ok"})
	w.Append(session.EventToolResult, session.ToolResultPayload{
		ID: json.RawMessage(`1`), Result: resultRef,
	})
	return path, blobs
}

// TestLoadResolvesBlobRefs: replay needs the blob store — placeholders
// are resolved to content (for matching, what-if, assertions), and a
// missing/tampered store is a load error, never silently-passed refs.
func TestLoadResolvesBlobRefs(t *testing.T) {
	path, _ := buildBlobTapelog(t)

	tape, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(tape.Interactions) != 1 {
		t.Fatalf("want 1 interaction, got %d", len(tape.Interactions))
	}
	got := tape.Interactions[0]
	if session.IsBlobRef(got.Args) || session.IsBlobRef(got.Result) {
		t.Fatalf("Load must resolve blob refs, got args=%s result=%s", got.Args, got.Result)
	}
	if !strings.Contains(string(got.Result), "bbbb") {
		t.Fatalf("resolved result must carry blob content, got %s", got.Result)
	}

	// Store removed: fail loud, naming the blob store.
	// (Rename the store dir out of the way.)
	storeDir := session.DefaultBlobDir(path)
	if err := os.Rename(storeDir, storeDir+".gone"); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "replay needs the blob store") {
		t.Fatalf("missing store must fail loud, got %v", err)
	}
}