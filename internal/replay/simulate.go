package replay

import (
	"fmt"
	"sort"

	"github.com/Caseymccallum/tapelog/internal/policy"
)

// orderedInteraction is one recorded call in recorded order, answered or not.
type orderedInteraction struct {
	order int
	it    Interaction
}

// orderedCalls returns answered and unanswered calls in recorded order. Both
// WhatIf and Simulate use this single ordering so their counts cannot drift.
func orderedCalls(tape *Tape) []orderedInteraction {
	calls := make([]orderedInteraction, 0, len(tape.Interactions)+len(tape.Unanswered))
	for _, it := range tape.Interactions {
		calls = append(calls, orderedInteraction{it.Order, it})
	}
	for _, it := range tape.Unanswered {
		calls = append(calls, orderedInteraction{it.Order, it})
	}
	sort.Slice(calls, func(i, j int) bool { return calls[i].order < calls[j].order })
	return calls
}

// SimulatedCall is one recorded call as seen by a candidate policy.
type SimulatedCall struct {
	Order            int    `json:"order"`
	Tool             string `json:"tool"`
	Answered         bool   `json:"answered"`
	RecordedVerdict  string `json:"recorded_verdict"`
	RecordedRule     string `json:"recorded_rule"`
	CandidateVerdict string `json:"candidate_verdict"`
	CandidateRule    string `json:"candidate_rule"`
	CandidateReason  string `json:"candidate_reason"`
	Changed          bool   `json:"changed"`
	Transition       string `json:"transition"`
}

// Simulation is the structured result of re-evaluating a recording against a
// candidate policy through the same engine the boundary uses.
type Simulation struct {
	Calls        []SimulatedCall `json:"calls"`
	Total        int             `json:"total"`
	Changed      int             `json:"changed"`
	Same         int             `json:"same"`
	NewlyDenied  int             `json:"newly_denied"`
	NewlyAllowed int             `json:"newly_allowed"`
	NewlyConfirm int             `json:"newly_confirm"`
	OtherChanges int             `json:"other_changes"`
}

// Simulate re-evaluates every recorded call against p and reports both the
// recorded and the candidate decision. It delegates to reevaluateOrders, so
// value-level taint, session taint and rule precedence are exactly the
// live/CLI what-if semantics. It never executes a tool call.
func Simulate(tape *Tape, p policy.Evaluator, task string) Simulation {
	calls := orderedCalls(tape)
	reeval := reevaluateOrders(tape, p, task)
	sim := Simulation{Calls: make([]SimulatedCall, 0, len(calls)), Total: len(calls)}
	for _, c := range calls {
		dec := reeval[c.order]
		call := SimulatedCall{
			Order:            c.order,
			Tool:             c.it.Tool,
			Answered:         len(c.it.Result) > 0,
			RecordedVerdict:  c.it.Verdict,
			RecordedRule:     c.it.RuleID,
			CandidateVerdict: string(dec.Verdict),
			CandidateRule:    dec.RuleID,
			CandidateReason:  dec.Reason,
		}
		if call.RecordedVerdict != call.CandidateVerdict {
			call.Changed = true
			call.Transition = call.RecordedVerdict + "->" + call.CandidateVerdict
			sim.Changed++
			switch call.CandidateVerdict {
			case string(policy.VerdictDeny):
				sim.NewlyDenied++
			case string(policy.VerdictAllow):
				sim.NewlyAllowed++
			case string(policy.VerdictConfirm):
				sim.NewlyConfirm++
			default:
				sim.OtherChanges++
			}
		} else {
			sim.Same++
		}
		sim.Calls = append(sim.Calls, call)
	}
	return sim
}

// Describe is the human summary the CLI and the workbench share.
func (s Simulation) Describe(policyName string) string {
	return fmt.Sprintf("%d calls re-evaluated against %s: %d same, %d changed",
		s.Total, policyName, s.Same, s.Changed)
}
