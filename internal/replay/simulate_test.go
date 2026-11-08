package replay

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/Caseymccallum/tapelog/internal/policy"
)

func simulatePolicy(t *testing.T, src string) *policy.Policy {
	t.Helper()
	p, err := policy.Parse([]byte(src), "candidate")
	if err != nil {
		t.Fatalf("candidate: %v", err)
	}
	return p
}

// Simulate must classify every recorded call and agree with the CLI what-if
// count, because both read the same re-evaluation. Like the CLI, it compares
// verdicts: a rule-id change with the same verdict is not a behaviour change.
func TestSimulateClassifiesChangesLikeTheCLI(t *testing.T) {
	tape := &Tape{
		Interactions: []Interaction{
			{Order: 1, Tool: "read_file", Args: json.RawMessage(`{"path":"/tmp/a"}`), Result: json.RawMessage(`{"ok":true}`), Verdict: "allow", RuleID: "default"},
			{Order: 3, Tool: "send_email", Args: json.RawMessage(`{"to":"x"}`), Result: json.RawMessage(`{}`), Verdict: "allow", RuleID: "default"},
			{Order: 4, Tool: "delete_user", Args: json.RawMessage(`{}`), Result: json.RawMessage(`{}`), Verdict: "deny", RuleID: "legacy"},
			{Order: 5, Tool: "delete_all", Args: json.RawMessage(`{}`), Result: json.RawMessage(`{}`), Verdict: "deny", RuleID: "legacy"},
		},
		Unanswered: []Interaction{
			{Order: 2, Tool: "delete_all", Args: json.RawMessage(`{}`), Verdict: "allow", RuleID: "default"},
		},
	}
	p := simulatePolicy(t, `version: 1
default: allow
rules:
  - id: block-delete
    tool: "delete_all"
    action: deny
  - id: approve-send
    tool: "send_*"
    action: confirm
`)
	sim := Simulate(tape, p, "")
	if sim.Total != 5 || sim.Same != 2 || sim.Changed != 3 {
		t.Fatalf("counts: %+v", sim)
	}
	if sim.NewlyDenied != 1 || sim.NewlyConfirm != 1 || sim.NewlyAllowed != 1 || sim.OtherChanges != 0 {
		t.Fatalf("classification: %+v", sim)
	}
	byOrder := map[int]SimulatedCall{}
	for _, c := range sim.Calls {
		byOrder[c.Order] = c
	}
	if got := byOrder[2]; got.Transition != "allow->deny" || !got.Changed || got.Answered || got.CandidateRule != "block-delete" {
		t.Fatalf("order 2: %+v", got)
	}
	if got := byOrder[3]; got.Transition != "allow->confirm" || got.CandidateReason == "" {
		t.Fatalf("order 3: %+v", got)
	}
	if got := byOrder[4]; got.Transition != "deny->allow" {
		t.Fatalf("order 4: %+v", got)
	}
	// Same verdict, different rule id: no behaviour change is claimed.
	if got := byOrder[5]; got.Changed || got.CandidateRule != "block-delete" || got.RecordedRule != "legacy" {
		t.Fatalf("order 5: %+v", got)
	}
	if got := byOrder[1]; got.Changed || !got.Answered {
		t.Fatalf("order 1: %+v", got)
	}
	// Ordering follows the recording, not the input slices.
	for i, want := range []int{1, 2, 3, 4, 5} {
		if sim.Calls[i].Order != want {
			t.Fatalf("order at %d: %+v", i, sim.Calls)
		}
	}

	var out bytes.Buffer
	changed := WhatIf(&out, tape, p, "candidate", "")
	if changed != sim.Changed {
		t.Fatalf("whatif reported %d changes, simulate reported %d", changed, sim.Changed)
	}
}

// Value-level taint must be reproduced by Simulate, or a live deny on a
// contaminated sink would look like a change.
func TestSimulateReproducesValueTaint(t *testing.T) {
	p := simulatePolicy(t, `version: 1
default: allow
flows:
  - id: no-secret-exfil
    from: ["read_secrets"]
    to: ["send_*"]
    mode: value
    action: deny
`)
	tape := &Tape{
		Interactions: []Interaction{
			{Order: 1, Tool: "read_secrets", Args: json.RawMessage(`{}`),
				Result: json.RawMessage(`{"content":[{"text":"sk-CANARY-7f3a9b"}]}`), Verdict: "allow", RuleID: "default"},
		},
		Unanswered: []Interaction{
			{Order: 2, Tool: "send_http", Args: json.RawMessage(`{"body":"sk-CANARY-7f3a9b"}`), Verdict: "deny", RuleID: "no-secret-exfil"},
		},
	}
	sim := Simulate(tape, p, "")
	if sim.Changed != 0 {
		t.Fatalf("verdicts must reproduce exactly: %+v", sim.Calls)
	}
	if sim.Calls[1].CandidateRule != "no-secret-exfil" {
		t.Fatalf("taint was not rebuilt: %+v", sim.Calls[1])
	}
}
