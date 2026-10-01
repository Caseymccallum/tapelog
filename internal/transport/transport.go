// Package transport provides MCP client-side transports: stdio
// subprocesses and streamable-HTTP endpoints. Messages are JSON-RPC
// envelopes (internal/jsonrpc), one per line on stdio.
package transport

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"sync"

	"github.com/Caseymccallum/tapelog/internal/jsonrpc"
)

// MCPVersion is the MCP protocol revision tapelog speaks (spec 2026-07-28).
// Single source of truth for the wire: the HTTP request-metadata header,
// the client `_meta`, and every serverInfo/DiscoverResult tapelog emits.
const MCPVersion = "2026-07-28"

// LegacyVersions are the handshake-based revisions a dual-era tapelog
// server negotiates on an `initialize` request (spec 2026-07-28 §Versioning:
// "An `initialize` request selects legacy semantics").
var LegacyVersions = []string{"2025-11-25", "2025-03-26", "2024-11-05"}

// NegotiateLegacy echoes a legacy client's requested version if we can
// serve it, else the modern revision. Used by dual-era servers (mux,
// replay) when answering `initialize`.
func NegotiateLegacy(requested string) string {
	for _, v := range LegacyVersions {
		if requested == v {
			return requested
		}
	}
	return MCPVersion
}

// Transport sends and receives JSON-RPC messages. Implementations must be
// safe for concurrent Send; Receive is serialized by the client.
type Transport interface {
	Send(ctx context.Context, msg *jsonrpc.Message) error
	Receive() (*jsonrpc.Message, error)
	Close() error
}

// Stdio spawns the command and speaks MCP over its stdin/stdout.
type Stdio struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Scanner
	mu     sync.Mutex
}

// NewStdio starts the command (stderr passes through to this process's
// stderr via the caller — we leave cmd.Stderr nil to inherit).
func NewStdio(command []string) (*Stdio, error) {
	if len(command) == 0 {
		return nil, fmt.Errorf("empty server command")
	}
	cmd := exec.Command(command[0], command[1:]...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %q: %w", command[0], err)
	}
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	return &Stdio{cmd: cmd, stdin: stdin, stdout: sc}, nil
}

// Send writes one message line. Honors ctx between queuing and write.
func (s *Stdio) Send(ctx context.Context, msg *jsonrpc.Message) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	b, err := jsonrpc.Marshal(msg)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err = s.stdin.Write(append(b, '\n'))
	return err
}

// Receive reads the next message line.
func (s *Stdio) Receive() (*jsonrpc.Message, error) {
	if !s.stdout.Scan() {
		if err := s.stdout.Err(); err != nil {
			return nil, err
		}
		return nil, io.EOF
	}
	return jsonrpc.Parse(s.stdout.Bytes())
}

// Close terminates the subprocess.
func (s *Stdio) Close() error {
	_ = s.stdin.Close()
	if s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
		_, _ = s.cmd.Process.Wait()
	}
	return nil
}
