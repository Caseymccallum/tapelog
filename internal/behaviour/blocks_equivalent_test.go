package behaviour

import (
	"encoding/json"
	"math/rand"
	"testing"
)

// detectBlocksReference is the ORIGINAL algorithm, kept verbatim as the oracle
// for the optimisation. It measures the run first and only then discards
// single-operation blocks, which is what made it quadratic on a long run of
// identical calls. Any drift in the optimised version shows up here.
func detectBlocksReference(calls []CallRecord) ([]RepeatFinding, map[int]bool) {
	covered := map[int]bool{}
	var out []RepeatFinding
	same := func(a, b int) bool {
		return calls[a].Tool == calls[b].Tool && calls[a].ArgsKey != "" && calls[a].ArgsKey == calls[b].ArgsKey
	}
	for i := 0; i < len(calls); i++ {
		matched := false
		for k := 2; k <= 4 && !matched; k++ {
			if i+2*k > len(calls) {
				continue
			}
			repeats := 1
			for {
				next := i + repeats*k
				if next+k > len(calls) {
					break
				}
				ok := true
				for j := 0; j < k; j++ {
					if !same(i+j, next+j) {
						ok = false
						break
					}
				}
				if !ok {
					break
				}
				repeats++
			}
			if repeats < MinRepeatCount {
				continue
			}
			distinct := map[string]bool{}
			for j := 0; j < k; j++ {
				distinct[calls[i+j].Tool+"\x00"+calls[i+j].ArgsKey] = true
			}
			if len(distinct) < 2 {
				continue
			}
			end := i + repeats*k
			f := RepeatFinding{Count: end - i, Repeats: repeats, BlockLength: k, Seqs: make([]uint64, 0, end-i)}
			seen := map[string]bool{}
			for j := i; j < end; j++ {
				f.Seqs = append(f.Seqs, calls[j].Seq)
				if !seen[calls[j].Tool] {
					seen[calls[j].Tool] = true
					f.Tools = append(f.Tools, calls[j].Tool)
				}
				covered[j] = true
			}
			f.FirstSeq, f.LastSeq = f.Seqs[0], f.Seqs[len(f.Seqs)-1]
			f.FirstIndex = i
			out = append(out, f)
			i = end - 1
			matched = true
		}
	}
	return out, covered
}

func shape(fs []RepeatFinding) string {
	type s struct {
		Count, Repeats, BlockLength, FirstIndex int
		Seqs                                    []uint64
		Tools                                   []string
	}
	out := make([]s, 0, len(fs))
	for _, f := range fs {
		out = append(out, s{f.Count, f.Repeats, f.BlockLength, f.FirstIndex, f.Seqs, f.Tools})
	}
	b, _ := json.Marshal(out)
	return string(b)
}

// The optimisation may not change WHAT is detected, only how fast.
func TestDetectBlocksMatchesReference(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	tools := []string{"read_file", "write_file", "search"}
	for _, n := range []int{0, 1, 2, 5, 40, 300} {
		for trial := 0; trial < 40; trial++ {
			calls := make([]CallRecord, n)
			for i := range calls {
				// A small key space produces long identical runs, alternating
				// pairs and real blocks, which is exactly the interesting mix.
				calls[i] = CallRecord{
					Seq:     uint64(i*2 + 2),
					Tool:    tools[rng.Intn(len(tools))],
					ArgsKey: string(rune('a' + rng.Intn(3))),
				}
			}
			got, gotCovered := detectBlocks(calls)
			want, wantCovered := detectBlocksReference(calls)
			if shape(got) != shape(want) {
				t.Fatalf("n=%d trial=%d: findings differ\n got %s\nwant %s", n, trial, shape(got), shape(want))
			}
			if len(gotCovered) != len(wantCovered) {
				t.Fatalf("n=%d trial=%d: coverage differs (%d vs %d)", n, trial, len(gotCovered), len(wantCovered))
			}
		}
	}
}
