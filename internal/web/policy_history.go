package web

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/Caseymccallum/tapelog/internal/replay"
	"github.com/Caseymccallum/tapelog/internal/session"
	"sort"
	"strings"
)

func shortText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.ToValidUTF8(s[:n], "�") + "… [preview limited]"
}

// Match call histories by the same documented tool/argument key and occurrence
// order as the workbench comparison; do not invent associations for missing IDs.
func comparePolicyHistory(a, b *recording, at, bt *replay.Tape) map[string]any {
	type history struct {
		digest [32]byte
		count  int
	}
	collect := func(rec *recording, tape *replay.Tape) map[string][]history {
		ordered := append([]replay.Interaction{}, tape.Interactions...)
		ordered = append(ordered, tape.Unanswered...)
		sort.Slice(ordered, func(i, j int) bool { return ordered[i].Order < ordered[j].Order })
		events := map[uint64]session.Event{}
		for _, e := range rec.Events {
			events[e.Seq] = e
		}
		groups := map[string][]history{}
		for _, it := range ordered {
			idx := it.Order - 1
			if idx < 0 || idx >= len(rec.Activities) {
				continue
			}
			activity := rec.Activities[idx]
			values := []string{}
			for _, seq := range activity.DecisionSeqs {
				var p session.PolicyDecisionPayload
				if e, ok := events[seq]; ok && json.Unmarshal(e.Payload, &p) == nil {
					values = append(values, p.Verdict, p.RuleID, p.Reason)
				}
			}
			encoded, _ := json.Marshal(values)
			key := it.Tool + "(" + it.ArgsHash + ")"
			groups[key] = append(groups[key], history{sha256.Sum256(encoded), len(values) / 3})
		}
		return groups
	}
	am, bm := collect(a, at), collect(b, bt)
	keys := []string{}
	for key := range am {
		if _, ok := bm[key]; ok {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	changes := []string{}
	total := 0
	for _, key := range keys {
		x, y := am[key], bm[key]
		n := len(x)
		if len(y) < n {
			n = len(y)
		}
		for i := 0; i < n; i++ {
			if x[i].digest != y[i].digest {
				total++
				if len(changes) < 100 {
					changes = append(changes, fmt.Sprintf("%s occurrence %d: %d recorded decisions → %d; verdict, rule or reason changed", shortText(key, 160), i+1, x[i].count, y[i].count))
				}
			}
		}
	}
	return map[string]any{"changes": changes, "total": total}
}

// Views are capped without mutating the underlying evidence or assertion names.
func safeActivity(a *activity) *activity {
	if a == nil {
		return nil
	}
	v := *a
	v.Tool = shortText(v.Tool, 512)
	v.Server = shortText(v.Server, 512)
	v.Rule = shortText(v.Rule, 512)
	v.Reason = shortText(v.Reason, 2048)
	v.Verdict = shortText(v.Verdict, 256)
	v.ServerSource = shortText(v.ServerSource, 512)
	v.Link = shortText(v.Link, 512)
	if len(v.DecisionSeqs) > 20 {
		v.DecisionSeqs = v.DecisionSeqs[:20]
	}
	return &v
}
