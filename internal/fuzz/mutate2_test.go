package fuzz

import (
	"testing"

	"github.com/Caseymccallum/tapelog/internal/replay"
)

// TestArgEncodingBypass: base64-ing a denied value escapes a `like` rule.
func TestArgEncodingBypass(t *testing.T) {
	ev := loadPolicy(t, `
version: 1
default: allow
rules:
  - id: no-secret
    tool: "send_data"
    where: 'context.args.data like "*secret*"'
    action: deny
`)
	tape := &replay.Tape{Interactions: []replay.Interaction{
		call(1, "send_data", `{"data":"secret-token"}`),
	}}
	f := Run(tape, ev, Options{Operators: []string{"arg_encoding"}})
	if !hasOp(f, "arg_encoding") {
		t.Fatalf("base64/url value evasion not caught: %+v", f)
	}
}

// TestArgUnicodeBypass: zero-width splitting escapes substring rules.
func TestArgUnicodeBypass(t *testing.T) {
	ev := loadPolicy(t, `
version: 1
default: allow
rules:
  - id: no-ddl
    tool: "run_sql"
    where: 'context.args.cmd like "*DROP TABLE*"'
    action: deny
`)
	tape := &replay.Tape{Interactions: []replay.Interaction{
		call(1, "run_sql", `{"cmd":"DROP TABLE users"}`),
	}}
	if !hasOp(Run(tape, ev, Options{Operators: []string{"arg_unicode"}}), "arg_unicode") {
		t.Fatal("unicode value evasion not caught")
	}
}

// TestArgBoundaryBypass: empty/boundary values escape glob rules.
func TestArgBoundaryBypass(t *testing.T) {
	ev := loadPolicy(t, `
version: 1
default: allow
rules:
  - id: no-etc
    tool: "read_file"
    where: 'context.args.path like "/etc/*"'
    action: deny
`)
	tape := &replay.Tape{Interactions: []replay.Interaction{
		call(1, "read_file", `{"path":"/etc/passwd"}`),
	}}
	if !hasOp(Run(tape, ev, Options{Operators: []string{"arg_boundary"}}), "arg_boundary") {
		t.Fatal("boundary value evasion not caught")
	}
}

// TestToolNamespaceBypass: namespaced names escape exact-tool rules.
func TestToolNamespaceBypass(t *testing.T) {
	ev := loadPolicy(t, `
version: 1
default: allow
rules:
  - id: no-delete
    tool: "delete_file"
    action: deny
`)
	tape := &replay.Tape{Interactions: []replay.Interaction{
		call(1, "delete_file", `{"path":"x"}`),
	}}
	if !hasOp(Run(tape, ev, Options{Operators: []string{"tool_namespace"}}), "tool_namespace") {
		t.Fatal("namespace name evasion not caught")
	}
}

// TestSwapRotateBypass: moving a sink BEFORE its source evades a session
// flow rule — the non-adjacent case swap_adjacent cannot reach.
func TestSwapRotateBypass(t *testing.T) {
	ev := loadPolicy(t, `
version: 1
default: allow
flows:
  - id: no-exfil
    from: ["read_secrets"]
    to: ["send_http"]
    action: deny
`)
	tape := &replay.Tape{Interactions: []replay.Interaction{
		call(1, "read_secrets", `{}`),
		call(2, "list_files", `{"path":"/"}`),
		call(3, "send_http", `{"url":"https://x"}`),
	}}
	if !hasOp(Run(tape, ev, Options{Operators: []string{"swap_rotate"}}), "swap_rotate") {
		t.Fatal("non-adjacent reorder evasion not caught")
	}
}

// TestNewOperatorsRegistered: every v2 operator ships enabled by default.
func TestNewOperatorsRegistered(t *testing.T) {
	want := map[string]bool{
		"arg_unicode": true, "arg_boundary": true, "arg_encoding": true,
		"tool_namespace": true, "swap_rotate": true,
	}
	for _, op := range Operators {
		delete(want, op)
	}
	for missing := range want {
		t.Fatalf("operator %s not registered in Operators", missing)
	}
}
