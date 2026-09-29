//go:build linux

package sandbox

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestLandlockEnforcement re-execs the test binary as a helper that
// applies the sandbox to itself and probes file access. Skips when the
// kernel lacks Landlock support.
func TestLandlockEnforcement(t *testing.T) {
	if os.Getenv("SANDBOX_HELPER") == "1" {
		dir := os.Getenv("SANDBOX_HELPER_DIR")
		if err := (Options{ReadOnly: []string{dir}}).Restrict(); err != nil {
			// Kernel without landlock: report and exit cleanly.
			println("restrict-error:", err.Error())
			os.Exit(0)
		}
		if _, err := os.ReadFile(filepath.Join(dir, "allowed.txt")); err != nil {
			println("allowed-failed")
		} else {
			println("allowed-ok")
		}
		if _, err := os.ReadFile(os.Getenv("SANDBOX_HELPER_DENIED")); err != nil {
			println("denied-blocked")
		} else {
			println("denied-LEAKED")
		}
		os.Exit(0)
	}

	home := os.Getenv("HOME")
	if home == "" {
		t.Skip("no HOME for the denied probe")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "allowed.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	denied := filepath.Join(home, "cassette-sandbox-denied-probe.txt")
	if err := os.WriteFile(denied, []byte("y"), 0o600); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(denied)

	cmd := exec.Command(os.Args[0], "-test.run", "TestLandlockEnforcement")
	cmd.Env = append(os.Environ(),
		"SANDBOX_HELPER=1",
		"SANDBOX_HELPER_DIR="+dir,
		"SANDBOX_HELPER_DENIED="+denied,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("helper failed: %v\n%s", err, out)
	}
	text := string(out)
	switch {
	case strings.Contains(text, "restrict-error"):
		t.Skip("landlock unavailable on this kernel")
	case strings.Contains(text, "denied-LEAKED"):
		t.Fatalf("sandbox leaked access:\n%s", text)
	case !strings.Contains(text, "allowed-ok") || !strings.Contains(text, "denied-blocked"):
		t.Fatalf("unexpected helper output:\n%s", text)
	}
}
