package mediator

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Caseymccallum/tapelog/internal/approval"
	"github.com/Caseymccallum/tapelog/internal/policy"
	"github.com/Caseymccallum/tapelog/internal/session"
	"github.com/Caseymccallum/tapelog/internal/taint"
)

// TestMalformedCallRecorded pins the evidence contract for boundary
// rejections: a malformed tools/call produces a tools/call event (tool
// name empty — none was extractable, redacted raw params as args) and a
// deny decision naming the rule, so the rejection is visible to
// inspect/verify like every other boundary outcome.
func TestMalformedCallRecorded(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "s.jsonl")
	w, err := session.NewWriter(logPath, "malformed-test")
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	m := New(Options{
		SessionID: "malformed-test",
		Evaluator: policy.AllowAll{},
		Confirmer: approval.Auto{},
		Writer:    w,
	})

	m.MalformedCall([]byte(`9`), json.RawMessage(`"not-an-object"`),
		errors.New("decode tools/call params: cannot unmarshal string into Go value of type jsonrpc.ToolCallParams"))
	// Real garbage: raw text with quotes and a secret. The evidence event
	// must still marshal (valid JSON — hand-quoting would break it) and
	// the secret must still be redacted.
	m.MalformedCall([]byte(`10`), []byte(`{"jsonrpc":"2.0","method":"tools/call","token":"ghp_abcdefghijklmnopqrstuvwxyz0123456789AB"`),
		errors.New("parse JSON-RPC message: unexpected end of JSON input"))

	events := readEvents(t, logPath)
	if len(events) != 4 {
		t.Fatalf("want 2x (tools/call + policy/decision), got %d events", len(events))
	}
	var callP session.ToolCallPayload
	if err := json.Unmarshal(events[0].Payload, &callP); err != nil {
		t.Fatal(err)
	}
	if callP.Tool != "" {
		t.Fatalf("malformed call must record an empty tool name, got %q", callP.Tool)
	}
	if len(callP.Args) == 0 {
		t.Fatal("raw params must be recorded as evidence")
	}
	var decP session.PolicyDecisionPayload
	if err := json.Unmarshal(events[1].Payload, &decP); err != nil {
		t.Fatal(err)
	}
	if decP.Verdict != "deny" || decP.RuleID != "malformed-request" {
		t.Fatalf("want deny/malformed-request, got %s/%s", decP.Verdict, decP.RuleID)
	}
	if !strings.Contains(decP.Reason, "never forwarded") {
		t.Fatalf("reason must state the boundary action, got %q", decP.Reason)
	}
	// Causation: the decision points back at its tools/call event like
	// any ordinary mediated call (spec §6.1).
	if decP.ParentSeq == nil || *decP.ParentSeq != events[0].Seq {
		t.Fatalf("decision parent_seq must be the call event seq %d, got %v", events[0].Seq, decP.ParentSeq)
	}

	// The quote-laden garbage evidence: valid JSON, redacted, causally
	// linked to ITS call event.
	if !json.Valid(events[2].Payload) {
		t.Fatalf("garbage evidence must still be valid JSON, got %s", events[2].Payload)
	}
	if strings.Contains(string(events[2].Payload), "ghp_") {
		t.Fatalf("secret in garbage must be redacted, got %s", events[2].Payload)
	}
	var dec2 session.PolicyDecisionPayload
	if err := json.Unmarshal(events[3].Payload, &dec2); err != nil {
		t.Fatal(err)
	}
	if dec2.ParentSeq == nil || *dec2.ParentSeq != events[2].Seq {
		t.Fatalf("second decision parent_seq must be %d, got %v", events[2].Seq, dec2.ParentSeq)
	}
}

// TestValueFlowConservativeUnderUnresolvedSource pins the taint-timing
// contract: when a source call is still in flight at the result-wait
// deadline, a value-mode flow degrades to conservative semantics — the
// unresolved source is ASSUMED to taint the sink. The precision model
// must never become "quietly allow when we don't know yet".
func TestValueFlowConservativeUnderUnresolvedSource(t *testing.T) {
	policyPath := filepath.Join(t.TempDir(), "p.yaml")
	body := `version: 1
default: allow
flows:
  - id: no-secret-exfil
    from: read_secrets
    to: "send_*"
    mode: value
    action: deny
    reason: secret values must not reach a sink
`
	if err := writeFile(policyPath, body); err != nil {
		t.Fatal(err)
	}
	p, err := policy.Load(policyPath)
	if err != nil {
		t.Fatal(err)
	}

	logPath := filepath.Join(t.TempDir(), "s.jsonl")
	w, err := session.NewWriter(logPath, "taint-test")
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	m := New(Options{
		SessionID:  "taint-test",
		Evaluator:  p,
		Confirmer:  approval.Auto{},
		Writer:     w,
		Values:     taint.New(0, 0),
		ResultWait: 30 * time.Millisecond,
	})

	// Source allowed but still running (no result reported yet).
	if out := m.Decide([]byte(`1`), "read_secrets", []byte(`{}`)); !out.Allowed {
		t.Fatalf("source must be allowed: %+v", out)
	}

	// Sink with CLEAN args arrives while the source is unresolved:
	// the value rule must still fire (conservative degradation).
	out := m.Decide([]byte(`2`), "send_http", []byte(`{"body":"benign request"}`))
	if out.Allowed {
		t.Fatalf("sink must be blocked while the source is unresolved: %+v", out)
	}
	if out.RuleID != "no-secret-exfil" {
		t.Fatalf("want the value flow rule, got %q", out.RuleID)
	}
	if !strings.Contains(out.Reason, "read_secrets") {
		t.Fatalf("reason must name the assumed source, got %q", out.Reason)
	}

	// Control: once the source's result is recorded, a CLEAN sink is
	// allowed again (precision holds when the facts are in).
	if blk := m.Result([]byte(`1`), false, []byte(`{"text":"sk-SECRET-VALUE-1"}`)); blk != nil {
		t.Fatalf("result delivery must not be blocked: %+v", blk)
	}
	out = m.Decide([]byte(`3`), "send_http", []byte(`{"body":"benign request"}`))
	if !out.Allowed {
		t.Fatalf("clean sink must be allowed once the source resolved: %+v", out)
	}

	// ...and the contaminated sink is still caught on the merits.
	out = m.Decide([]byte(`4`), "send_http", []byte(`{"body":"sk-SECRET-VALUE-1"}`))
	if out.Allowed {
		t.Fatalf("contaminated sink must be denied: %+v", out)
	}
}

// writeFile writes a small fixture file (test helper).
func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o600)
}