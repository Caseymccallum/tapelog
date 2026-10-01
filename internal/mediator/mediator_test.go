package mediator

import (
	"errors"
	"sync"
	"testing"

	"github.com/Caseymccallum/tapelog/internal/approval"
	"github.com/Caseymccallum/tapelog/internal/policy"
	"github.com/Caseymccallum/tapelog/internal/session"
)

// failingWriter implements EventWriter; it fails after N successful
// appends to simulate a disk going bad mid-session.
type failingWriter struct {
	mu       sync.Mutex
	allowed  int
	appends  int
	failWith error
}

func (w *failingWriter) Append(t session.EventType, payload any) (*session.Event, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.appends++
	if w.appends > w.allowed {
		return nil, w.failWith
	}
	return &session.Event{Type: t}, nil
}

func newAuditTestMediator(w EventWriter, mode string) *Mediator {
	return New(Options{
		SessionID: "audit-test",
		Evaluator: policy.AllowAll{},
		Confirmer: approval.Auto{},
		AuditMode: mode,
		Writer:    w,
	})
}

func TestAuditStrictDeniesOnceLogUnwritable(t *testing.T) {
	w := &failingWriter{allowed: 1, failWith: errors.New("disk full")}
	m := newAuditTestMediator(w, AuditStrict)

	// First call: the tools/call append succeeds (allowed=1) but the
	// decision append fails -> strict mode must not let it through.
	out := m.Decide([]byte(`1`), "read_file", []byte(`{}`))
	if out.Allowed {
		t.Fatalf("strict mode must deny when the decision cannot be recorded: %+v", out)
	}
	if out.RuleID != "audit-degraded" {
		t.Fatalf("want rule_id audit-degraded, got %q (%s)", out.RuleID, out.Reason)
	}

	// Second call: the log is already degraded -> fail closed before
	// anything else.
	out = m.Decide([]byte(`2`), "read_file", []byte(`{}`))
	if out.Allowed || out.RuleID != "audit-degraded" {
		t.Fatalf("degraded session must fail closed: %+v", out)
	}
}

func TestAuditStrictBlocksUnrecordedResult(t *testing.T) {
	w := &failingWriter{allowed: 0, failWith: errors.New("disk full")}
	m := newAuditTestMediator(w, AuditStrict)

	block := m.Result([]byte(`1`), false, []byte(`{"content":[]}`))
	if block == nil || block.Code != "audit_degraded" {
		t.Fatalf("unrecorded result must not be delivered, got %+v", block)
	}
}

func TestAuditBestEffortKeepsEnforcing(t *testing.T) {
	w := &failingWriter{allowed: 0, failWith: errors.New("disk full")}
	m := newAuditTestMediator(w, AuditBestEffort)

	out := m.Decide([]byte(`1`), "read_file", []byte(`{}`))
	if !out.Allowed {
		t.Fatalf("best-effort mode must keep enforcing after append failure: %+v", out)
	}
	if block := m.Result([]byte(`1`), false, []byte(`{"content":[]}`)); block != nil {
		t.Fatalf("best-effort must still deliver results, got %+v", block)
	}
}

func TestAuditDegradedIsSticky(t *testing.T) {
	w := &failingWriter{allowed: 0, failWith: errors.New("disk full")}
	m := newAuditTestMediator(w, AuditStrict)
	m.Decide([]byte(`1`), "read_file", []byte(`{}`))

	degraded, cause := m.auditDegraded()
	if !degraded || cause == nil {
		t.Fatalf("append failure must latch: degraded=%v cause=%v", degraded, cause)
	}
}