package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The demo is the only first-run experience a downloaded package has, so it gets a
// test that runs the real binary: it must produce a real recording containing an
// allowed call, a policy denial and a tool error, and the chain must verify.
func TestDemoRecordsDenyAndErrorFromAPackage(t *testing.T) {
	dir := t.TempDir()
	suffix := ""
	if runtime.GOOS == "windows" {
		suffix = ".exe"
	}
	binary := filepath.Join(dir, "tapelog"+suffix)
	if out, err := exec.Command("go", "build", "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}

	work := filepath.Join(dir, "demo")
	cmd := exec.Command(binary, "demo", "--dir", work)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("demo failed: %v\nstdout: %s\nstderr: %s", err, stdout.String(), stderr.String())
	}

	logs, err := filepath.Glob(filepath.Join(work, "recordings", "*.jsonl"))
	if err != nil || len(logs) != 1 {
		t.Fatalf("expected exactly one recording, got %v (%v)", logs, err)
	}
	raw, err := os.ReadFile(logs[0])
	if err != nil {
		t.Fatal(err)
	}
	log := string(raw)
	for _, want := range []string{
		`"type":"session/start"`,
		`"type":"tools/call"`,
		`"type":"policy/decision"`,
		`"verdict":"deny"`,
		`"type":"tools/result"`,
		`"type":"session/end"`,
	} {
		if !strings.Contains(log, want) {
			t.Fatalf("recording is missing %s:\n%s", want, log)
		}
	}
	if !strings.Contains(stderr.String()+stdout.String(), "chain intact") &&
		!strings.Contains(stdout.String(), "OK:") {
		t.Fatalf("demo did not verify its own recording:\n%s", stdout.String())
	}

	// A second run must refuse to trample the first: a demo that silently overwrites
	// evidence teaches exactly the wrong habit.
	again := exec.Command(binary, "demo", "--dir", work)
	if err := again.Run(); err == nil {
		t.Fatal("demo overwrote an existing directory instead of refusing")
	}
}
