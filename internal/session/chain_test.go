package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixedClock() time.Time {
	return time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
}

// writeTestLog builds a small valid log and returns its path.
func writeTestLog(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "session.jsonl")
	w, err := NewWriter(path, "test-session")
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	w.SetClock(fixedClock)

	if _, err := w.Append(EventSessionStart, SessionStartPayload{Harness: "test", PolicyID: "observe"}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Append(EventToolCall, ToolCallPayload{ID: json.RawMessage(`1`), Tool: "read_file", Args: json.RawMessage(`{"path":"/tmp/a"}`)}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Append(EventPolicyDecision, PolicyDecisionPayload{ID: json.RawMessage(`1`), Verdict: "allow", RuleID: "rule-1", Reason: "matched"}); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestVerifyIntact(t *testing.T) {
	path := writeTestLog(t)
	res, err := VerifyFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK() {
		t.Fatalf("intact log failed verification: seq %d: %s", res.FirstBadSeq, res.Problem)
	}
	if res.Events != 3 {
		t.Fatalf("want 3 events, got %d", res.Events)
	}
}

// A live session must survive its log being rewritten underneath it:
// the writer holds no long-lived handle, so a mid-session tamper can
// neither lock it out nor strand later appends on an orphaned handle.
// Events appended after the tamper must land in the same file, and the
// chain must expose the edit at the exact event it happened.
func TestWriterSurvivesExternalEditMidSession(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	w, err := NewWriter(path, "live-session")
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	w.SetClock(fixedClock)

	for i := 0; i < 2; i++ {
		if _, err := w.Append(EventToolCall, ToolCallPayload{ID: json.RawMessage(`1`), Tool: "read_file", Args: json.RawMessage(`{"path":"/tmp/a"}`)}); err != nil {
			t.Fatal(err)
		}
	}

	// External tamper mid-session: an editor rewrites the file (this is
	// possible at all only because the writer closes between appends).
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	lines[0] = strings.Replace(lines[0], "read_file", "delete_all", 1)
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// The session continues: the next append must succeed and be present.
	if _, err := w.Append(EventSessionEnd, SessionEndPayload{Reason: "client disconnect"}); err != nil {
		t.Fatalf("append after external edit must not fail: %v", err)
	}
	out, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "client disconnect") {
		t.Fatal("post-tamper append did not land in the log file")
	}

	// And the chain pinpoints where the file stopped being ours.
	res, err := VerifyFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if res.OK() {
		t.Fatal("log tampered mid-session verified OK")
	}
	if res.FirstBadSeq != 1 {
		t.Fatalf("want first bad seq 1, got %d (%s)", res.FirstBadSeq, res.Problem)
	}
}

func TestVerifyDetectsTamper(t *testing.T) {
	path := writeTestLog(t)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")

	// Tamper with the payload of the second event.
	tampered := strings.Replace(lines[1], "/tmp/a", "/etc/shadow", 1)
	if tampered == lines[1] {
		t.Fatal("tamper did not change the line")
	}
	lines[1] = tampered
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	res, err := VerifyFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if res.OK() {
		t.Fatal("tampered log verified OK")
	}
	if res.FirstBadSeq != 2 {
		t.Fatalf("want first bad seq 2, got %d (%s)", res.FirstBadSeq, res.Problem)
	}
}

func TestVerifyDetectsDeletion(t *testing.T) {
	path := writeTestLog(t)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")

	// Delete the middle event: seq contiguity and chain linkage both break.
	out := strings.Join([]string{lines[0], lines[2]}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(out), 0o600); err != nil {
		t.Fatal(err)
	}

	res, err := VerifyFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if res.OK() {
		t.Fatal("log with deleted event verified OK")
	}
	if res.FirstBadSeq != 3 {
		t.Fatalf("want first bad seq 3, got %d (%s)", res.FirstBadSeq, res.Problem)
	}
}

func TestVerifyDetectsReorder(t *testing.T) {
	path := writeTestLog(t)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")

	// Swap events 2 and 3 (both hash-valid but linked in the wrong order).
	out := strings.Join([]string{lines[0], lines[2], lines[1]}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(out), 0o600); err != nil {
		t.Fatal(err)
	}

	res, err := VerifyFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if res.OK() {
		t.Fatal("reordered log verified OK")
	}
}

func TestVerifyDetectsRewrittenChain(t *testing.T) {
	// A determined attacker rewrites a line AND its hash: the prev_hash of
	// the NEXT event must still expose the edit.
	path := writeTestLog(t)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")

	var e Event
	if err := json.Unmarshal([]byte(lines[1]), &e); err != nil {
		t.Fatal(err)
	}
	e.Payload = json.RawMessage(`{"id":1,"tool":"delete_all","args":{}}`)
	newHash, err := e.ComputeHash()
	if err != nil {
		t.Fatal(err)
	}
	e.Hash = newHash
	rewritten, _ := json.Marshal(e)
	lines[1] = string(rewritten)
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	res, err := VerifyFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if res.OK() {
		t.Fatal("rewritten event with recomputed hash verified OK")
	}
	if res.FirstBadSeq != 3 {
		t.Fatalf("break should surface at the following event (seq 3), got %d (%s)", res.FirstBadSeq, res.Problem)
	}
}
