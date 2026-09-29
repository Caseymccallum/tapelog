package policy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

const scopedPolicy = `
version: 1
default: deny
rules:
  - id: temp-allow
    tool: "deploy*"
    action: allow
    reason: "temporary deployment grant"
    expires: "2026-09-29T12:00:00Z"
  - id: task-allow
    tool: "read*"
    action: allow
    reason: "reads allowed for the migration task"
    tasks: ["migration"]
  - id: tmp-only
    tool: "write*"
    action: allow
    reason: "writes restricted to /tmp"
    where: 'context.args.path like "/tmp/*"'
  - id: no-secrets
    tool: "*"
    action: deny
    reason: "everything else is forbidden"
`

func TestExpiredRuleSkipped(t *testing.T) {
	p, err := Load(writePolicy(t, scopedPolicy))
	if err != nil {
		t.Fatal(err)
	}
	before := time.Date(2026, 9, 29, 11, 0, 0, 0, time.UTC)
	after := time.Date(2026, 9, 29, 13, 0, 0, 0, time.UTC)

	p.now = func() time.Time { return before }
	if dec := p.Evaluate(Request{Tool: "deploy_prod"}); dec.Verdict != VerdictAllow {
		t.Fatalf("before expiry: want allow, got %s", dec.Verdict)
	}
	p.now = func() time.Time { return after }
	if dec := p.Evaluate(Request{Tool: "deploy_prod"}); dec.Verdict != VerdictDeny {
		t.Fatalf("after expiry: want deny (rule skipped), got %s", dec.Verdict)
	}
}

func TestTaskScoping(t *testing.T) {
	p, err := Load(writePolicy(t, scopedPolicy))
	if err != nil {
		t.Fatal(err)
	}
	if dec := p.Evaluate(Request{Tool: "read_file", Task: "migration"}); dec.Verdict != VerdictAllow {
		t.Fatalf("matching task: want allow, got %s", dec.Verdict)
	}
	if dec := p.Evaluate(Request{Tool: "read_file", Task: "other"}); dec.Verdict != VerdictDeny {
		t.Fatalf("other task: want deny, got %s", dec.Verdict)
	}
	if dec := p.Evaluate(Request{Tool: "read_file"}); dec.Verdict != VerdictDeny {
		t.Fatalf("no task: want deny, got %s", dec.Verdict)
	}
}

func TestWhereConditions(t *testing.T) {
	p, err := Load(writePolicy(t, scopedPolicy))
	if err != nil {
		t.Fatal(err)
	}
	allow := Request{Tool: "write_file", Args: json.RawMessage(`{"path":"/tmp/x.txt"}`)}
	deny := Request{Tool: "write_file", Args: json.RawMessage(`{"path":"/etc/passwd"}`)}

	if dec := p.Evaluate(allow); dec.Verdict != VerdictAllow {
		t.Fatalf("/tmp write: want allow, got %s (%s)", dec.Verdict, dec.Reason)
	}
	if dec := p.Evaluate(deny); dec.Verdict != VerdictDeny {
		t.Fatalf("/etc write: want deny (condition failed), got %s", dec.Verdict)
	}
}

func TestInvalidWhereRejectedAtLoad(t *testing.T) {
	bad := "version: 1\nrules: [{id: x, tool: \"a*\", action: allow, where: 'context.args.path =~ '}]\n"
	if _, err := Load(writePolicy(t, bad)); err == nil {
		t.Fatal("invalid Cedar expression accepted")
	}
}

func TestCompileCedar(t *testing.T) {
	p, err := Load(writePolicy(t, scopedPolicy))
	if err != nil {
		t.Fatal(err)
	}
	out := CompileCedar(p)
	for _, want := range []string{`@id("temp-allow")`, "permit(", "forbid(", `context.tool like "deploy*"`} {
		if !strings.Contains(out, want) {
			t.Errorf("compiled Cedar missing %q:\n%s", want, out)
		}
	}
}
