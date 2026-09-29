package approval

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAutoAndDeny(t *testing.T) {
	if (Auto{}).Confirm("x", nil) != ChoiceAllowSession {
		t.Error("Auto must allow")
	}
	if (Deny{}).Confirm("x", nil) != ChoiceDeny {
		t.Error("Deny must deny")
	}
}

func TestInteractiveChoices(t *testing.T) {
	args := json.RawMessage(`{"command":"ls"}`)

	cases := []struct {
		input string
		want  Choice
	}{
		{"a\n", ChoiceAllowOnce},
		{"y\n", ChoiceAllowOnce},
		{"s\n", ChoiceAllowSession},
		{"d\n", ChoiceDeny},
		{"\n", ChoiceDeny},   // empty = default deny
		{"", ChoiceDeny},     // EOF = fail closed
		{"garbage\n", ChoiceDeny},
	}
	for _, c := range cases {
		var out strings.Builder
		in := strings.NewReader(c.input)
		got := NewInteractive(in, &out).Confirm("exec_shell", args)
		if got != c.want {
			t.Errorf("input %q: got %v, want %v", c.input, got, c.want)
		}
		if !strings.Contains(out.String(), "exec_shell") {
			t.Errorf("prompt must show the tool name; got %q", out.String())
		}
	}
}

func TestSessionAllowanceRemembered(t *testing.T) {
	var out strings.Builder
	c := NewInteractive(strings.NewReader("s\n"), &out)
	if got := c.Confirm("exec_shell", nil); got != ChoiceAllowSession {
		t.Fatalf("first call: got %v", got)
	}
	// No further input available — but the session grant must cover it.
	if got := c.Confirm("exec_shell", nil); got != ChoiceAllowSession {
		t.Fatalf("second call must inherit session grant, got %v", got)
	}
	// A different tool is NOT covered.
	if got := c.Confirm("other_tool", nil); got != ChoiceDeny {
		t.Fatalf("other tool must not inherit grant, got %v", got)
	}
}
