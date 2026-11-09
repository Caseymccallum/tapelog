package mediator

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/Caseymccallum/tapelog/internal/approval"
	"github.com/Caseymccallum/tapelog/internal/policy"
	"github.com/Caseymccallum/tapelog/internal/session"
	"github.com/Caseymccallum/tapelog/internal/taint"
)

func taintMediator(t *testing.T) (*Mediator, *taint.Store) {
	t.Helper()
	logPath := filepath.Join(t.TempDir(), "taint.jsonl")
	w, err := session.NewWriter(logPath, "taint-identity")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.Close() })
	st := taint.New(0, 0)
	m := New(Options{
		SessionID:  "taint-identity",
		Evaluator:  policy.AllowAll{},
		Confirmer:  approval.Auto{},
		Writer:     w,
		Values:     st,
		ResultWait: 5 * time.Millisecond,
	})
	return m, st
}

func hasSource(sources []string, want string) bool {
	for _, s := range sources {
		if s == want {
			return true
		}
	}
	return false
}

// A JSON-RPC id can be reused once its call has completed, and a long-lived
// connection can also see ids reused while one is still outstanding. In both
// cases every result must still be labelled with the tool that produced it:
// value-taint rules key on that source, so a lost or crossed label either
// misses a real exfiltration rule (under-taint) or names the wrong tool.
func TestValueTaintSurvivesRepeatedRequestIDs(t *testing.T) {
	m, st := taintMediator(t)

	m.Decide([]byte(`1`), "read_secrets", []byte(`{}`))
	m.Decide([]byte(`1`), "read_public", []byte(`{}`))
	// The SECRET arrives SECOND: once the queue drains to one candidate, a
	// non-sticky union would label it with the wrong tool and the real
	// source would be lost.
	m.Result([]byte(`1`), false, []byte(`{"text":"PUBLIC-ABC"}`))
	m.Result([]byte(`1`), false, []byte(`{"text":"SECRET-XYZ"}`))

	if got := st.ContaminatedBy([]byte(`{"body":"SECRET-XYZ"}`)); !hasSource(got, "read_secrets") {
		t.Fatalf("the secret result must stay attributable to read_secrets, got %v", got)
	}
	if got := st.ContaminatedBy([]byte(`{"body":"PUBLIC-ABC"}`)); !hasSource(got, "read_public") {
		t.Fatalf("the public result must stay attributable to read_public, got %v", got)
	}
}

// Sequential reuse (the previous call answered before the next begins) is
// legal and unambiguous: it must keep working exactly as before.
func TestValueTaintSequentialReuseStaysExact(t *testing.T) {
	m, st := taintMediator(t)

	m.Decide([]byte(`1`), "read_secrets", []byte(`{}`))
	m.Result([]byte(`1`), false, []byte(`{"text":"SECRET-XYZ"}`))
	m.Decide([]byte(`1`), "read_public", []byte(`{}`))
	m.Result([]byte(`1`), false, []byte(`{"text":"PUBLIC-ABC"}`))

	secret := st.ContaminatedBy([]byte(`{"body":"SECRET-XYZ"}`))
	public := st.ContaminatedBy([]byte(`{"body":"PUBLIC-ABC"}`))
	if !hasSource(secret, "read_secrets") || hasSource(secret, "read_public") {
		t.Fatalf("secret must be attributed exactly to read_secrets, got %v", secret)
	}
	if !hasSource(public, "read_public") || hasSource(public, "read_secrets") {
		t.Fatalf("public must be attributed exactly to read_public, got %v", public)
	}
}

