package policy

import (
	"testing"

	"github.com/Caseymccallum/tapelog/internal/taint"
)

// A value whose producer could not be tracked might have come from any source.
// Treating it as clean would let the exact exfiltration this rule exists to
// stop pass through, so an unknown source satisfies the rule's `from` set.
func TestUnknownSourceSatisfiesValueFlow(t *testing.T) {
	p := loadFlowPolicy(t, `version: 1
default: allow
flows:
  - id: no-secret-exfil
    from: ["read_secrets"]
    to: ["send_*"]
    mode: value
    action: deny
    reason: secret values must not reach a sink
`)
	var state TaintState

	dec, applied := p.CheckFlowValues(&state, "send_http", []string{taint.UnknownSource})
	if !applied {
		t.Fatal("an unknown source must apply a value-flow rule whose from-set is non-empty")
	}
	if dec.Verdict != VerdictDeny {
		t.Fatalf("want deny, got %q", dec.Verdict)
	}

	// Control: an unrelated, known source does not trigger the rule.
	if _, applied := p.CheckFlowValues(&state, "send_http", []string{"read_file"}); applied {
		t.Fatal("a known unrelated source must not trigger the rule")
	}
	// Control: no contamination at all stays clean.
	if _, applied := p.CheckFlowValues(&state, "send_http", nil); applied {
		t.Fatal("no contamination must not trigger the rule")
	}
}
