package policy

import (
	"os"
	"path/filepath"
	"testing"
)

func writePolicy(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "policy.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

const samplePolicy = `
version: 1
default: deny
rules:
  - id: allow-reads
    tool: ["read*", "list*", "search?"]
    action: allow
    reason: "reads are safe"
  - id: confirm-exec
    tool: "exec*"
    action: confirm
    reason: "shell execution needs confirmation"
  - id: deny-delete
    tool: "*delete*"
    action: deny
    reason: "deletion is forbidden"
`

func TestEvaluateFirstMatchWins(t *testing.T) {
	p, err := Load(writePolicy(t, samplePolicy))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		tool    string
		want    Verdict
		ruleID  string
		isDef   bool
	}{
		{"read_file", VerdictAllow, "allow-reads", false},
		{"list_dir", VerdictAllow, "allow-reads", false},
		{"search1", VerdictAllow, "allow-reads", false},  // ? matches exactly one char
		{"search12", VerdictDeny, "default", true},       // ? does not match two chars
		{"exec_shell", VerdictConfirm, "confirm-exec", false},
		{"delete_file", VerdictDeny, "deny-delete", false},
		{"write_file", VerdictDeny, "default", true},     // no match -> fail-closed
	}
	for _, c := range cases {
		dec := p.Evaluate(Request{Tool: c.tool})
		if dec.Verdict != c.want {
			t.Errorf("%s: verdict %s, want %s", c.tool, dec.Verdict, c.want)
		}
		if dec.RuleID != c.ruleID {
			t.Errorf("%s: rule %s, want %s", c.tool, dec.RuleID, c.ruleID)
		}
		if dec.IsDefault != c.isDef {
			t.Errorf("%s: IsDefault=%v, want %v", c.tool, dec.IsDefault, c.isDef)
		}
		if dec.Reason == "" {
			t.Errorf("%s: empty reason; decisions must be explainable", c.tool)
		}
	}
}

func TestDefaultAllowWhenConfigured(t *testing.T) {
	p, err := Load(writePolicy(t, "version: 1\ndefault: allow\nrules: []\n"))
	if err != nil {
		t.Fatal(err)
	}
	if dec := p.Evaluate(Request{Tool: "anything"}); dec.Verdict != VerdictAllow {
		t.Fatalf("want allow, got %s", dec.Verdict)
	}
}

func TestDefaultIsFailClosed(t *testing.T) {
	p, err := Load(writePolicy(t, "version: 1\nrules: []\n"))
	if err != nil {
		t.Fatal(err)
	}
	if dec := p.Evaluate(Request{Tool: "anything"}); dec.Verdict != VerdictDeny {
		t.Fatalf("omitted default must be deny, got %s", dec.Verdict)
	}
}

func TestLoadRejectsInvalidPolicies(t *testing.T) {
	bad := []string{
		"version: 2\nrules: []\n",                              // wrong version
		"version: 1\nrules: [{id: x, tool: t, action: nuke}]\n", // bad action
		"version: 1\nrules: [{id: x, action: allow}]\n",         // no tool patterns
		"version: 1\ndefault: maybe\nrules: []\n",               // bad default
	}
	for i, content := range bad {
		if _, err := Load(writePolicy(t, content)); err == nil {
			t.Errorf("case %d: invalid policy accepted", i)
		}
	}
}

func TestRuleIDsAutoAssigned(t *testing.T) {
	p, err := Load(writePolicy(t, "version: 1\nrules: [{tool: \"a*\", action: allow}]\n"))
	if err != nil {
		t.Fatal(err)
	}
	if dec := p.Evaluate(Request{Tool: "abc"}); dec.RuleID != "rule-1" {
		t.Fatalf("want auto id rule-1, got %s", dec.RuleID)
	}
}

func TestAllowAllObserveMode(t *testing.T) {
	var e Evaluator = AllowAll{}
	dec := e.Evaluate(Request{Tool: "anything"})
	if dec.Verdict != VerdictAllow || dec.RuleID != "observe" {
		t.Fatalf("unexpected observe-mode decision: %+v", dec)
	}
}
