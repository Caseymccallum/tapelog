package scenario

import (
	"fmt"
	"strings"

	"github.com/Caseymccallum/tapelog/internal/policy"
	"github.com/Caseymccallum/tapelog/internal/replay"
)

// Failure is one assertion that did not hold.
type Failure struct {
	Check  string // human rendering of the check
	Detail string
}

// Run executes the scenario's assertions against a loaded cassette. When
// evaluator is non-nil, `allowed`/`denied`/invariants use re-evaluated
// verdicts (policy what-if semantics); otherwise recorded verdicts.
func Run(sc *Scenario, tape *replay.Tape, evaluator policy.Evaluator) []Failure {
	inter := tape.Interactions
	if evaluator != nil {
		inter = reevaluate(tape, evaluator)
	}
	// all is the full boundary trajectory: answered AND denied calls.
	// Verdict assertions and `attempted:` see everything that reached the
	// boundary; `called:`/`never_called:`/`sequence:` stay on answered
	// calls (side effects that actually happened).
	all := append(append([]replay.Interaction{}, inter...), tape.Unanswered...)
	if evaluator != nil {
		reev := map[int]replay.Interaction{}
		for _, it := range reevaluate(tape, evaluator) {
			reev[it.Order] = it
		}
		for i := range tape.Unanswered {
			u := tape.Unanswered[i]
			if r, ok := reev[u.Order]; ok {
				all[len(inter)+i] = r
			}
		}
	}

	var fails []Failure
	for _, c := range sc.Assert {
		switch {
		case c.Called != nil:
			spec := c.Called
			var filtered []replay.Interaction
			for _, it := range matching(inter, spec.Tool) {
				if argsSubset(it.Args, spec.Args) {
					filtered = append(filtered, it)
				}
			}
			n := len(filtered)
			switch {
			case spec.Times != nil && n != *spec.Times:
				fails = append(fails, Failure{
					fmt.Sprintf("called %s (times=%d)", spec.Tool, *spec.Times),
					fmt.Sprintf("matched %d call(s)", n),
				})
			case spec.Times == nil && n == 0:
				fails = append(fails, Failure{
					fmt.Sprintf("called %s", spec.Tool), "never called",
				})
			}
		case c.Attempted != nil:
			spec := c.Attempted
			var filtered []replay.Interaction
			for _, it := range matching(all, spec.Tool) {
				if argsSubset(it.Args, spec.Args) {
					filtered = append(filtered, it)
				}
			}
			n := len(filtered)
			switch {
			case spec.Times != nil && n != *spec.Times:
				fails = append(fails, Failure{
					fmt.Sprintf("attempted %s (times=%d)", spec.Tool, *spec.Times),
					fmt.Sprintf("matched %d attempt(s)", n),
				})
			case spec.Times == nil && n == 0:
				fails = append(fails, Failure{
					fmt.Sprintf("attempted %s", spec.Tool), "never reached the boundary",
				})
			}
		case c.NeverCalled != "":
			if hits := matching(inter, c.NeverCalled); len(hits) > 0 {
				fails = append(fails, Failure{
					fmt.Sprintf("never_called %s", c.NeverCalled),
					fmt.Sprintf("called %d time(s), first: %s", len(hits), compact(hits[0].Args)),
				})
			}
		case len(c.Sequence) > 0:
			if !isSubsequence(inter, c.Sequence) {
				fails = append(fails, Failure{
					"sequence " + strings.Join(c.Sequence, " -> "),
					"not an ordered subsequence of the trajectory",
				})
			}
		case c.TaintNever != nil:
			from := matching(inter, c.TaintNever.From)
			to := matching(inter, c.TaintNever.To)
			if len(from) > 0 && len(to) > 0 && to[0].Order > from[0].Order {
				fails = append(fails, Failure{
					fmt.Sprintf("taint_never %s -> %s", c.TaintNever.From, c.TaintNever.To),
					fmt.Sprintf("%s (order %d) followed %s (order %d)",
						c.TaintNever.To, to[0].Order, c.TaintNever.From, from[0].Order),
				})
			}
		case c.ResultContains != nil:
			ok := false
			for _, it := range matching(inter, c.ResultContains.Tool) {
				if strings.Contains(string(it.Result), c.ResultContains.Text) {
					ok = true
					break
				}
			}
			if !ok {
				fails = append(fails, Failure{
					fmt.Sprintf("result_contains %s %q", c.ResultContains.Tool, c.ResultContains.Text),
					"no matching recorded result contains the text",
				})
			}
		case c.Allowed != "":
			if !anyVerdict(all, c.Allowed, "allow", "confirm") {
				fails = append(fails, Failure{
					fmt.Sprintf("allowed %s", c.Allowed), "no call with an allowing verdict",
				})
			}
		case c.Denied != "":
			if !anyVerdict(all, c.Denied, "deny") {
				fails = append(fails, Failure{
					fmt.Sprintf("denied %s", c.Denied), "no call with verdict deny",
				})
			}
		case c.Invariant != "":
			fails = append(fails, runInvariant(c.Invariant, tape, evaluator)...)
		}
	}
	return fails
}

// runInvariant checks one boundary invariant.
func runInvariant(name string, tape *replay.Tape, ev policy.Evaluator) []Failure {
	switch name {
	case "no_deny_bypassed":
		// A deny verdict must never have produced a result.
		for _, it := range tape.Interactions {
			if it.Verdict == "deny" {
				return []Failure{{"invariant no_deny_bypassed",
					fmt.Sprintf("%s was denied yet produced a result", it.Tool)}}
			}
		}
		if ev != nil {
			for _, it := range tape.Interactions {
				if reevaluateOne(it, ev).Verdict == "deny" {
					return []Failure{{"invariant no_deny_bypassed",
						fmt.Sprintf("%s would be denied under policy yet produced a result", it.Tool)}}
				}
			}
		}
		return nil
	default:
		return []Failure{{"invariant " + name, "unknown invariant"}}
	}
}

// reevaluate re-runs the decision pipeline per call (what-if semantics):
// the same flow-aware walk the live mediator performs (replay.Reevaluate).
func reevaluate(tape *replay.Tape, ev policy.Evaluator) []replay.Interaction {
	return replay.Reevaluate(tape, ev, "")
}

func reevaluateOne(it replay.Interaction, ev policy.Evaluator) replay.Interaction {
	dec := ev.Evaluate(policy.Request{Tool: it.Tool, Args: it.Args})
	it.Verdict = string(dec.Verdict)
	it.RuleID = dec.RuleID
	return it
}
