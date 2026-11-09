package behaviour

import (
	"fmt"
	"testing"
)

// GroupConversations must stay linear: recording libraries can be large and the
// grouping runs on every behaviour request.
func BenchmarkGroupConversations(b *testing.B) {
	for _, n := range []int{100, 2000, 12000} {
		calls := make([]CallRecord, 0, n)
		for i := 0; i < n; i++ {
			calls = append(calls, CallRecord{
				Seq: uint64(i*2 + 2), Tool: "read_file",
				ArgsKey: "k", ConversationID: fmt.Sprintf("conv-%d", i),
				ResultSeq: uint64(i*2 + 3),
			})
		}
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				GroupConversations(calls)
			}
		})
	}
}
