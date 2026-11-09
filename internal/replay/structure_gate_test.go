package replay

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Caseymccallum/tapelog/internal/session"
)

func gatePath(t *testing.T, evs ...session.Event) string {
	t.Helper()
	for i := range evs {
		evs[i].Seq = uint64(i + 1)
	}
	return gatePathRaw(t, evs...)
}

// gatePathRaw chains events with the Seq values already set on them.
func gatePathRaw(t *testing.T, evs ...session.Event) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "gate.jsonl")
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
	return path
}

func gStart(sid string) session.Event {
	return session.Event{Type: session.EventSessionStart, SessionID: sid,
		Payload: json.RawMessage(`{"harness":"t","policy_id":"observe"}`)}
}

func gEnd(sid string) session.Event {
	return session.Event{Type: session.EventSessionEnd, SessionID: sid,
		Payload: json.RawMessage(`{"reason":"client disconnect"}`)}
}

func gCall(sid string) session.Event {
	return gCallID(sid, 1)
}

// gCallID keeps request ids distinct unless a test is deliberately about
// duplicate ids: an overlapping duplicate now trips the ambiguity gate
// before any structure gate, which would make a structure test prove the
// wrong thing.
func gCallID(sid string, id int) session.Event {
	return session.Event{Type: session.EventToolCall, SessionID: sid,
		Payload: json.RawMessage(fmt.Sprintf(`{"id":%d,"tool":"read_file","args":{}}`, id))}
}

// TestLoadVerifiedRefusesInvalidStructure pins the replay structure gate: an
// intact chain carrying two session ids must be refused by default and served
// only through the explicit escape hatch.
func TestLoadVerifiedRefusesInvalidStructure(t *testing.T) {
	path := gatePath(t, gStart("a"), gCallID("a", 1), gStart("b"), gCallID("b", 2), gEnd("b"))

	if _, _, err := LoadVerified(path, false); err == nil {
		t.Fatal("LoadVerified must refuse a mixed-identity file")
	}
	if _, _, err := LoadVerifiedStructure(path, false, false); err == nil {
		t.Fatal("LoadVerifiedStructure must refuse by default")
	}
	if _, _, err := LoadVerifiedStructure(path, false, true); err != nil {
		t.Fatalf("allow-invalid-structure must load: %v", err)
	}
}

// TestLoadVerifiedRefusesSeqZeroIdentity is the F1/F3 regression: an empty
// identity at seq 0 must be refused, not served.
func TestLoadVerifiedRefusesSeqZeroIdentity(t *testing.T) {
	path := gatePathRaw(t,
		session.Event{Type: session.EventSessionStart, SessionID: "", Seq: 0,
			Payload: json.RawMessage(`{"harness":"t","policy_id":"observe"}`)},
		session.Event{Type: session.EventToolCall, SessionID: "sess-1", Seq: 1,
			Payload: json.RawMessage(`{"id":1,"tool":"read_file","args":{}}`)},
		session.Event{Type: session.EventSessionEnd, SessionID: "sess-1", Seq: 2,
			Payload: json.RawMessage(`{"reason":"client disconnect"}`)},
	)
	if _, _, err := LoadVerifiedStructure(path, false, false); err == nil {
		t.Fatal("a seq-0 empty identity must be refused")
	}
}
