package behaviour

import (
	"fmt"
	"testing"
)

// Repeats are measured inside a conversation. Four conversations that each call
// the same tool twice must not surface as one eight-occurrence loop.
func TestRepeatsDoNotCrossConversations(t *testing.T) {
	var calls []CallRecord
	seq := uint64(2)
	for c := 1; c <= 4; c++ {
		conv := fmt.Sprintf("conv-%d", c)
		for i := 0; i < 2; i++ {
			calls = append(calls, rec(seq, conv, "read_file", `{"path":"/x"}`, false))
			seq += 2
		}
	}
	for _, g := range GroupConversations(calls) {
		for _, f := range g.Repeats {
			if f.Count > 2 {
				t.Fatalf("conversation %q reported %d occurrences; it has 2 calls (cross-conversation merge)", g.ID, f.Count)
			}
		}
	}
}
