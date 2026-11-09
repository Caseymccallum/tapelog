package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Caseymccallum/tapelog/internal/session"
)

// TestReplayStructureOverrideIsVisible guards the escape hatch itself. Before
// this, --allow-invalid-structure silenced the refusal and then served the file
// with no marker at all — an empty file even printed an empty session id — so a
// forensic double was indistinguishable on stderr from a normal replay. The
// override must be visible, the way --allow-unverified already is.
func TestReplayStructureOverrideIsVisible(t *testing.T) {
	dir := t.TempDir()
	suffix := ""
	if runtime.GOOS == "windows" {
		suffix = ".exe"
	}
	binary := filepath.Join(dir, "tapelog"+suffix)
	server := filepath.Join(dir, "fixture"+suffix)
	for _, build := range []struct{ out, pkg string }{{binary, "."}, {server, "../../internal/compat/fakecmd"}} {
		cmd := exec.Command("go", "build", "-o", build.out, build.pkg)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("build: %v %s", err, out)
		}
	}

	recording := filepath.Join(dir, "session.jsonl")
	rec := exec.Command(binary, "record", "--harness", "override-test", "--log", recording, "--", server, "-era=2025-11-25")
	stdin, err := rec.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	rec.Stdout = io.Discard
	var recErr bytes.Buffer
	rec.Stderr = &recErr
	if err := rec.Start(); err != nil {
		t.Fatal(err)
	}
	for _, request := range []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"read_file","arguments":{"path":"/fictional/a.md"}}}`,
	} {
		if _, err := io.WriteString(stdin, request+"\n"); err != nil {
			t.Fatal(err)
		}
	}
	_ = stdin.Close()
	if err := rec.Wait(); err != nil {
		t.Fatalf("record: %v %s", err, recErr.String())
	}

	// Drop session/start and re-chain the remainder: this must be a structural
	// defect, not a cryptographic one, or the chain gate answers first.
	raw, err := os.ReadFile(recording)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if len(lines) < 3 || !strings.Contains(lines[0], `"session/start"`) {
		t.Fatalf("unexpected recording shape: %q", lines[0])
	}
	var events []session.Event
	for _, line := range lines[1:] {
		var e session.Event
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("event: %v", err)
		}
		events = append(events, e)
	}
	var body strings.Builder
	prev := ""
	for i := range events {
		events[i].Seq = uint64(i + 1)
		events[i].PrevHash = prev
		hash, err := events[i].ComputeHash()
		if err != nil {
			t.Fatalf("rehash: %v", err)
		}
		events[i].Hash = hash
		prev = hash
		line, _ := json.Marshal(events[i])
		body.Write(line)
		body.WriteString("\n")
	}
	startless := filepath.Join(dir, "startless.jsonl")
	if err := os.WriteFile(startless, []byte(body.String()), 0600); err != nil {
		t.Fatal(err)
	}

	// Default: refuse, and name the flag that would override it.
	refuse := exec.Command(binary, "replay", startless)
	refuse.Stdin = strings.NewReader("")
	var refuseOut bytes.Buffer
	refuse.Stdout = io.Discard
	refuse.Stderr = &refuseOut
	if err := refuse.Run(); err == nil {
		t.Fatalf("replay accepted a startless recording without an override: %s", refuseOut.String())
	}
	if !strings.Contains(refuseOut.String(), "not one complete valid session (no_start)") {
		t.Fatalf("refusal must name the defect: %s", refuseOut.String())
	}

	// Override: allowed, but loudly and with the same code.
	allow := exec.Command(binary, "replay", startless, "--allow-invalid-structure")
	allow.Stdin = strings.NewReader("")
	var allowOut bytes.Buffer
	allow.Stdout = io.Discard
	allow.Stderr = &allowOut
	if err := allow.Run(); err != nil {
		t.Fatalf("override failed: %v %s", err, allowOut.String())
	}
	if !strings.Contains(allowOut.String(), "WARNING — replaying evidence that is NOT one complete valid session (no_start)") {
		t.Fatalf("override was silent about the defect it overrode: %s", allowOut.String())
	}
}
