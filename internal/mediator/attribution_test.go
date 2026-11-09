package mediator

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Caseymccallum/tapelog/internal/approval"
	"github.com/Caseymccallum/tapelog/internal/jsonrpc"
	"github.com/Caseymccallum/tapelog/internal/policy"
	"github.com/Caseymccallum/tapelog/internal/session"
)

func newAttributionMediator(t *testing.T) (*Mediator, string) {
	t.Helper()
	logPath := filepath.Join(t.TempDir(), "s.jsonl")
	w, err := session.NewWriter(logPath, "attribution-test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.Close() })
	m := New(Options{
		SessionID: "attribution-test",
		Evaluator: policy.AllowAll{},
		Confirmer: approval.Auto{},
		Writer:    w,
	})
	return m, logPath
}

// The attribution a client declares on a request must appear on the call,
// its decision and its result: three events, one claim, no inference.
func TestAttributionRecordedOnFollowOnEvents(t *testing.T) {
	m, logPath := newAttributionMediator(t)
	meta := json.RawMessage(`{"io.tapelog/conversationId":"conv-1","io.tapelog/toolCallId":"call-7","io.tapelog/subagentId":"worker-b"}`)
	m.DecideMeta([]byte(`1`), "read_file", []byte(`{"path":"/x"}`), meta)
	m.Result([]byte(`1`), false, []byte(`{"text":"ok"}`))

	events := readEvents(t, logPath)
	if len(events) != 3 {
		t.Fatalf("want call + decision + result, got %d", len(events))
	}
	for _, e := range events {
		var p struct {
			ConversationID string `json:"conversation_id"`
			ToolCallID     string `json:"tool_call_id"`
			SubagentID     string `json:"subagent_id"`
		}
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			t.Fatal(err)
		}
		if p.ConversationID != "conv-1" || p.ToolCallID != "call-7" || p.SubagentID != "worker-b" {
			t.Fatalf("%s: attribution must carry across events, got %+v", e.Type, p)
		}
	}
}

// A client that declares nothing must produce a recording with no
// attribution keys at all — not an invented or default identifier.
func TestAttributionAbsentIsNotInvented(t *testing.T) {
	m, logPath := newAttributionMediator(t)
	m.Decide([]byte(`2`), "read_file", []byte(`{"path":"/x"}`))
	m.Result([]byte(`2`), false, []byte(`{"text":"ok"}`))

	for _, e := range readEvents(t, logPath) {
		if strings.Contains(string(e.Payload), "conversation_id") ||
			strings.Contains(string(e.Payload), "tool_call_id") ||
			strings.Contains(string(e.Payload), "subagent_id") {
			t.Fatalf("%s: no attribution was declared; payload must not gain one: %s", e.Type, e.Payload)
		}
	}
}

// A rejected identifier is never recorded (a truncated id is a different
// id), and cannot be smuggled in to force a bogus group.
func TestAttributionInvalidIsNotRecorded(t *testing.T) {
	m, logPath := newAttributionMediator(t)
	bad := json.RawMessage(`{"io.tapelog/conversationId":"` + strings.Repeat("x", jsonrpc.MaxAttributionLen+1) + `"}`)
	m.DecideMeta([]byte(`3`), "read_file", []byte(`{"path":"/x"}`), bad)
	m.Result([]byte(`3`), false, []byte(`{"text":"ok"}`))

	for _, e := range readEvents(t, logPath) {
		if strings.Contains(string(e.Payload), "conversation_id") {
			t.Fatalf("%s: rejected id must not be recorded: %s", e.Type, e.Payload)
		}
	}
}

// Two calls in flight share a JSON-RPC id. No ordering assumption makes the
// pairing knowable, so EVERY result under that id is recorded as ambiguous —
// including the last one, because the first response may have been either
// call's. M11 paired them FIFO; that was manufactured certainty.
func TestAttributionSurvivesRepeatedRequestIDs(t *testing.T) {
	m, logPath := newAttributionMediator(t)
	metaA := json.RawMessage(`{"io.tapelog/conversationId":"conv-A"}`)
	metaB := json.RawMessage(`{"io.tapelog/conversationId":"conv-B"}`)
	m.DecideMeta([]byte(`1`), "read_file", []byte(`{"path":"/a"}`), metaA)
	m.DecideMeta([]byte(`1`), "read_file", []byte(`{"path":"/b"}`), metaB)
	m.Result([]byte(`1`), false, []byte(`{"text":"first"}`))
	m.Result([]byte(`1`), false, []byte(`{"text":"second"}`))

	var results []session.ToolResultPayload
	for _, e := range readEvents(t, logPath) {
		if e.Type != session.EventToolResult {
			continue
		}
		var p session.ToolResultPayload
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			t.Fatal(err)
		}
		results = append(results, p)
	}
	if len(results) != 2 {
		t.Fatalf("want 2 results, got %d", len(results))
	}
	for i, r := range results {
		if !r.AttributionAmbiguous {
			t.Fatalf("result %d must be recorded ambiguous, got %+v", i, r)
		}
		if r.ConversationID != "" || r.ToolCallID != "" || r.ParentSeq != nil {
			t.Fatalf("result %d must claim no correlation, got %+v", i, r)
		}
	}
}

// Attribution is correlation, not structure: a recording that mixes two
// session identities stays structurally INVALID even when every event
// carries a distinct conversation id.
func TestAttributionCannotRepairStructure(t *testing.T) {
	lines := []string{
		`{"type":"session/start","session_id":"a","payload":{}}`,
		`{"type":"tools/call","session_id":"a","payload":{"id":1,"tool":"read_file","args":{},"conversation_id":"conv-1"}}`,
		`{"type":"tools/call","session_id":"b","payload":{"id":2,"tool":"read_file","args":{},"conversation_id":"conv-2"}}`,
		`{"type":"session/end","session_id":"a","payload":{}}`,
	}
	rep, err := session.InspectStructure(strings.NewReader(strings.Join(lines, "\n") + "\n"))
	if err != nil {
		t.Fatal(err)
	}
	if rep.Valid() {
		t.Fatal("two identities must remain INVALID however they are attributed")
	}
	found := false
	for _, p := range rep.Problems {
		if p.Code == "mixed_identity" {
			found = true
		}
	}
	if !found {
		t.Fatalf("want mixed_identity, got %+v", rep.Problems)
	}
}
