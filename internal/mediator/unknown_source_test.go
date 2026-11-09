package mediator

import (
	"fmt"
	"testing"

	"github.com/Caseymccallum/tapelog/internal/taint"
)

// Exactly at capacity: the last source still fits, nothing is unknown.
func TestSourceCapacityExact(t *testing.T) {
	m, st := taintMediator(t)

	m.mu.Lock()
	m.allSeenTools = make([]string, 0, maxSeenTools)
	for i := 0; i < maxSeenTools-1; i++ {
		m.allSeenTools = append(m.allSeenTools, fmt.Sprintf("tool_%05d", i))
	}
	m.mu.Unlock()

	m.Decide([]byte(`1`), "last_fitting_tool", []byte(`{}`))
	m.Result([]byte(`1`), false, []byte(`{"text":"EXACTLY-AT-CAP"}`))

	got := st.ContaminatedBy([]byte(`{"body":"EXACTLY-AT-CAP"}`))
	if !hasSource(got, "last_fitting_tool") {
		t.Fatalf("the last fitting source must be named, got %v", got)
	}
	if hasSource(got, taint.UnknownSource) {
		t.Fatalf("nothing is unknown at exactly capacity, got %v", got)
	}
}

// One past capacity: the source can no longer be named, so its values must be
// marked UNKNOWN rather than silently clean. A test that only checks "tainted
// by something" would pass here even without the sentinel; this asserts the
// sentinel specifically.
func TestSourceCapacityExhaustedMarksUnknown(t *testing.T) {
	m, st := taintMediator(t)

	m.mu.Lock()
	m.allSeenTools = make([]string, 0, maxSeenTools)
	for i := 0; i < maxSeenTools; i++ {
		m.allSeenTools = append(m.allSeenTools, fmt.Sprintf("tool_%05d", i))
	}
	m.mu.Unlock()

	m.Decide([]byte(`2`), "overflow_source", []byte(`{}`))
	m.Result([]byte(`2`), false, []byte(`{"text":"OVERFLOW-VALUE"}`))

	got := st.ContaminatedBy([]byte(`{"body":"OVERFLOW-VALUE"}`))
	if !hasSource(got, taint.UnknownSource) {
		t.Fatalf("an untrackable source must be recorded as unknown, got %v", got)
	}

	// The sharper case: the queue has drained, so the source cannot be named
	// from pending state either. A NEW value from that untracked source must
	// still come out as unknown, never as clean.
	m.Result([]byte(`3`), false, []byte(`{"text":"AFTER-DRAIN-VALUE"}`))
	later := st.ContaminatedBy([]byte(`{"body":"AFTER-DRAIN-VALUE"}`))
	if !hasSource(later, taint.UnknownSource) {
		t.Fatalf("a value from an untracked source must stay unknown after the queue drains, got %v", later)
	}
}

// Capacity never grows past the bound, however many tools are seen.
func TestSourceCapacityIsBounded(t *testing.T) {
	m, _ := taintMediator(t)

	m.mu.Lock()
	m.allSeenTools = make([]string, 0, maxSeenTools)
	for i := 0; i < maxSeenTools; i++ {
		m.allSeenTools = append(m.allSeenTools, fmt.Sprintf("tool_%05d", i))
	}
	m.mu.Unlock()

	for i := 0; i < 50; i++ {
		m.Decide([]byte(fmt.Sprintf("%d", 100+i)), fmt.Sprintf("extra_%d", i), []byte(`{}`))
	}
	m.mu.Lock()
	n := len(m.allSeenTools)
	m.mu.Unlock()
	if n > maxSeenTools {
		t.Fatalf("source set must stay bounded at %d, got %d", maxSeenTools, n)
	}
}
