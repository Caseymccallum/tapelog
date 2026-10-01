package replay

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"

	"github.com/Caseymccallum/tapelog/internal/policy"
	taintstore "github.com/Caseymccallum/tapelog/internal/taint"
)

// reevaluateOrders walks the whole session in recorded order mirroring the
// live mediator pipeline — value-level taint rebuilt from recorded results
// (CaMeL-style contamination), then session-mode flows, then per-call
// rules — and returns the re-evaluated interaction per recorded order
// (verdict + rule filled in). Covers answered AND unanswered calls.
func reevaluateOrders(tape *Tape, p policy.Evaluator, task string) map[int]Interaction {
	type call struct {
		order  int
		base   Interaction
		result json.RawMessage // nil for unanswered (denied) calls
	}
	var calls []call
	for _, it := range tape.Interactions {
		calls = append(calls, call{it.Order, it, it.Result})
	}
	for _, it := range tape.Unanswered {
		calls = append(calls, call{it.Order, it, nil})
	}
	sort.Slice(calls, func(i, j int) bool { return calls[i].order < calls[j].order })

	var taint policy.TaintState
	values := taintstore.New(0, 0)
	flowChecker, hasFlows := p.(policy.FlowChecker)
	valueChecker, hasValueFlows := p.(policy.ValueFlowChecker)

	out := map[int]Interaction{}
	for _, c := range calls {
		var dec policy.Decision
		decided := false
		if hasValueFlows {
			if fdec, applied := valueChecker.CheckFlowValues(&taint, c.base.Tool, values.ContaminatedBy(c.base.Args)); applied {
				dec, decided = fdec, true
			}
		}
		if !decided && hasFlows {
			if fdec, applied := flowChecker.CheckFlow(&taint, c.base.Tool); applied {
				dec, decided = fdec, true
			}
		}
		if !decided {
			dec = p.Evaluate(policy.Request{Tool: c.base.Tool, Args: c.base.Args, Task: task})
		}
		if dec.Verdict == policy.VerdictAllow {
			taint.Record(c.base.Tool)
		}
		if len(c.result) > 0 {
			values.Mark(c.base.Tool, c.result) // value taint derives from results
		}
		c.base.Verdict = string(dec.Verdict)
		c.base.RuleID = dec.RuleID
		out[c.order] = c.base
	}
	return out
}

// Reevaluate returns the answered interactions carrying the verdicts the
// policy would have produced (same order/shape as tape.Interactions).
// Used by scenario `policy:` re-evaluation.
func Reevaluate(tape *Tape, p policy.Evaluator, task string) []Interaction {
	orders := reevaluateOrders(tape, p, task)
	out := make([]Interaction, len(tape.Interactions))
	for i, it := range tape.Interactions {
		out[i] = orders[it.Order]
	}
	return out
}

// WhatIf re-evaluates every recorded call against p and diffs the verdicts
// against the recorded ones, printing each change to out. Returns the
// number of verdicts that would change.
func WhatIf(out io.Writer, tape *Tape, p policy.Evaluator, policyPath, task string) int {
	type call struct {
		order int
		it    Interaction
	}
	var calls []call
	for _, it := range tape.Interactions {
		calls = append(calls, call{it.Order, it})
	}
	for _, it := range tape.Unanswered {
		calls = append(calls, call{it.Order, it})
	}
	sort.Slice(calls, func(i, j int) bool { return calls[i].order < calls[j].order })

	reeval := reevaluateOrders(tape, p, task)

	changed := 0
	for _, c := range calls {
		dec := reeval[c.order]
		if dec.Verdict == c.it.Verdict {
			continue
		}
		changed++
		fmt.Fprintf(out, "~ %s %s\n", c.it.Tool, c.it.Args)
		fmt.Fprintf(out, "    recorded: %-7s (%s)\n", c.it.Verdict, c.it.RuleID)
		fmt.Fprintf(out, "    what-if:  %-7s (%s) — re-evaluation differs\n", dec.Verdict, dec.RuleID)
	}
	fmt.Fprintf(out, "\n%d calls re-evaluated against %s: %d same, %d changed\n",
		len(calls), policyPath, len(calls)-changed, changed)
	return changed
}