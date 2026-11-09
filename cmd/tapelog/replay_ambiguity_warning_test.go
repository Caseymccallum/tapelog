package main

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Caseymccallum/tapelog/internal/session"
)

// TestReplayAmbiguityOverrideIsVisible guards the second escape hatch. The
// refusal for a shared, still-outstanding request id says replay "will not
// present a guess as the recorded outcome — use --allow-ambiguous-ids to replay
// anyway", and the flag exists to permit exactly that guess. Before this test
// the flag was the only override that served evidence silently: a caller piping
// replay into an agent harness saw a normal replay, with no sign that a result
// may have been paired with the wrong call.
func TestReplayAmbiguityOverrideIsVisible(t *testing.T) {
	dir := t.TempDir()
	suffix := ""
	if runtime.GOOS == "windows" {
		suffix = ".exe"
	}
	binary := filepath.Join(dir, "tapelog"+suffix)
	if out, err := exec.Command("go", "build", "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}

	// Two calls outstanding under one id, answered in arrival order: nothing in
	// the transport ties either result to either call.
	recording := filepath.Join(dir, "ambiguous.jsonl")
	w, err := session.NewWriter(recording, "ambiguity-override-test")
	if err != nil {
		t.Fatal(err)
	}
	events := []struct {
		kind    session.EventType
		payload any
	}{
		{session.EventSessionStart, map[string]any{"harness": "ambiguity-override-test"}},
		{session.EventToolCall, map[string]any{"id": 1, "tool": "read_file", "args": map[string]any{"path": "/fictional/a.md"}}},
		{session.EventToolCall, map[string]any{"id": 1, "tool": "read_file", "args": map[string]any{"path": "/fictional/b.md"}}},
		{session.EventToolResult, map[string]any{"id": 1, "is_error": false,
			"result": map[string]any{"content": []any{map[string]any{"type": "text", "text": "FIRST"}}}}},
		{session.EventToolResult, map[string]any{"id": 1, "is_error": false,
			"result": map[string]any{"content": []any{map[string]any{"type": "text", "text": "SECOND"}}}}},
		{session.EventSessionEnd, map[string]any{"reason": "client disconnect"}},
	}
	for _, e := range events {
		if _, err := w.Append(e.kind, e.payload); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	// Default: refuse, and name the flag that would override it.
	refuse := exec.Command(binary, "replay", recording)
	refuse.Stdin = strings.NewReader("")
	refuse.Stdout = io.Discard
	var refuseOut bytes.Buffer
	refuse.Stderr = &refuseOut
	if err := refuse.Run(); err == nil {
		t.Fatalf("replay served an ambiguous recording without an override: %s", refuseOut.String())
	}
	if !strings.Contains(refuseOut.String(), "cannot be determined from the transport") ||
		!strings.Contains(refuseOut.String(), "--allow-ambiguous-ids") {
		t.Fatalf("refusal must name the defect and the override: %s", refuseOut.String())
	}

	// Override: allowed, but it must say what it is serving.
	allow := exec.Command(binary, "replay", recording, "--allow-ambiguous-ids")
	allow.Stdin = strings.NewReader("")
	allow.Stdout = io.Discard
	var allowOut bytes.Buffer
	allow.Stderr = &allowOut
	if err := allow.Run(); err != nil {
		t.Fatalf("override failed: %v %s", err, allowOut.String())
	}
	if !strings.Contains(allowOut.String(), "WARNING — replaying evidence with ambiguous request id(s) 1") {
		t.Fatalf("override was silent about the ambiguity it overrode: %s", allowOut.String())
	}
	_ = os.Remove(recording)
}
