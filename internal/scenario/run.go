package scenario

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Caseymccallum/tapelog/internal/policy"
	"github.com/Caseymccallum/tapelog/internal/replay"
	taintstore "github.com/Caseymccallum/tapelog/internal/taint"
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
			if f := countCheck("called "+spec.Tool, len(filtered), spec.Times, spec.Min, spec.Max, "call(s)"); f != nil {
				fails = append(fails, *f)
			}
		case c.Attempted != nil:
			spec := c.Attempted
			var filtered []replay.Interaction
			for _, it := range matching(all, spec.Tool) {
				if argsSubset(it.Args, spec.Args) {
					filtered = append(filtered, it)
				}
			}
			if f := countCheck("attempted "+spec.Tool, len(filtered), spec.Times, spec.Min, spec.Max, "attempt(s)"); f != nil {
				fails = append(fails, *f)
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
		case c.FlowDenied != nil:
			hits := flowHits(all, c.FlowDenied)
			n := 0
			for _, h := range hits {
				if h.Verdict == "deny" {
					n++
				}
			}
			if f := countCheck(fmt.Sprintf("flow_denied %s -> %s", c.FlowDenied.From, c.FlowDenied.To),
				n, c.FlowDenied.Times, nil, nil, "blocked flow(s)"); f != nil {
				fails = append(fails, *f)
			}
		case c.FlowAttempted != nil:
			hits := flowHits(all, c.FlowAttempted)
			if f := countCheck(fmt.Sprintf("flow_attempted %s -> %s", c.FlowAttempted.From, c.FlowAttempted.To),
				len(hits), c.FlowAttempted.Times, nil, nil, "flow decision(s)"); f != nil {
				fails = append(fails, *f)
			}
		case c.MaxDepth != nil:
			if depth, chain := maxChainDepth(all); depth > *c.MaxDepth {
				fails = append(fails, Failure{
					fmt.Sprintf("max_depth %d", *c.MaxDepth),
					fmt.Sprintf("dependency chain of depth %d: %s", depth, strings.Join(chain, " -> ")),
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

// countCheck applies the count semantics for called/attempted and flow
// specs: `times` is exact; `min_times`/`max_times` bound the count
// (absent sides default to "at least one" below and unbounded above).
// Returns nil when the count is acceptable.
func countCheck(label string, n int, exact, min, max *int, unit string) *Failure {
	lo, hi := 1, -1 // default: at least one, no ceiling
	if exact != nil {
		lo, hi = *exact, *exact
	}
	if min != nil {
		lo = *min
	}
	if max != nil {
		hi = *max
	}
	switch {
	case n < lo:
		detail := "never matched"
		if n > 0 {
			detail = fmt.Sprintf("matched only %d %s", n, unit)
		}
		if lo == hi {
			return &Failure{fmt.Sprintf("%s (times=%d)", label, lo),
				fmt.Sprintf("matched %d %s, want exactly %d", n, unit, lo)}
		}
		return &Failure{fmt.Sprintf("%s (min_times=%d)", label, lo), detail}
	case hi >= 0 && n > hi:
		return &Failure{fmt.Sprintf("%s (max_times=%d)", label, hi),
			fmt.Sprintf("matched %d %s, want at most %d", n, unit, hi)}
	}
	return nil
}

// flowHits returns the interactions whose recorded decision was produced
// by a flow rule naming a source that matches the spec's `from` glob (the
// `[taint sources: …]` / `[contaminated by: …]` provenance the policy
// engine stamps on flow decisions), with the tool matching `to`.
func flowHits(all []replay.Interaction, spec *FlowSpec) []replay.Interaction {
	toRe := globToRegex(spec.To)
	fromRe := globToRegex(spec.From)
	if toRe == nil || fromRe == nil {
		return nil
	}
	var out []replay.Interaction
	for _, it := range all {
		if !toRe.MatchString(it.Tool) {
			continue
		}
		for _, src := range flowSources(it.Reason) {
			if fromRe.MatchString(src) || fromRe.MatchString(denamespace(src)) {
				out = append(out, it)
				break
			}
		}
	}
	return out
}

// denamespace strips a mux tool namespace (`a__read_secrets` →
// `read_secrets`) so scenario globs can match either form.
func denamespace(name string) string {
	if i := strings.Index(name, "__"); i > 0 {
		return name[i+2:]
	}
	return name
}

// flowSources extracts the source tool names from a decision reason's
// flow provenance bracket. Mirrors the reason format policy.flowDecision
// stamps: "… [taint sources: a, b]" (session mode) or
// "… [contaminated by: a, b]" (value mode). Names keep their mux
// namespace (`a__read_secrets`); match globs against either form.
func flowSources(reason string) []string {
	for _, marker := range []string{"[taint sources: ", "[contaminated by: "} {
		if i := strings.LastIndex(reason, marker); i >= 0 {
			rest := reason[i+len(marker):]
			if j := strings.IndexByte(rest, ']'); j >= 0 {
				var out []string
				for _, name := range strings.Split(rest[:j], ",") {
					if name = strings.TrimSpace(name); name != "" {
						out = append(out, name)
					}
				}
				return out
			}
		}
	}
	return nil
}

// maxChainDepth walks the trajectory in recorded order and returns the
// longest data-dependency chain — a call whose arguments carry recorded
// values from an earlier call's result extends that call's chain by one
// hop (same value-contamination matching as `flows: mode: value`,
// internal/taint). A chain's rendered form lists the tools in order.
func maxChainDepth(all []replay.Interaction) (int, []string) {
	order := append([]replay.Interaction{}, all...)
	sort.SliceStable(order, func(i, j int) bool { return order[i].Order < order[j].Order })

	values := taintstore.New(0, 0)
	type node struct {
		tool  string
		depth int
		chain []string
	}
	var prior []node // completed nodes, in recorded order
	best := node{depth: 0}
	for _, it := range order {
		n := node{tool: it.Tool, depth: 1, chain: []string{it.Tool}}
		for _, src := range values.ContaminatedBy(it.Args) {
			// Sources are tool names; the deepest earlier chain ending in
			// that tool is the extension point.
			for _, prev := range prior {
				if prev.tool != src {
					continue
				}
				if prev.depth+1 > n.depth {
					n.depth = prev.depth + 1
					n.chain = append(append([]string{}, prev.chain...), it.Tool)
				}
			}
		}
		prior = append(prior, n)
		if n.depth > best.depth {
			best = n
		}
		if len(it.Result) > 0 {
			values.Mark(it.Tool, it.Result) // value taint derives from results
		}
	}
	return best.depth, best.chain
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
	it.Reason = dec.Reason
	return it
}
