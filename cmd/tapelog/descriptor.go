package main

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/Caseymccallum/tapelog/internal/jsonrpc"
	"github.com/Caseymccallum/tapelog/internal/mediator"
)

// descriptorTracker watches tools/list traffic in the transparent-proxy
// path and feeds descriptor pins to the mediator (rug-pull detection,
// docs/THREAT_MODEL.md claim #2). Hashing/pinning/drift logic lives in the
// mediator so the mux path shares it.
type descriptorTracker struct {
	mu       sync.Mutex
	listIDs  map[string]bool
	listings int // in-flight tools/list requests
	med      *mediator.Mediator
}

func newDescriptorTracker(med *mediator.Mediator) *descriptorTracker {
	return &descriptorTracker{listIDs: map[string]bool{}, med: med}
}

// awaitPendingListings blocks until every in-flight tools/list request has
// been answered (or timeout elapses). Tool calls are then evaluated against
// fully up-to-date descriptor pins — without this, a fast client could race
// a poisoned listing response past enforcement.
func (t *descriptorTracker) awaitPendingListings(timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	t.mu.Lock()
	defer t.mu.Unlock()
	for t.listings > 0 && time.Now().Before(deadline) {
		t.mu.Unlock()
		time.Sleep(2 * time.Millisecond)
		t.mu.Lock()
	}
}

// observe is a proxy.OnRawMessage hook.
func (t *descriptorTracker) observe(direction string, raw []byte) {
	msg, err := jsonrpc.Parse(raw)
	if err != nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	if direction == "c2s" && msg.IsRequest() && msg.Method == "tools/list" {
		t.listIDs[string(msg.ID)] = true
		t.listings++
		return
	}
	if direction == "s2c" && msg.IsResponse() && t.listIDs[string(msg.ID)] {
		delete(t.listIDs, string(msg.ID))
		if t.listings > 0 {
			t.listings--
		}
		var result struct {
			Tools []json.RawMessage `json:"tools"`
		}
		if err := json.Unmarshal(msg.Result, &result); err != nil {
			return
		}
		for _, tool := range result.Tools {
			var desc struct {
				Name string `json:"name"`
			}
			if err := json.Unmarshal(tool, &desc); err != nil || desc.Name == "" {
				continue
			}
			hash, _ := t.med.HashDescriptor(tool)
			t.med.PinDescriptor(desc.Name, hash) // drift detection lives in the mediator
			_ = t.med.PinSchema(desc.Name, tool) // inbound schema firewall
		}
		t.med.RecordToolsList(result.Tools)
	}
}
