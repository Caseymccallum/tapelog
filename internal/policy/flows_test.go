package policy

import (
	"strings"
	"testing"
)

const flowsPolicy = `
version: 1
default: deny
rules:
  - id: allow-tools
    tool: ["read*", "send*", "exec*"]
    action: allow
    reason: "tools permitted"
flows:
  - id: no-exfil
    from: ["read_file", "query_db"]
    to: ["send_*"]
    action: deny
    reason: "file/db data must not be sent anywhere"
  - id: confirm-exec-taint
    from: ["read_secrets*"]
    to: ["exec*"]
    action: confirm
    reason: "running commands after reading secrets needs a human"
`

func loadFlowsPolicy(t *testing.T) *Policy {
	t.Helper()
	p, err := Load(writePolicy(t, flowsPolicy))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestFlowDeniesAfterSource(t *testing.T) {
	p := loadFlowsPolicy(t)
	state := &TaintState{}

	// Before any source runs, the sink is governed by regular rules only.
	if dec, applied := p.CheckFlow(state, "send_http"); applied {
		t.Fatalf("flow must not apply before taint: %+v", dec)
	}

	state.Record("read_file")
	dec, applied := p.CheckFlow(state, "send_http")
	if !applied {
		t.Fatal("flow must apply after read_file ran")
	}
	if dec.Verdict != VerdictDeny || dec.RuleID != "no-exfil" {
		t.Fatalf("bad flow decision: %+v", dec)
	}
	if !strings.Contains(dec.Reason, "read_file") {
		t.Fatalf("reason must name taint sources: %q", dec.Reason)
	}
}

func TestFlowIgnoresNonMatchingPairs(t *testing.T) {
	p := loadFlowsPolicy(t)
	state := &TaintState{}
	state.Record("read_file")

	// exec is a sink only for read_secrets* taint — not for read_file.
	if _, applied := p.CheckFlow(state, "exec_shell"); applied {
		t.Fatal("exec must not be restricted by read_file taint")
	}
	// send after a non-source tool taint is not restricted.
	state2 := &TaintState{}
	state2.Record("list_dir")
	if _, applied := p.CheckFlow(state2, "send_http"); applied {
		t.Fatal("non-source taint must not trigger flows")
	}
}

func TestFlowConfirmAction(t *testing.T) {
	p := loadFlowsPolicy(t)
	state := &TaintState{}
	state.Record("read_secrets")

	dec, applied := p.CheckFlow(state, "exec_shell")
	if !applied || dec.Verdict != VerdictConfirm || dec.RuleID != "confirm-exec-taint" {
		t.Fatalf("want confirm flow, got %+v (applied=%v)", dec, applied)
	}
}

func TestFlowPrecedenceOverRules(t *testing.T) {
	// The record path applies flows first: allow-tools would allow
	// send_http, but the flow denies it after taint.
	p := loadFlowsPolicy(t)
	state := &TaintState{}
	state.Record("read_file")

	if dec, applied := p.CheckFlow(state, "send_http"); !applied || dec.Verdict != VerdictDeny {
		t.Fatalf("flow should decide before regular rules: %+v", dec)
	}
	if dec := p.Evaluate(Request{Tool: "send_http"}); dec.Verdict != VerdictAllow {
		t.Fatalf("sanity: regular rules alone would allow: %+v", dec)
	}
}

func TestInvalidFlowsRejected(t *testing.T) {
	bad := []string{
		"version: 1\nflows: [{id: f, from: [a], to: [b], action: allow}]\n", // bad action
		"version: 1\nflows: [{id: f, from: [a], action: deny}]\n",           // missing to
		"version: 1\nflows: [{id: f, to: [b], action: deny}]\n",             // missing from
	}
	for i, content := range bad {
		if _, err := Load(writePolicy(t, content)); err == nil {
			t.Errorf("case %d: invalid flow accepted", i)
		}
	}
}
