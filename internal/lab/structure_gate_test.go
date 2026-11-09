package lab

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Caseymccallum/tapelog/internal/session"
)

// gateChain writes a correctly hash-chained log with seq = position+1.
func gateChain(t *testing.T, path string, evs ...session.Event) {
	t.Helper()
	prev := ""
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for i := range evs {
		e := evs[i]
		e.Version = session.SchemaVersion
		e.Seq = uint64(i + 1)
		e.PrevHash = prev
		h, err := e.ComputeHash()
		if err != nil {
			t.Fatal(err)
		}
		e.Hash = h
		prev = h
		raw, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write(append(raw, '\n')); err != nil {
			t.Fatal(err)
		}
	}
}

// gateChainRaw chains events using the Seq values the caller set, so a test can
// place an event at seq 0.
func gateChainRaw(t *testing.T, path string, evs ...session.Event) {
	t.Helper()
	prev := ""
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for i := range evs {
		e := evs[i]
		e.Version = session.SchemaVersion
		e.PrevHash = prev
		h, err := e.ComputeHash()
		if err != nil {
			t.Fatal(err)
		}
		e.Hash = h
		prev = h
		raw, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write(append(raw, '\n')); err != nil {
			t.Fatal(err)
		}
	}
}

func gateStart(sid string) session.Event {
	return session.Event{Type: session.EventSessionStart, SessionID: sid,
		Payload: json.RawMessage(`{"harness":"t","policy_id":"observe"}`)}
}

func gateEnd(sid string) session.Event {
	return session.Event{Type: session.EventSessionEnd, SessionID: sid,
		Payload: json.RawMessage(`{"reason":"client disconnect"}`)}
}

func gateCall(sid string, tool string) session.Event {
	raw, _ := json.Marshal(map[string]any{"id": 1, "tool": tool, "args": map[string]any{}})
	return session.Event{Type: session.EventToolCall, SessionID: sid, Payload: raw}
}

func gateCase() Case {
	return Case{
		Version: 1, Title: "gate", Evidence: "e.jsonl", Eligibility: "complete-boundary",
		Expectations: []Expectation{{Kind: "attempted", Tool: "read_file"}},
	}
}

func gateEvaluate(t *testing.T, path string) Result {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return Evaluate(gateCase(), data, "")
}

// TestStructureValidityGateRefusesMixedIdentity pins the lab structure gate:
// an intact chain carrying two session ids must be INCONCLUSIVE
// structure_invalid, never PASS.
func TestStructureValidityGateRefusesMixedIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mixed.jsonl")
	gateChain(t, path, gateStart("a"), gateCall("a", "read_file"), gateStart("b"), gateCall("b", "read_file"), gateEnd("b"))
	r := gateEvaluate(t, path)
	if r.Status != "INCONCLUSIVE" || r.Checks[0].Code != CodeStructure {
		t.Fatalf("mixed identity: status=%s code=%q checks=%+v", r.Status, r.Checks[0].Code, r.Checks)
	}
}

// TestStructureValidityGateRefusesEmptyIdentity pins the empty-identity half:
// the chain is intact, so the structure gate (not the chain gate) must reject.
func TestStructureValidityGateRefusesEmptyIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "no-id.jsonl")
	gateChain(t, path, gateStart(""), gateCall("", "read_file"), gateEnd(""))
	r := gateEvaluate(t, path)
	if r.Status != "INCONCLUSIVE" || r.Checks[0].Code != CodeStructure {
		t.Fatalf("empty identity: status=%s code=%q checks=%+v", r.Status, r.Checks[0].Code, r.Checks)
	}
}

// TestStructureGateRestoresPreChangeRejectionAtSeqZero is the F1/F3 regression.
// Before the change this fixture was INCONCLUSIVE ("the recording has no
// session identity"); the seq:0 bypass made it PASS. It must be INCONCLUSIVE
// again. The code is damaged_recording because a seq-0 event is a genuine
// sequence gap, which the chain gate now detects instead of hiding.
func TestStructureGateRestoresPreChangeRejectionAtSeqZero(t *testing.T) {
	path := filepath.Join(t.TempDir(), "seq0.jsonl")
	gateChainRaw(t, path,
		session.Event{Type: session.EventSessionStart, SessionID: "", Seq: 0,
			Payload: json.RawMessage(`{"harness":"t","policy_id":"observe"}`)},
		session.Event{Type: session.EventToolCall, SessionID: "sess-1", Seq: 1,
			Payload: json.RawMessage(`{"id":1,"tool":"read_file","args":{}}`)},
		session.Event{Type: session.EventSessionEnd, SessionID: "sess-1", Seq: 2,
			Payload: json.RawMessage(`{"reason":"client disconnect"}`)},
	)
	r := gateEvaluate(t, path)
	if r.Status != "INCONCLUSIVE" {
		t.Fatalf("seq-0 empty identity must be INCONCLUSIVE, got status=%s checks=%+v", r.Status, r.Checks)
	}
	if r.Checks[0].Code == "" {
		t.Fatal("the inconclusive check must carry a code")
	}
}
