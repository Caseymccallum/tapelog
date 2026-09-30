package fuzz

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Caseymccallum/tapelog/internal/policy"
	"github.com/Caseymccallum/tapelog/internal/replay"
)

func loadPolicy(t *testing.T, yamlText string) policy.Evaluator {
	t.Helper()
	p := filepath.Join(t.TempDir(), "p.yaml")
	if err := os.WriteFile(p, []byte(yamlText), 0o600); err != nil {
		t.Fatal(err)
	}
	ev, err := policy.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	return ev
}

func call(order int, tool, args string) replay.Interaction {
	return replay.Interaction{Order: order, Tool: tool, Args: json.RawMessage(args)}
}

func hasOp(findings []Finding, op string) bool {
	for _, f := range findings {
		if f.Operator == op {
			return true
		}
	}
	return false
}

func TestToolNameBypasses(t *testing.T) {
	// Exact-name rule: every name mutation escapes it.
	ev := loadPolicy(t, `
version: 1
default: allow
rules:
  - id: no-del
    tool: "delete_file"
    action: deny
    reason: "no deletes"
`)
	tape := &replay.Tape{Interactions: []replay.Interaction{call(1, "delete_file", `{}`)}}
	findings := Run(tape, ev, Options{})
	for _, op := range []string{"tool_case", "tool_space", "tool_homoglyph", "tool_traversal"} {
		if !hasOp(findings, op) {
			t.Errorf("expected a %s bypass finding, got %+v", op, findings)
		}
	}
	for _, f := range findings {
		if f.WasVerdict != "deny" || f.NowVerdict == "deny" || f.RuleID != "no-del" {
			t.Fatalf("malformed finding: %+v", f)
		}
	}
}

func TestGlobHoldsAgainstTraversal(t *testing.T) {
	// A wildcard suffix catches traversal-prefixed names ("../delete_file"
	// is held). The suffix variant "delete_file/.." escapes — a real rule
	// robustness gap the fuzzer reports (and this test documents).
	ev := loadPolicy(t, `
version: 1
default: allow
rules:
  - id: no-del
    tool: "*delete_file"
    action: deny
    reason: "no deletes"
`)
	tape := &replay.Tape{Interactions: []replay.Interaction{call(1, "delete_file", `{}`)}}
	findings := Run(tape, ev, Options{Operators: []string{"tool_traversal"}})
	for _, f := range findings {
		if strings.Contains(f.Mutation, "traversal prefix") {
			t.Fatalf("prefix must be held by the wildcard: %+v", f)
		}
	}
}

func TestArgTraversalBypass(t *testing.T) {
	// Prefix-shaped conditions can be escaped with ../ indirection.
	ev := loadPolicy(t, `
version: 1
default: allow
rules:
  - id: no-etc
    tool: "read_file"
    where: 'context.args.path like "/etc/*"'
    action: deny
    reason: "etc is off limits"
`)
	tape := &replay.Tape{Interactions: []replay.Interaction{
		call(1, "read_file", `{"path":"/etc/passwd"}`),
	}}
	findings := Run(tape, ev, Options{Operators: []string{"arg_traversal"}})
	if !hasOp(findings, "arg_traversal") {
		t.Fatalf("expected arg_traversal bypass (like-pattern evasion), got %+v", findings)
	}
}

func TestSwapEvadesFlowRule(t *testing.T) {
	// Flow rule forbids read_secrets -> send_http. Swapping the order
	// makes the sink happen before the taint — the flow rule no longer
	// fires. That is a real evasion and must be reported.
	ev := loadPolicy(t, `
version: 1
default: allow
flows:
  - id: no-exfil
    from: ["read_secrets"]
    to: ["send_http"]
    action: deny
    reason: "secrets must not leave"
`)
	tape := &replay.Tape{Interactions: []replay.Interaction{
		call(1, "read_secrets", `{}`),
		call(2, "send_http", `{"url":"https://x"}`),
	}}
	base := verdicts(tape.Interactions, ev)
	if base[1] != "deny" {
		t.Fatalf("flow rule should deny the sink in order, got %v", base)
	}
	findings := Run(tape, ev, Options{Operators: []string{"swap_adjacent"}})
	if !hasOp(findings, "swap_adjacent") {
		t.Fatalf("expected swap_adjacent evasion finding, got %+v", findings)
	}
}
