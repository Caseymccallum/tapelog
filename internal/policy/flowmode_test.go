package policy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func loadFlowPolicy(t *testing.T, yamlText string) *Policy {
	t.Helper()
	p := filepath.Join(t.TempDir(), "p.yaml")
	if err := os.WriteFile(p, []byte(yamlText), 0o600); err != nil {
		t.Fatal(err)
	}
	pol, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	return pol
}

const valueFlowYAML = `
version: 1
default: allow
flows:
  - id: no-exfil-value
    from: ["read_secrets"]
    to: ["send_*"]
    mode: value
    action: deny
    reason: "secret values must not reach a sink"
`

func TestValueFlowNeedsContamination(t *testing.T) {
	pol := loadFlowPolicy(t, valueFlowYAML)
	if !pol.HasValueFlows() {
		t.Fatal("HasValueFlows should be true")
	}
	var state TaintState

	// Session taint alone must NOT trip a value-mode rule (precision win).
	state.Record("read_secrets")
	if _, applied := pol.CheckFlow(&state, "send_http"); applied {
		t.Fatal("value-mode rule must not fire on session taint alone")
	}

	// Contamination by the rule's source DOES trip it.
	dec, applied := pol.CheckFlowValues(&state, "send_http", []string{"read_secrets"})
	if !applied || dec.Verdict != VerdictDeny {
		t.Fatalf("value rule should deny contaminated sink, got %+v applied=%v", dec, applied)
	}
	if !strings.Contains(dec.Reason, "contaminated by: read_secrets") {
		t.Fatalf("reason should name the source: %q", dec.Reason)
	}

	// Contamination by an unrelated source does not.
	if _, applied := pol.CheckFlowValues(&state, "send_http", []string{"fetch_page"}); applied {
		t.Fatal("unrelated contamination must not trip the rule")
	}
	// Non-sink tools are unaffected.
	if _, applied := pol.CheckFlowValues(&state, "write_file", []string{"read_secrets"}); applied {
		t.Fatal("non-sink must not trip the rule")
	}
}

func TestSessionModeUnchanged(t *testing.T) {
	pol := loadFlowPolicy(t, `
version: 1
default: allow
flows:
  - id: no-exfil-session
    from: ["read_secrets"]
    to: ["send_*"]
    action: deny
`)
	var state TaintState
	if _, applied := pol.CheckFlowValues(&state, "send_http", []string{"read_secrets"}); applied {
		t.Fatal("session-mode rule must not be consulted by CheckFlowValues")
	}
	state.Record("read_secrets")
	dec, applied := pol.CheckFlow(&state, "send_http")
	if !applied || dec.Verdict != VerdictDeny {
		t.Fatalf("session rule should fire on taint, got %+v", dec)
	}
	if !strings.Contains(dec.Reason, "taint sources: read_secrets") {
		t.Fatalf("session reason wording: %q", dec.Reason)
	}
}

func TestBadFlowModeRejected(t *testing.T) {
	p := filepath.Join(t.TempDir(), "p.yaml")
	text := strings.Replace(valueFlowYAML, "mode: value", "mode: quantum", 1)
	if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p); err == nil || !strings.Contains(err.Error(), "mode") {
		t.Fatalf("bad mode must fail with a clear error, got %v", err)
	}
}
