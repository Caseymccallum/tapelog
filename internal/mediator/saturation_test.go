package mediator

import (
	"fmt"
	"testing"
)

// More distinct tools than the per-id union cap can hold. A late result whose
// source was already consumed must still be tainted by that source: dropping
// it would silently stop a value-flow rule firing. The union overflows into a
// conservative "everything this session has seen" fallback.
func TestTaintUnionOverflowStaysConservative(t *testing.T) {
	m, st := taintMediator(t)

	const n = 40
	for i := 0; i < n; i++ {
		m.Decide([]byte(`1`), fmt.Sprintf("tool_%02d", i), []byte(`{}`))
	}
	// Consume 36 results in order, draining tools 00..35 from the queue.
	for i := 0; i < 36; i++ {
		m.Result([]byte(`1`), false, []byte(fmt.Sprintf(`{"text":"MARKER-%02d"}`, i)))
	}
	// A late result whose true source (tool_35) has already been consumed and
	// is beyond the capped union. The value must appear ONLY here: reusing an
	// earlier marker would let the store match a value marked by an earlier
	// result and the test would pass without the fallback ever running.
	m.Result([]byte(`1`), false, []byte(`{"text":"LATE-ONLY-SECRET"}`))

	if got := st.ContaminatedBy([]byte(`{"body":"LATE-ONLY-SECRET"}`)); !hasSource(got, "tool_35") {
		t.Fatalf("a source beyond the union cap must still taint late results, got %v", got)
	}
	// Everything seen is assumed, so an early source is still covered too.
	if got := st.ContaminatedBy([]byte(`{"body":"LATE-ONLY-SECRET"}`)); len(got) == 0 {
		t.Fatal("overflow must over-taint, not clear the taint")
	}
}
