package approval

import (
	"io"
	"os"
	"runtime"
)

// OpenTerminal opens the controlling terminal for prompt input, or returns
// nil when there is none (pipes, CI). The MCP transport owns stdin, so the
// prompt reads the TTY directly: /dev/tty on Unix, CONIN$ on Windows.
func OpenTerminal() io.ReadCloser {
	path := "/dev/tty"
	if runtime.GOOS == "windows" {
		path = "CONIN$"
	}
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	return f
}

// NewInteractiveFromTerminal builds an Interactive confirmer on the real
// terminal (prompt output goes to stderr, which the MCP transport does not
// own). ok=false when no terminal is available.
func NewInteractiveFromTerminal() (*Interactive, bool) {
	tty := OpenTerminal()
	if tty == nil {
		return nil, false
	}
	return NewInteractive(tty, os.Stderr), true
}
