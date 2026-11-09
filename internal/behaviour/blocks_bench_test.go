package behaviour

import (
	"fmt"
	"testing"
)

// Pathological but valid: a long run of identical calls (a polling tool) and a
// long alternating pair. Both are single recordings a real agent can produce.
func benchCalls(kind string, n int) []CallRecord {
	calls := make([]CallRecord, 0, n)
	for i := 0; i < n; i++ {
		tool, key := "read_file", "k1"
		switch kind {
		case "alternating":
			if i%2 == 1 {
				tool, key = "write_file", "k2"
			}
		case "mixed":
			tool, key = fmt.Sprintf("tool-%d", i%7), fmt.Sprintf("k-%d", i%5)
		}
		calls = append(calls, CallRecord{Seq: uint64(i*2 + 2), Tool: tool, ArgsKey: key, ResultSeq: uint64(i*2 + 3), Status: "ok"})
	}
	return calls
}

func BenchmarkDetectBlocks(b *testing.B) {
	for _, kind := range []string{"identical", "alternating", "mixed"} {
		for _, n := range []int{500, 2000, 8000} {
			calls := benchCalls(kind, n)
			b.Run(fmt.Sprintf("%s/n=%d", kind, n), func(b *testing.B) {
				for i := 0; i < b.N; i++ {
					detectBlocks(calls)
				}
			})
		}
	}
}
