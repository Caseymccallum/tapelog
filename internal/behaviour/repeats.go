package behaviour

import (
	"fmt"
	"sort"
	"strings"
)

const (
	MinRepeatCount        = 3
	MaxFindingsPerSession = 20
)

type CallRecord struct {
	Seq        uint64   `json:"seq"`
	TS         string   `json:"ts,omitempty"`
	EpochMS    int64    `json:"-"` // parsed TS; 0 when absent or unparseable
	Tool       string   `json:"tool"`
	Server     string   `json:"server,omitempty"`
	ArgsKey    string   `json:"-"`
	ResultKey  string   `json:"-"`
	Status     string   `json:"status"`
	Verdict    string   `json:"verdict,omitempty"`
	Rule       string   `json:"rule,omitempty"`
	IsError    bool     `json:"is_error,omitempty"`
	DurationMS *float64 `json:"duration_ms"`
	Signature  string   `json:"signature,omitempty"`
	ResultSeq  uint64   `json:"result_seq,omitempty"`
}

// Repeat findings use a fixed vocabulary so the interface never has to invent a
// category. Only the last two are interpretations; the rest are observations.
const (
	KindPossibleRetryLoop = "possible_retry_loop"
	KindRepeatedFailure   = "repeated_failure"
	KindRepeatedDenial    = "repeated_denial"
	KindRepeatedActivity  = "repeated_activity"
	KindAlternating       = "alternating_sequence"
	KindRepeatingBlock    = "repeating_block"
	KindInsufficient      = "insufficient_evidence"
)

// RepeatFinding is one deterministic observation about repetition inside a
// single recording. It always carries the calls it was derived from, the
// evidence that produced it, and an interpretation that states what the
// recording cannot tell you.
type RepeatFinding struct {
	Kind             string   `json:"kind"`
	Tool             string   `json:"tool,omitempty"`
	Tools            []string `json:"tools,omitempty"`
	Count            int      `json:"count"`
	Consecutive      int      `json:"consecutive,omitempty"`
	Repeats          int      `json:"repeats,omitempty"`
	BlockLength      int      `json:"block_length,omitempty"`
	Seqs             []uint64 `json:"seqs"`
	FirstSeq         uint64   `json:"first_seq"`
	LastSeq          uint64   `json:"last_seq"`
	FirstIndex       int      `json:"first_index"`
	IdenticalResults bool     `json:"identical_results,omitempty"`
	MedianGapMS      *float64 `json:"median_gap_ms"`
	Evidence         string   `json:"evidence"`
	Interpretation   string   `json:"interpretation"`
	Confidence       string   `json:"confidence"`
	Signature        string   `json:"signature,omitempty"`
}

// detectRepeats runs the deterministic detectors over one recording's calls.
//
// Thresholds: three occurrences of the same argument set, or three repetitions
// of the same short block. Two identical failing calls are reported, but only
// as insufficient evidence, because a single retry is ordinary behaviour.
func DetectRepeats(calls []CallRecord) []RepeatFinding {
	if len(calls) < 2 {
		return nil
	}
	var out []RepeatFinding
	blockFindings, covered := detectBlocks(calls)
	out = append(out, blockFindings...)

	type key struct{ tool, args string }
	groups := map[key][]int{}
	var order []key
	for i, c := range calls {
		if c.ArgsKey == "" {
			continue // arguments were unreadable; grouping on nothing would over-merge
		}
		k := key{c.Tool, c.ArgsKey}
		if _, seen := groups[k]; !seen {
			order = append(order, k)
		}
		groups[k] = append(groups[k], i)
	}
	for _, k := range order {
		idx := groups[k]
		if len(idx) < 2 || allCovered(idx, covered) {
			continue
		}
		if f := classifyGroup(calls, idx, k.args); f != nil {
			out = append(out, *f)
		}
	}
	sortFindings(out)
	if len(out) > MaxFindingsPerSession {
		out = out[:MaxFindingsPerSession]
	}
	return out
}

func allCovered(idx []int, covered map[int]bool) bool {
	for _, i := range idx {
		if !covered[i] {
			return false
		}
	}
	return true
}

