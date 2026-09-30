// Package approval implements the human-in-the-loop step for `confirm`
// policy verdicts: allow once, allow for the rest of the session, or deny.
//
// The prompt is a git-style line prompt read from the controlling terminal
// — the MCP transport owns stdin/stdout, so the prompt must not touch them.
// Design note (vs. STACK.md's huh suggestion): a line prompt is more robust
// than a TUI inside a proxy hot path and matches git's established UX.
package approval

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
)

// Choice is the human's answer for one confirmation.
type Choice int

const (
	ChoiceDeny Choice = iota
	ChoiceAllowOnce
	ChoiceAllowSession
)

// Confirmer resolves a `confirm` verdict. Implementations must be safe
// for concurrent use.
type Confirmer interface {
	Confirm(tool string, args json.RawMessage) Choice
}

// ExplainedConfirmer is an optional Confirmer that also returns a reason
// suffix for the audit log (e.g. "expired in the approval queue").
type ExplainedConfirmer interface {
	ConfirmExplain(tool string, args json.RawMessage) (Choice, string)
}

// Auto always allows (the --auto-confirm mode). Everything is recorded.
type Auto struct{}

// Confirm implements Confirmer.
func (Auto) Confirm(string, json.RawMessage) Choice { return ChoiceAllowSession }

// Deny always denies — used in non-interactive sessions without
// --auto-confirm (fail-closed).
type Deny struct{}

// Confirm implements Confirmer.
func (Deny) Confirm(string, json.RawMessage) Choice { return ChoiceDeny }

// Interactive prompts on the terminal. "allow session" decisions are
// remembered per tool for the rest of the session.
type Interactive struct {
	in      io.Reader
	out     io.Writer
	mu      sync.Mutex
	session map[string]bool // tool -> allowed for session
}

// NewInteractive creates an Interactive confirmer over the given terminal.
func NewInteractive(in io.Reader, out io.Writer) *Interactive {
	return &Interactive{in: in, out: out, session: map[string]bool{}}
}

// Confirm implements Confirmer.
func (c *Interactive) Confirm(tool string, args json.RawMessage) Choice {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.session[tool] {
		return ChoiceAllowSession
	}

	fmt.Fprintf(c.out, "\ntapelog: confirmation required\n")
	fmt.Fprintf(c.out, "  tool: %s\n", tool)
	fmt.Fprintf(c.out, "  args: %s\n", compact(args))
	fmt.Fprintf(c.out, "  [a]llow once / allow [s]ession / [d]eny (default: deny): ")

	reader := bufio.NewReader(c.in)
	line, err := reader.ReadString('\n')
	if err != nil && line == "" {
		fmt.Fprintln(c.out)
		return ChoiceDeny // no input available: fail closed
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "a", "allow", "y", "yes":
		fmt.Fprintln(c.out, "  -> allowed (once)")
		return ChoiceAllowOnce
	case "s", "session":
		c.session[tool] = true
		fmt.Fprintln(c.out, "  -> allowed (for the rest of this session)")
		return ChoiceAllowSession
	default:
		fmt.Fprintln(c.out, "  -> denied")
		return ChoiceDeny
	}
}

// compact renders args single-line for the prompt.
func compact(raw json.RawMessage) string {
	s := strings.Join(strings.Fields(string(raw)), " ")
	if len(s) > 120 {
		s = s[:117] + "..."
	}
	return s
}
