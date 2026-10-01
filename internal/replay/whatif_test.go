package replay

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Caseymccallum/tapelog/internal/policy"
)

// TestWhatIfReproducesValueTaintVerdicts is the parity contract for
// policy regression testing: what-if must replay value-level taint
// (contamination from recorded results), not just per-call rules — or a
// live deny on a contaminated sink would look like a verdict "change".
func TestWhatIfReproducesValueTaintVerdicts(t *testing.T) {
	policyPath := filepath.Join(t.TempDir(), "p.yaml")
	if err := os.WriteFile(policyPath, []byte(`
version: 1
default: allow
flows:
  - id: no-secret-exfil
    from: ["read_secrets"]
    to: ["send_*"]
    mode: value
    action: deny
    reason: "secret values must not reach a sink"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := policy.Load(policyPath)
	if err != nil {
		t.Fatal(err)
	}

	// Recorded session: source yields the secret, contaminated sink was
	// denied live (rule no-secret-exfil), clean sink allowed.
	tape := &Tape{
		Interactions: []Interaction{
			{Order: 1, Tool: "read_secrets", Args: json.RawMessage(`{}`),
				Result: json.RawMessage(`{"content":[{"type":"text","text":"sk-CANARY-7f3a9b"}]}`),
				Verdict: "allow", RuleID: "default"},
			{Order: 3, Tool: "send_http", Args: json.RawMessage(`{"url":"https://x","body":"hello"}`),
				Result: json.RawMessage(`{"content":[]}`), Verdict: "allow", RuleID: "default"},
		},
		Unanswered: []Interaction{
			{Order: 2, Tool: "send_http", Args: json.RawMessage(`{"url":"https://x","body":"sk-CANARY-7f3a9b"}`),
				Verdict: "deny", RuleID: "no-secret-exfil"},
		},
	}

	var out bytes.Buffer
	if changed := WhatIf(&out, tape, p, policyPath, ""); changed != 0 {
		t.Fatalf("verdicts must reproduce exactly, %d changed:\n%s", changed, out.String())
	}

	// Sanity: dropping the taint (clean session) must flip the deny —
	// the test would be vacuous if the flow rule never fired.
	clean := *tape
	clean.Interactions = tape.Interactions[:1]
	clean.Unanswered = []Interaction{{Order: 2, Tool: "send_http",
		Args: json.RawMessage(`{"url":"https://x","body":"hello"}`), Verdict: "deny", RuleID: "no-secret-exfil"}}
	out.Reset()
	if changed := WhatIf(&out, &clean, p, policyPath, ""); changed != 1 {
		t.Fatalf("clean sink should flip to allow (1 changed), got %d:\n%s", changed, out.String())
	}
}