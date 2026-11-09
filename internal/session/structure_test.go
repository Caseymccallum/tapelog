package session

import (
	"encoding/json"
	"strings"
	"testing"
)

// chainEvents renders events as a correctly hash-chained JSONL log so a
// structure test is never confounded by a chain failure.
func chainEvents(t *testing.T, evs ...Event) string {
	t.Helper()
	prev := ""
	var lines []string
	for i := range evs {
		e := evs[i]
		e.Version = SchemaVersion
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
		lines = append(lines, string(raw))
	}
	return strings.Join(lines, "\n") + "\n"
}

func startEv(sid string) Event {
	return Event{Type: EventSessionStart, SessionID: sid, Payload: json.RawMessage(`{"harness":"t","policy_id":"observe"}`)}
}

func endEv(sid string) Event {
	return Event{Type: EventSessionEnd, SessionID: sid, Payload: json.RawMessage(`{"reason":"client disconnect"}`)}
}

func callEv(sid string, id int) Event {
	return Event{Type: EventToolCall, SessionID: sid,
		Payload: json.RawMessage(`{"id":` + jsonInt(id) + `,"tool":"read_file","args":{}}`)}
}

func jsonInt(n int) string {
	raw, _ := json.Marshal(n)
	return string(raw)
}

func TestInspectStructure(t *testing.T) {
	tests := []struct {
		name     string
		log      string
		valid    bool
		complete bool
		code     string
	}{
		{"control", chainEvents(t, startEv("s"), callEv("s", 1), endEv("s")), true, true, ""},
		{"no end is incomplete not invalid", chainEvents(t, startEv("s"), callEv("s", 1)), true, false, StructNoEnd},
		{"mixed identity", chainEvents(t, startEv("a"), callEv("a", 1), startEv("b"), endEv("b")), false, false, StructMixedIdentity},
		{"no start", chainEvents(t, callEv("s", 1), endEv("s")), false, false, StructNoStart},
		{"two starts", chainEvents(t, startEv("s"), startEv("s"), endEv("s")), false, false, StructMultipleStarts},
		{"start not first", chainEvents(t, callEv("s", 1), startEv("s"), endEv("s")), false, false, StructStartNotFirst},
		{"two ends", chainEvents(t, startEv("s"), endEv("s"), endEv("s")), false, false, StructMultipleEnds},
		{"end not last", chainEvents(t, startEv("s"), endEv("s"), callEv("s", 1)), false, false, StructEndNotLast},
		{"no identity", chainEvents(t, startEv(""), endEv("")), false, false, StructNoIdentity},
		{"empty file", "", false, false, StructNoEvents},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rep, err := InspectStructure(strings.NewReader(tt.log))
			if err != nil {
				t.Fatalf("InspectStructure: %v", err)
			}
			if rep.Valid() != tt.valid {
				t.Errorf("Valid() = %v, want %v (problems %+v)", rep.Valid(), tt.valid, rep.Problems)
			}
			if rep.Complete() != tt.complete {
				t.Errorf("Complete() = %v, want %v (problems %+v)", rep.Complete(), tt.complete, rep.Problems)
			}
			p := rep.FirstProblem()
			if tt.code == "" {
				if p != nil {
					t.Errorf("FirstProblem() = %+v, want nil", *p)
				}
				return
			}
			if p == nil || p.Code != tt.code {
				t.Fatalf("FirstProblem() = %+v, want code %s", p, tt.code)
			}
		})
	}
}

// TestStructureIsOrthogonalToChain pins the contract: a structurally invalid
// file can still have an intact hash chain, and Verify must not start
// reporting structure problems.
func TestStructureIsOrthogonalToChain(t *testing.T) {
	mixed := chainEvents(t, startEv("a"), callEv("a", 1), startEv("b"), endEv("b"))
	res, err := Verify(strings.NewReader(mixed))
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK() {
		t.Fatalf("mixed-identity log must still verify as chain-intact: %+v", res)
	}
	rep, err := InspectStructure(strings.NewReader(mixed))
	if err != nil {
		t.Fatal(err)
	}
	if rep.Valid() {
		t.Fatal("mixed-identity log must be structurally invalid")
	}
}

