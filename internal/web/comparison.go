package web

import (
	"crypto/sha256"
	"fmt"
	"github.com/Caseymccallum/tapelog/internal/replay"
	"sort"
)

// Compare separately from the CLI's answered-call diff.
func compareAttempts(a, b *replay.Tape) map[string]any {
	type item struct {
		key, outcome string
		order        int
	}
	collect := func(t *replay.Tape) []item {
		out := []item{}
		add := func(it replay.Interaction, answered bool) {
			outcome := "unanswered"
			if answered {
				sum := sha256.Sum256(it.Result)
				outcome = fmt.Sprintf("result error=%t sha256=%x", it.IsError, sum[:6])
			}
			out = append(out, item{it.Tool + "(" + it.ArgsHash + ")", outcome + "; last policy=" + it.Verdict + "; rule=" + it.RuleID, it.Order})
		}
		for _, it := range t.Interactions {
			add(it, true)
		}
		for _, it := range t.Unanswered {
			add(it, false)
		}
		sort.Slice(out, func(i, j int) bool { return out[i].order < out[j].order })
		return out
	}
	aa, bb := collect(a), collect(b)
	groups := func(xs []item) map[string][]item {
		m := map[string][]item{}
		for _, x := range xs {
			m[x.key] = append(m[x.key], x)
		}
		return m
	}
	am, bm := groups(aa), groups(bb)
	all := map[string]bool{}
	for k := range am {
		all[k] = true
	}
	for k := range bm {
		all[k] = true
	}
	keys := []string{}
	for k := range all {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	changes := []string{}
	ambiguous := false
	total := 0
	appendChange := func(s string) {
		total++
		if len(changes) < 100 {
			changes = append(changes, s)
		}
	}
	for _, k := range keys {
		x, y := am[k], bm[k]
		if len(x) > 1 || len(y) > 1 {
			ambiguous = true
		}
		label := k
		if len(label) > 160 {
			label = label[:160] + "..."
		}
		n := len(x)
		if len(y) > n {
			n = len(y)
		}
		for i := 0; i < n; i++ {
			if i >= len(x) {
				appendChange(fmt.Sprintf("Additional in compared recording: %s occurrence %d", label, i+1))
			} else if i >= len(y) {
				appendChange(fmt.Sprintf("Only in this recording: %s occurrence %d", label, i+1))
			} else if x[i].outcome != y[i].outcome {
				appendChange(fmt.Sprintf("Changed %s occurrence %d: %s -> %s", label, i+1, x[i].outcome, y[i].outcome))
			}
		}
	}
	sequenceDiff := len(aa) != len(bb)
	if !sequenceDiff {
		for i := range aa {
			if aa[i].key != bb[i].key {
				sequenceDiff = true
				break
			}
		}
	}
	return map[string]any{"attempts_a": len(aa), "attempts_b": len(bb), "changes": changes, "change_count": total, "ambiguous_duplicates": ambiguous, "sequence_differs": sequenceDiff}
}