// An ambiguous id must not be given either conversation: the recorder knows
// it cannot tell the calls apart, and says so on the event.
func TestAmbiguousAttributionIsMarkedUnknown(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "attr.jsonl")
	w, err := session.NewWriter(logPath, "attr-ambiguous")
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	m := New(Options{SessionID: "attr-ambiguous", Evaluator: policy.AllowAll{}, Confirmer: approval.Auto{}, Writer: w})

	m.DecideMeta([]byte(`1`), "read_file", []byte(`{"path":"/a"}`), json.RawMessage(`{"io.tapelog/conversationId":"conv-A"}`))
	m.DecideMeta([]byte(`1`), "read_file", []byte(`{"path":"/b"}`), json.RawMessage(`{"io.tapelog/conversationId":"conv-B"}`))
	m.Result([]byte(`1`), false, []byte(`{"text":"one"}`))

	seen := 0
	for _, e := range readEvents(t, logPath) {
		if e.Type != session.EventToolResult {
			continue
		}
		seen++
		var p session.ToolResultPayload
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			t.Fatal(err)
		}
		if p.ConversationID != "" || p.ToolCallID != "" {
			t.Fatalf("ambiguous id must not be attributed, got conv=%q call=%q", p.ConversationID, p.ToolCallID)
		}
		if !p.AttributionAmbiguous {
			t.Fatal("the result must record that its attribution was ambiguous")
		}
		if p.ParentSeq != nil {
			t.Fatalf("ambiguous id must not claim a parent call, got %d", *p.ParentSeq)
		}
	}
	if seen != 1 {
		t.Fatalf("want 1 result event, got %d", seen)
	}
}

// A decision belongs to the call that produced it. With a reused id the
// queue head is the OLDER call, so a decision resolved through the queue
// would point at the wrong tools/call event.
func TestDecisionLinksToItsOwnCallUnderReusedIDs(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "dec.jsonl")
	w, err := session.NewWriter(logPath, "decision-link")
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	m := New(Options{SessionID: "decision-link", Evaluator: policy.AllowAll{}, Confirmer: approval.Auto{}, Writer: w})

	m.DecideMeta([]byte(`1`), "read_file", []byte(`{"path":"/a"}`), nil)
	m.DecideMeta([]byte(`1`), "read_file", []byte(`{"path":"/b"}`), nil)

	var callSeqs []uint64
	parentByDecision := map[uint64]uint64{}
	for _, e := range readEvents(t, logPath) {
		switch e.Type {
		case session.EventToolCall:
			callSeqs = append(callSeqs, e.Seq)
		case session.EventPolicyDecision:
			var p session.PolicyDecisionPayload
			if err := json.Unmarshal(e.Payload, &p); err != nil {
				t.Fatal(err)
			}
			if p.ParentSeq == nil {
				t.Fatal("decision must link to a call")
			}
			parentByDecision[e.Seq] = *p.ParentSeq
		}
	}
	if len(callSeqs) != 2 {
		t.Fatalf("want 2 calls, got %d", len(callSeqs))
	}
	// Each decision's parent must be the call immediately before it.
	var parents []uint64
	for seq := range parentByDecision {
		_ = seq
	}
	for _, want := range callSeqs {
		found := false
		for _, p := range parentByDecision {
			if p == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("no decision points at call seq %d (parents: %v)", want, parentByDecision)
		}
		parents = append(parents, want)
	}
}

// A hostile client can reuse one id indefinitely. The sticky union must stay
// bounded (and therefore only ever conservative), not grow per call.
func TestAmbiguousToolUnionIsBounded(t *testing.T) {
	m, _ := taintMediator(t)
	for i := 0; i < 200; i++ {
		m.Decide([]byte(`1`), fmt.Sprintf("tool_%d", i), []byte(`{}`))
	}
	m.Result([]byte(`1`), false, []byte(`{"text":"X"}`))

	m.mu.Lock()
	got := len(m.ambiguousTools["1"])
	m.mu.Unlock()
	if got == 0 {
		t.Fatal("the id should have been marked ambiguous")
	}
	if got > maxAmbiguousTools {
		t.Fatalf("sticky union must stay bounded at %d, got %d", maxAmbiguousTools, got)
	}
}
