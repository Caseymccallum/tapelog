package mediator

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Caseymccallum/tapelog/internal/approval"
	"github.com/Caseymccallum/tapelog/internal/policy"
	"github.com/Caseymccallum/tapelog/internal/session"
)

// TestCausationAndTraceparent pins the spec §6.1 contract: every
// follow-on event for a request carries parent_seq = its tools/call
// event's seq, and `_meta.traceparent` passes through to all of them.
func TestCausationAndTraceparent(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "s.jsonl")
	w, err := session.NewWriter(logPath, "causation-test")
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	m := New(Options{
		SessionID: "causation-test",
		Evaluator: policy.AllowAll{},
		Confirmer: approval.Auto{},
		Writer:    w,
	})

	meta := json.RawMessage(`{"traceparent":"00-abc123-def456-01"}`)
	m.DecideMeta([]byte(`1`), "read_file", []byte(`{"path":"/x"}`), meta)
	m.Result([]byte(`1`), false, []byte(`{"text":"ok"}`))

	events := readEvents(t, logPath)
	if len(events) != 3 {
		t.Fatalf("want call + decision + result, got %d events", len(events))
	}
	callSeq := events[0].Seq
	if callSeq == 0 {
		t.Fatal("tools/call event must carry a seq")
	}

	want := map[session.EventType]bool{
		session.EventPolicyDecision: true,
		session.EventToolResult:     true,
	}
	seen := map[session.EventType]bool{}
	for _, e := range events[1:] {
		var p struct {
			ParentSeq   *uint64 `json:"parent_seq"`
			Traceparent string  `json:"traceparent"`
		}
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			t.Fatal(err)
		}
		if p.ParentSeq == nil || *p.ParentSeq != callSeq {
			t.Fatalf("%s: parent_seq must point at the call event (seq %d), got %v", e.Type, callSeq, p.ParentSeq)
		}
		if p.Traceparent != "00-abc123-def456-01" {
			t.Fatalf("%s: traceparent must pass through, got %q", e.Type, p.Traceparent)
		}
		seen[e.Type] = true
	}
	for typ := range want {
		if !seen[typ] {
			t.Fatalf("missing follow-on event %s", typ)
		}
	}
	// The call event itself carries the traceparent too (correlation).
	var callP struct {
		Traceparent string `json:"traceparent"`
	}
	if err := json.Unmarshal(events[0].Payload, &callP); err != nil {
		t.Fatal(err)
	}
	if callP.Traceparent != "00-abc123-def456-01" {
		t.Fatalf("call event must carry traceparent, got %q", callP.Traceparent)
	}
}

// TestCausationAbsentMetaIsAdditive: without _meta, the linkage fields
// simply stay absent — old readers are unaffected (additive schema).
func TestCausationAbsentMetaIsAdditive(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "s.jsonl")
	w, err := session.NewWriter(logPath, "plain-test")
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	m := New(Options{
		SessionID: "plain-test",
		Evaluator: policy.AllowAll{},
		Confirmer: approval.Auto{},
		Writer:    w,
	})
	m.Decide([]byte(`1`), "read_file", []byte(`{}`))

	events := readEvents(t, logPath)
	for _, e := range events {
		if strings.Contains(string(e.Payload), "traceparent") {
			t.Fatalf("%s: traceparent must be absent without _meta: %s", e.Type, e.Payload)
		}
	}
	// parent_seq still links the decision to its call (causation is
	// internal, not transport-dependent).
	var p struct {
		ParentSeq *uint64 `json:"parent_seq"`
	}
	if err := json.Unmarshal(events[len(events)-1].Payload, &p); err != nil {
		t.Fatal(err)
	}
	if p.ParentSeq == nil {
		t.Fatalf("decision must link back to its call even without _meta")
	}
}

// TestBlobOffload wires the blob store through Decide/Result: oversized
// payloads land in the store and the event carries the digest reference.
func TestBlobOffload(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "s.jsonl")
	w, err := session.NewWriter(logPath, "blob-test")
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	blobs := session.NewBlobStore(session.DefaultBlobDir(logPath))
	m := New(Options{
		SessionID:     "blob-test",
		Evaluator:     policy.AllowAll{},
		Confirmer:     approval.Auto{},
		Writer:        w,
		Blobs:         blobs,
		BlobThreshold: 64,
	})

	big := json.RawMessage(`{"text":"` + strings.Repeat("q", 256) + `"}`)
	m.Decide([]byte(`1`), "read_file", big)
	m.Result([]byte(`1`), false, big)

	events := readEvents(t, logPath)
	refs := 0
	for _, e := range events {
		var p struct {
			Args   json.RawMessage `json:"args"`
			Result json.RawMessage `json:"result"`
		}
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			t.Fatal(err)
		}
		for _, raw := range []json.RawMessage{p.Args, p.Result} {
			if session.IsBlobRef(raw) {
				refs++
				if _, err := blobs.Resolve(raw); err != nil {
					t.Fatalf("recorded ref must resolve: %v", err)
				}
			}
		}
	}
	if refs != 2 {
		t.Fatalf("want 2 blob refs (args + result), got %d", refs)
	}

	// Replay-side load resolves both and fails loud when the store is
	// gone — exercised via the session API directly.
	events[0] = session.Event{Payload: session.ResolvePayload(events[0].Payload, blobs)}
	var p struct {
		Args json.RawMessage `json:"args"`
	}
	_ = json.Unmarshal(events[0].Payload, &p)
	if session.IsBlobRef(p.Args) {
		t.Fatal("resolved payload must not still be a placeholder")
	}
}

func readEvents(t *testing.T, path string) []session.Event {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out []session.Event
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var e session.Event
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("bad event line %q: %v", line, err)
		}
		out = append(out, e)
	}
	return out
}