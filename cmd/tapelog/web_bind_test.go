package main

import (
	"bytes"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestWebDoesNotAnnounceBeforeBinding guards a diagnostic that lied: the command
// printed "session dashboard on http://..." and only then called ListenAndServe,
// so a busy port produced a success-looking URL followed by a failure — and any
// script grepping for the URL got the address of a server that never ran.
func TestWebDoesNotAnnounceBeforeBinding(t *testing.T) {
	dir := t.TempDir()
	suffix := ""
	if runtime.GOOS == "windows" {
		suffix = ".exe"
	}
	binary := filepath.Join(dir, "tapelog"+suffix)
	build := exec.Command("go", "build", "-o", binary, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	recordings := filepath.Join(dir, "recordings")
	if err := os.MkdirAll(recordings, 0700); err != nil {
		t.Fatal(err)
	}

	// Hold a port so the server cannot bind.
	held, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = held.Close() }()
	addr := held.Addr().String()

	cmd := exec.Command(binary, "web", "--dir", recordings, "--listen", addr)
	cmd.Stdin = strings.NewReader("")
	var out bytes.Buffer
	cmd.Stdout = io.Discard
	cmd.Stderr = &out
	err = cmd.Run()
	if err == nil {
		t.Fatalf("web started on a port that was already held (%s): %s", addr, out.String())
	}
	if strings.Contains(out.String(), "session dashboard on") {
		t.Fatalf("the dashboard URL was announced before the bind succeeded:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "bind") && !strings.Contains(out.String(), "Only one usage") {
		t.Fatalf("the failure should name the bind problem:\n%s", out.String())
	}
}