func TestInspectStructureRejectsUnreadableLine(t *testing.T) {
	if _, err := InspectStructure(strings.NewReader("{\"v\":0}\n{broken\n")); err == nil {
		t.Fatal("an unreadable line must be an error, not a report")
	}
}

// chainEventsRaw chains events using the Seq values the caller set, so a test
// can place an event at seq 0.
func chainEventsRaw(t *testing.T, evs ...Event) string {
	t.Helper()
	prev := ""
	var lines []string
	for i := range evs {
		e := evs[i]
		e.Version = SchemaVersion
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
		lines = append(lines, string(raw))
	}
	return strings.Join(lines, "\n") + "\n"
}

// TestInspectStructureIdentityProblemAtSeqZero pins the root cause of the
// seq:0 bypass: identity problems are recorded with a boolean, not a uint64
// zero sentinel, so an empty or mixed identity at seq 0 is still reported.
func TestInspectStructureIdentityProblemAtSeqZero(t *testing.T) {
	t.Run("empty identity at seq 0", func(t *testing.T) {
		log := chainEventsRaw(t,
			Event{Type: EventSessionStart, SessionID: "", Seq: 0, Payload: json.RawMessage(`{"harness":"t"}`)},
			Event{Type: EventToolCall, SessionID: "sess-1", Seq: 1, Payload: json.RawMessage(`{"id":1,"tool":"read_file","args":{}}`)},
			Event{Type: EventSessionEnd, SessionID: "sess-1", Seq: 2, Payload: json.RawMessage(`{"reason":"r"}`)},
		)
		rep, err := InspectStructure(strings.NewReader(log))
		if err != nil {
			t.Fatal(err)
		}
		p := rep.FirstProblem()
		if rep.Valid() || p == nil || p.Code != StructNoIdentity {
			t.Fatalf("empty identity at seq 0 must be INVALID no_identity, got valid=%v problems=%+v", rep.Valid(), rep.Problems)
		}
		if !p.HasSeq || p.Seq != 0 {
			t.Fatalf("the problem must carry seq 0, got %+v", *p)
		}
	})

	t.Run("mixed identity at seq 0", func(t *testing.T) {
		log := chainEventsRaw(t,
			Event{Type: EventSessionStart, SessionID: "a", Seq: 0, Payload: json.RawMessage(`{"harness":"t"}`)},
			Event{Type: EventSessionStart, SessionID: "b", Seq: 1, Payload: json.RawMessage(`{"harness":"t"}`)},
			Event{Type: EventSessionEnd, SessionID: "b", Seq: 2, Payload: json.RawMessage(`{"reason":"r"}`)},
		)
		rep, err := InspectStructure(strings.NewReader(log))
		if err != nil {
			t.Fatal(err)
		}
		if rep.Valid() {
			t.Fatalf("mixed identity at seq 0 must be INVALID, got %+v", rep.Problems)
		}
	})
}

// TestVerifyDetectsSequenceGapAtSeqZero pins the other half of the root cause:
// FirstBadSeq 0 must not read as "intact". OK() reads Bad, so a problem at
// seq 0 is a problem.
func TestVerifyDetectsSequenceGapAtSeqZero(t *testing.T) {
	log := chainEventsRaw(t,
		Event{Type: EventSessionStart, SessionID: "s", Seq: 0, Payload: json.RawMessage(`{"harness":"t"}`)},
	)
	res, err := Verify(strings.NewReader(log))
	if err != nil {
		t.Fatal(err)
	}
	if res.OK() {
		t.Fatalf("a seq-0 first event must not verify as intact: %+v", res)
	}
	if !res.Bad {
		t.Fatal("Bad must be set for a seq-0 sequence gap")
	}
	if res.FirstBadSeq != 0 {
		t.Fatalf("FirstBadSeq = %d, want 0", res.FirstBadSeq)
	}
	if res.Problem == "" {
		t.Fatal("the problem must be described")
	}
}
