package mediator

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Caseymccallum/tapelog/internal/approval"
	"github.com/Caseymccallum/tapelog/internal/policy"

	"github.com/Caseymccallum/tapelog/internal/session"
)

// F1: when the pending-call cap discards entries, the discarded sources must
// keep tainting later results. Losing them would silently UN-taint a secret
// and let a value-flow rule miss a real exfiltration.
func TestDiscardedPendingSourcesStayTainted(t *testing.T) {
	m, st := taintMediator(t)

	m.mu.Lock()
	m.pendingTools["777"] = []string{"read_secrets"}
	for i := 0; i < 4200; i++ {
		m.pendingTools[fmt.Sprintf("filler-%d", i)] = []string{"filler_tool"}
	}
	m.mu.Unlock()

	// A new call trips the cap and wholesale-drops the pending map.
	m.Decide([]byte(`999`), "read_file", []byte(`{}`))

	// The delayed result of the discarded call arrives afterwards.
	m.Result([]byte(`777`), false, []byte(`{"text":"SECRET-LATE"}`))

	if got := st.ContaminatedBy([]byte(`{"body":"SECRET-LATE"}`)); !hasSource(got, "read_secrets") {
		t.Fatalf("a source discarded by the cap must still taint later results, got %v", got)
	}
}

// F3: after link state is discarded, a delayed old response could be paired
// with a NEW call under the same id and look exact. It must be marked
// ambiguous instead of being presented as fact.
func TestDiscardedLinkStateMakesPairingAmbiguous(t *testing.T) {
	m, logPath := newAttributionMediator(t)

	m.mu.Lock()
	for i := 0; i < 4200; i++ {
		m.links[fmt.Sprintf("filler-%d", i)] = []*callLink{{seq: 1}}
	}
	m.mu.Unlock()

	// A new call trips the links cap; its result must not look exact.
	m.DecideMeta([]byte(`42`), "read_file", []byte(`{"path":"/new"}`), nil)
	m.Result([]byte(`42`), false, []byte(`{"text":"x"}`))

	seen := false
	for _, e := range readEvents(t, logPath) {
		if e.Type != session.EventToolResult {
			continue
		}
		seen = true
		var p session.ToolResultPayload
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			t.Fatal(err)
		}
		if !p.AttributionAmbiguous {
			t.Fatal("after correlation state was discarded, a pairing must not be presented as exact")
		}
	}
	if !seen {
		t.Fatal("no result recorded")
	}
}

// F4: stickiness belongs to the ambiguous cluster, not to the id forever. A
// later sequential reuse is genuinely unambiguous and must not be flagged.
func TestSequentialReuseAfterClusterIsClean(t *testing.T) {
	m, logPath := newAttributionMediator(t)
	meta := json.RawMessage(`{"io.tapelog/conversationId":"conv-C"}`)

	// Ambiguous cluster under id 5.
	m.DecideMeta([]byte(`5`), "read_file", []byte(`{"path":"/a"}`), meta)
	m.DecideMeta([]byte(`5`), "read_file", []byte(`{"path":"/b"}`), meta)
	m.Result([]byte(`5`), false, []byte(`{"text":"1"}`))
	m.Result([]byte(`5`), false, []byte(`{"text":"2"}`))

	// Later, sequential reuse of the same id.
	m.DecideMeta([]byte(`5`), "read_file", []byte(`{"path":"/c"}`), meta)
	m.Result([]byte(`5`), false, []byte(`{"text":"3"}`))

	var flags []bool
	var convs []string
	for _, e := range readEvents(t, logPath) {
		if e.Type != session.EventToolResult {
			continue
		}
		var p session.ToolResultPayload
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			t.Fatal(err)
		}
		flags = append(flags, p.AttributionAmbiguous)
		convs = append(convs, p.ConversationID)
	}
	if len(flags) != 3 {
		t.Fatalf("want 3 results, got %d", len(flags))
	}
	if !flags[0] || !flags[1] {
		t.Fatalf("the ambiguous cluster must be flagged, got %v", flags)
	}
	if flags[2] {
		t.Fatalf("a later sequential reuse must not be flagged, got %v", flags)
	}
	if convs[2] != "conv-C" {
		t.Fatalf("the sequential reuse must carry its exact attribution, got %q", convs[2])
	}
}

// A denied call never receives a result. If its correlation link stayed in the
// queue, the next call under the same id would look ambiguous for no reason.
func TestDeniedCallDoesNotPoisonLaterReuse(t *testing.T) {
	denyPath := filepath.Join(t.TempDir(), "deny.yaml")
	if err := os.WriteFile(denyPath, []byte(`version: 1
default: deny
`), 0o600); err != nil {
		t.Fatal(err)
	}
	denyEval, err := policy.Load(denyPath)
	if err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(t.TempDir(), "deny.jsonl")
	w, err := session.NewWriter(logPath, "deny-link")
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	m := New(Options{SessionID: "deny-link", Evaluator: denyEval, Confirmer: approval.Auto{}, Writer: w})

	m.DecideMeta([]byte(`9`), "read_file", []byte(`{"path":"/denied"}`), nil) // denied
	m.opts.Evaluator = policy.AllowAll{}
	m.DecideMeta([]byte(`9`), "read_file", []byte(`{"path":"/allowed"}`), nil) // allowed
	m.Result([]byte(`9`), false, []byte(`{"text":"ok"}`))

	var results []session.ToolResultPayload
	for _, e := range readEvents(t, logPath) {
		if e.Type != session.EventToolResult {
			continue
		}
		var pl session.ToolResultPayload
		if err := json.Unmarshal(e.Payload, &pl); err != nil {
			t.Fatal(err)
		}
		results = append(results, pl)
	}
	if len(results) != 1 {
		t.Fatalf("want 1 result, got %d", len(results))
	}
	if results[0].AttributionAmbiguous {
		t.Fatal("a denied call must not make a later, unambiguous reuse look ambiguous")
	}
}