// classifyGroup turns one group of identical-argument calls into a finding.
// The digest is included so a reader can confirm two calls really share
// arguments without the workbench printing argument values.
func classifyGroup(calls []CallRecord, idx []int, digest string) *RepeatFinding {
	var errored, denied, succeeded, unanswered int
	resultKeys := map[string]bool{}
	for _, i := range idx {
		c := calls[i]
		switch {
		case c.ResultKey != "" && c.IsError:
			errored++
		case c.ResultKey != "":
			succeeded++
		case c.Verdict == "deny":
			denied++
		default:
			unanswered++
		}
		if c.ResultKey != "" {
			resultKeys[c.ResultKey] = true
		}
	}
	seqs := make([]uint64, 0, len(idx))
	for _, i := range idx {
		seqs = append(seqs, calls[i].Seq)
	}
	n := len(idx)
	f := &RepeatFinding{
		Tool:             calls[idx[0]].Tool,
		Count:            n,
		Consecutive:      longestRun(idx),
		Seqs:             seqs,
		FirstSeq:         seqs[0],
		LastSeq:          seqs[len(seqs)-1],
		FirstIndex:       idx[0],
		MedianGapMS:      medianGap(calls, idx),
		Signature:        calls[idx[0]].Signature,
		IdenticalResults: len(resultKeys) == 1,
	}
	identity := fmt.Sprintf("argument digest %s", shortDigest(digest))
	switch {
	case denied == n && n >= MinRepeatCount:
		f.Kind = KindRepeatedDenial
		f.Confidence = "observed"
		f.Evidence = fmt.Sprintf("%d calls with the same tool and %s were all denied by the boundary", n, identity)
		f.Interpretation = "The boundary denied every one of these identical attempts. The recording shows the denials, not whether the agent understood them: a repeated denial may be a probe, a retry, or an agent that discarded the error."
	case errored == n && n >= MinRepeatCount:
		f.Kind = KindRepeatedFailure
		f.Confidence = "observed"
		f.Evidence = fmt.Sprintf("%d calls with %s all returned recorded errors", n, identity)
		if f.IdenticalResults {
			f.Kind = KindPossibleRetryLoop
			f.Confidence = "possible"
			f.Evidence += "; every error result is byte-identical"
			f.Interpretation = "Possible retry loop: identical arguments produced byte-identical error results each time, so no attempt after the first could have made progress. This is an interpretation drawn from the recording — the agent's reasoning is not recorded — and a deliberate bounded retry can look the same."
		} else {
			f.Interpretation = "The same call failed repeatedly with differing error detail. Repeated failure is observed behaviour, not proof that the agent was stuck: a bounded retry against a flaky server produces the same trace."
		}
	case errored >= 2:
		f.Kind = KindRepeatedFailure
		f.Confidence = "observed"
		f.Evidence = fmt.Sprintf("%d calls with %s produced %d errors, %d recorded results and %d denials", n, identity, errored, succeeded, denied)
		f.Interpretation = "The same call repeatedly failed alongside other outcomes. Read this as a prompt to check the evidence, not as a diagnosis: Tapelog records outcomes, not the decisions behind them."
	case succeeded == n && n >= MinRepeatCount:
		f.Kind = KindRepeatedActivity
		f.Confidence = "observed"
		f.Evidence = fmt.Sprintf("%d calls with %s each recorded a result", n, identity)
		if f.Consecutive >= MinRepeatCount {
			f.Evidence += fmt.Sprintf("; %d of them ran back to back", f.Consecutive)
		}
		f.Interpretation = "Repeated activity observed. Tapelog cannot tell whether it was necessary: polling, pagination and read-after-write loops call the same tool with the same arguments by design. Whether this repetition is worth attention is a judgement about your tool, not a finding."
	case unanswered == n:
		f.Kind = KindInsufficient
		f.Confidence = "insufficient"
		f.Evidence = fmt.Sprintf("%d calls with %s have no recorded result and no recorded decision", n, identity)
		f.Interpretation = "Insufficient evidence: these calls were never answered, so the recording cannot show whether they made progress."
	case n < MinRepeatCount:
		// Two identical calls are reported, but only as a statement about the
		// evidence: a single retry, a confirmation prompt or a poll pair is
		// ordinary behaviour and cannot support a pattern claim.
		f.Kind = KindInsufficient
		f.Confidence = "insufficient"
		f.Evidence = fmt.Sprintf("%d calls with %s produced %d errors, %d recorded results and %d denials", n, identity, errored, succeeded, denied)
		f.Interpretation = fmt.Sprintf("Insufficient evidence: %d identical calls are below the %d-occurrence threshold Tapelog uses to report a repeating pattern, and a single retry is ordinary behaviour.", n, MinRepeatCount)
	default:
		return nil
	}
	return f
}

