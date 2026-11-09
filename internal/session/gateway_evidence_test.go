package session

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The M14.1 gateway evidence is a real session recorded through Tapelog by a
// Hermes gateway: two conversations over one MCP connection. Both snapshots are
// committed so the difference between "still open" and "closed" is a verifiable
// fact rather than a claim in a report:
//
//	session-gateway-open.jsonl    16 events, chain intact, INCOMPLETE — no_end
//	session-gateway-closed.jsonl  17 events, the same prefix + session/end
//
// The open snapshot is byte-for-byte the file that was verified during M14.1;
// the gateway wrote session/end eleven minutes later, when the child terminated.

const gatewayDir = "../../test-fixtures/m14.1-gateway"

func gatewayEvents(t *testing.T, name string) ([]Event, []byte) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(gatewayDir, name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	var events []Event
	for _, line := range bytes.Split(bytes.TrimRight(data, "\n"), []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var e Event
		if err := json.Unmarshal(line, &e); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		events = append(events, e)
	}
	return events, data
}

func TestGatewayEvidenceIsVerifiableAndHonest(t *testing.T) {
	closedEvents, closedData := gatewayEvents(t, "session-gateway-closed.jsonl")
	openEvents, openData := gatewayEvents(t, "session-gateway-open.jsonl")

	// Integrity is the same question for both files; completeness is not.
	for name, data := range map[string][]byte{
		"session-gateway-closed.jsonl": closedData,
		"session-gateway-open.jsonl":   openData,
	} {
		res, err := Verify(bytes.NewReader(data))
		if err != nil || !res.OK() {
			t.Fatalf("%s: chain not intact: %+v %v", name, res, err)
		}
		structure, err := InspectStructure(bytes.NewReader(data))
		if err != nil || !structure.Valid() {
			t.Fatalf("%s: not one valid session log: %+v %v", name, structure, err)
		}
		if name == "session-gateway-open.jsonl" {
			if structure.Complete() {
				t.Fatal("the open snapshot must stay INCOMPLETE — it has no session/end")
			}
			if got := structure.FirstProblem(); got == nil || got.Code != StructNoEnd {
				t.Fatalf("open snapshot must be no_end, got %+v", got)
			}
		} else if !structure.Complete() {
			t.Fatal("the closed snapshot must be complete")
		}
	}

	// The open file must be a byte prefix of the closed one: one session, one
	// event apart. If this ever fails, the pair is no longer evidence of anything.
	if !bytes.HasPrefix(closedData, openData) {
		t.Fatal("the open snapshot is not a byte prefix of the closed one")
	}
	if len(closedEvents) != len(openEvents)+1 {
		t.Fatalf("expected exactly one extra event, got %d vs %d", len(closedEvents), len(openEvents))
	}
	if last := closedEvents[len(closedEvents)-1]; last.Type != EventSessionEnd {
		t.Fatalf("the extra event must be session/end, got %q", last.Type)
	}
	// The closed file's session/end must chain to the open snapshot's head.
	if closedEvents[len(closedEvents)-1].PrevHash != openEvents[len(openEvents)-1].Hash {
		t.Fatal("session/end does not chain to the open snapshot's head")
	}

	// The committed pair is DERIVED: one machine-specific value was neutralised and
	// both files re-chained. DERIVED.json records both heads so the artefact and its
	// provenance note cannot drift apart, and so nobody mistakes it for the original.
	derivedRaw, err := os.ReadFile(filepath.Join(gatewayDir, "DERIVED.json"))
	if err != nil {
		t.Fatalf("read provenance note: %v", err)
	}
	var provenance struct {
		Source struct {
			ClosedHead string `json:"closed_head"`
			OpenHead   string `json:"open_head"`
		} `json:"source"`
		Derived struct {
			ClosedHead string `json:"closed_head"`
			OpenHead   string `json:"open_head"`
			PolicyID   string `json:"policy_id"`
		} `json:"derived"`
	}
	if err := json.Unmarshal(derivedRaw, &provenance); err != nil {
		t.Fatalf("provenance note is not valid JSON: %v", err)
	}
	if got := closedEvents[len(closedEvents)-1].Hash; got != provenance.Derived.ClosedHead {
		t.Fatalf("committed closed head %s does not match DERIVED.json (%s)", got, provenance.Derived.ClosedHead)
	}
	if got := openEvents[len(openEvents)-1].Hash; got != provenance.Derived.OpenHead {
		t.Fatalf("committed open head %s does not match DERIVED.json (%s)", got, provenance.Derived.OpenHead)
	}
	if provenance.Derived.ClosedHead == provenance.Source.ClosedHead {
		t.Fatal("a sanitised artefact must not share the original's head")
	}
	var startPayload SessionStartPayload
	if err := json.Unmarshal(closedEvents[0].Payload, &startPayload); err != nil {
		t.Fatalf("start payload: %v", err)
	}
	if startPayload.PolicyID != "policy.yaml" {
		t.Fatalf("the committed artefact must carry the neutral policy name, got %q", startPayload.PolicyID)
	}

	// Attribution: what the artifact actually demonstrates.
	counts := map[string]int{}
	var callsByConversation = map[string]int{}
	var unattributed int
	var denyRules []string
	for _, e := range closedEvents {
		counts[string(e.Type)]++
		switch e.Type {
		case EventToolCall:
			var p ToolCallPayload
			if err := json.Unmarshal(e.Payload, &p); err != nil {
				t.Fatalf("call payload: %v", err)
			}
			if p.ConversationID == "" {
				unattributed++
				continue
			}
			callsByConversation[p.ConversationID]++
		case EventPolicyDecision:
			var p PolicyDecisionPayload
			if err := json.Unmarshal(e.Payload, &p); err != nil {
				t.Fatalf("decision payload: %v", err)
			}
			if p.Verdict == "deny" {
				denyRules = append(denyRules, p.RuleID)
			}
		}
	}
	if counts[string(EventToolCall)] != 5 {
		t.Fatalf("expected 5 mediated calls, got %d", counts[string(EventToolCall)])
	}
	if counts[string(EventSessionStart)] != 1 || counts[string(EventSessionEnd)] != 1 {
		t.Fatalf("expected exactly one start and one end, got %v", counts)
	}
	if unattributed != 0 {
		t.Fatalf("every call in this artifact declares a conversation, got %d unattributed", unattributed)
	}
	if len(callsByConversation) != 2 {
		t.Fatalf("expected two conversations, got %v", callsByConversation)
	}
	for id, n := range callsByConversation {
		if !strings.HasPrefix(id, "local-") || n == 0 {
			t.Fatalf("unexpected conversation %q with %d calls", id, n)
		}
	}
	if len(denyRules) != 1 || denyRules[0] != "no-delete" {
		t.Fatalf("expected exactly one deny from no-delete, got %v", denyRules)
	}
}

// TestGatewayEvidenceCannotRepairStructure pins the limit of the artifact: the
// conversation ids are client-declared correlation, so they must not make a
// mixed-identity file valid, and they must never create a session boundary.
func TestGatewayEvidenceCannotRepairStructure(t *testing.T) {
	events, _ := gatewayEvents(t, "session-gateway-closed.jsonl")
	for i := range events {
		if events[i].Type == EventToolCall {
			events[i].SessionID = "20260101T000000.000-ffffffffffffffff"
			break
		}
	}
	var buf bytes.Buffer
	for _, e := range events {
		line, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		buf.Write(line)
		buf.WriteByte('\n')
	}
	structure, err := InspectStructure(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if structure.Valid() {
		t.Fatal("attribution must not be able to repair a mixed-identity session")
	}
}