// shortDigest abbreviates a stored argument digest for display.
func shortDigest(digest string) string {
	if len(digest) > 8 {
		return digest[:8]
	}
	return digest
}

// longestRun reports the longest run of consecutive array positions.
func longestRun(idx []int) int {
	best, run := 1, 1
	for i := 1; i < len(idx); i++ {
		if idx[i] == idx[i-1]+1 {
			run++
		} else {
			run = 1
		}
		if run > best {
			best = run
		}
	}
	return best
}

// medianGap reports the middle inter-call interval when timestamps exist. It is
// the only way to separate a rapid burst from slow polling, so it is reported as
// data rather than turned into a conclusion.
func medianGap(calls []CallRecord, idx []int) *float64 {
	var gaps []int64
	var prev int64
	for _, i := range idx {
		ts := calls[i].EpochMS
		if ts == 0 {
			continue
		}
		if prev != 0 && ts > prev {
			gaps = append(gaps, ts-prev)
		}
		prev = ts
	}
	if len(gaps) == 0 {
		return nil
	}
	sort.Slice(gaps, func(i, j int) bool { return gaps[i] < gaps[j] })
	mid := float64(gaps[len(gaps)/2])
	return &mid
}

// detectBlocks finds short call sequences that repeat, in order, at least three
// times. It scans left to right and consumes each match, so one long repeated
// run produces one finding rather than one per starting offset. Block length 1
// is excluded on purpose: single-call repeats belong to the group detector and
// would otherwise be reported twice.
func detectBlocks(calls []CallRecord) ([]RepeatFinding, map[int]bool) {
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
			// A block of identical calls is a single-call run, which the group
			// detector owns. Requiring two distinct calls keeps a polling tool
			// from being reported as an "alternating sequence".
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
			f.Confidence = "observed"
			f.Tool = strings.Join(f.Tools, " → ")
			if k == 2 {
				f.Kind = KindAlternating
				f.Evidence = fmt.Sprintf("two calls alternated %d times in the same order with identical arguments", repeats)
				f.Interpretation = "An alternating sequence was observed. Some agents iterate over a fixed list of two operations; read the arguments in the timeline before treating this as a loop."
			} else {
				f.Kind = KindRepeatingBlock
				f.Evidence = fmt.Sprintf("a block of %d calls repeated %d times in the same order with identical arguments", k, repeats)
				f.Interpretation = "A repeating sequence was observed. It may be legitimate batch work over a fixed list; Tapelog reports the shape of the repetition, not its purpose."
			}
			out = append(out, f)
			i = end - 1
			matched = true
		}
	}
	return out, covered
}

// sortFindings orders observations by how much attention they deserve, then by
// size and recording order, so the list is deterministic.
func sortFindings(f []RepeatFinding) {
	rank := map[string]int{
		KindPossibleRetryLoop: 0, KindRepeatedFailure: 1, KindRepeatedDenial: 2,
		KindAlternating: 3, KindRepeatingBlock: 3, KindRepeatedActivity: 4, KindInsufficient: 5,
	}
	sort.SliceStable(f, func(i, j int) bool {
		if rank[f[i].Kind] != rank[f[j].Kind] {
			return rank[f[i].Kind] < rank[f[j].Kind]
		}
		if f[i].Count != f[j].Count {
			return f[i].Count > f[j].Count
		}
		return f[i].FirstSeq < f[j].FirstSeq
	})
}
