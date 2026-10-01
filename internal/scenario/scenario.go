// Package scenario implements `tapelog test`: behavioral regression at
// the tool boundary. A scenario asserts on the tool-call trajectory of a
// recorded cassette — exact, deterministic, no LLM judge required
// (docs/RESEARCH-EVOLUTION.md "tapelog test").
package scenario

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Scenario is one test file: a cassette fixture plus assertions.
type Scenario struct {
	Version int     `yaml:"version"`
	Name    string  `yaml:"scenario"`
	Fixture string  `yaml:"fixture"` // recorded session JSONL (cassette)
	Policy  string  `yaml:"policy"`  // optional: re-evaluate verdicts under this policy
	Assert  []Check `yaml:"assert"`
}

// CalledSpec matches tool calls: glob tool, optional args subset, count.
type CalledSpec struct {
	Tool  string         `yaml:"tool"`
	Args  map[string]any `yaml:"args"`
	Times *int           `yaml:"times"`     // exact count; nil = at least one
	Min   *int           `yaml:"min_times"` // count floor (with max_times: bounds instead of exact)
	Max   *int           `yaml:"max_times"` // count ceiling ("at most N")
}

// AttemptSpec matches calls that reached the boundary (answered OR
// denied) — "the agent tried this", regardless of what the boundary did.
type AttemptSpec struct {
	Tool  string         `yaml:"tool"`
	Args  map[string]any `yaml:"args"`
	Times *int           `yaml:"times"`
	Min   *int           `yaml:"min_times"`
	Max   *int           `yaml:"max_times"`
}

// TaintSpec forbids a flow: no `To` call after a `From` call.
type TaintSpec struct {
	From string `yaml:"from"`
	To   string `yaml:"to"`
}

// FlowSpec asserts over a toxic-flow pair (policy `flows:` semantics):
// calls to `To` whose recorded decision names a source matching `From`
// in its flow provenance (`[taint sources: …]` / `[contaminated by: …]`).
type FlowSpec struct {
	From  string `yaml:"from"`
	To    string `yaml:"to"`
	Times *int   `yaml:"times"` // exact count; nil = at least one
}

// ResultSpec asserts text appears in a tool's recorded result.
type ResultSpec struct {
	Tool string `yaml:"tool"`
	Text string `yaml:"text"`
}

// Check is one assertion. Exactly one field is set (enforced at parse):
//
//	called:        {tool, args?, times?|min_times?/max_times?}  (answered calls)
//	attempted:     {tool, args?, times?|min_times?/max_times?}  (answered + denied — boundary saw it)
//	never_called:  glob                  (answered calls)
//	sequence:      [tool, tool, ...]   (ordered subsequence of the trajectory)
//	taint_never:   {from, to}
//	flow_denied:   {from, to, times?}   (toxic flow attempted AND blocked by a flow rule)
//	flow_attempted: {from, to, times?}  (toxic flow reached a flow-rule decision, any verdict)
//	result_contains: {tool, text}
//	allowed / denied: glob             (verdict assertions, answered + denied)
//	invariant:     name                (boundary invariant)
//	max_depth:     N                   (data-dependency chain depth ceiling)
type Check struct {
	Called         *CalledSpec  `yaml:"called,omitempty"`
	Attempted      *AttemptSpec `yaml:"attempted,omitempty"`
	NeverCalled    string       `yaml:"never_called,omitempty"`
	Sequence       []string     `yaml:"sequence,omitempty"`
	TaintNever     *TaintSpec   `yaml:"taint_never,omitempty"`
	FlowDenied     *FlowSpec    `yaml:"flow_denied,omitempty"`
	FlowAttempted  *FlowSpec    `yaml:"flow_attempted,omitempty"`
	ResultContains *ResultSpec  `yaml:"result_contains,omitempty"`
	Allowed        string       `yaml:"allowed,omitempty"`
	Denied         string       `yaml:"denied,omitempty"`
	Invariant      string       `yaml:"invariant,omitempty"`
	MaxDepth       *int         `yaml:"max_depth,omitempty"`
}

// Load parses one scenario file and validates it.
func Load(path string) (*Scenario, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var sc Scenario
	if err := yaml.Unmarshal(data, &sc); err != nil {
		return nil, fmt.Errorf("scenario %s: %w", path, err)
	}
	if sc.Version != 1 {
		return nil, fmt.Errorf("scenario %s: unsupported version %d (want 1)", path, sc.Version)
	}
	if sc.Fixture == "" {
		return nil, fmt.Errorf("scenario %s: fixture is required", path)
	}
	for i, c := range sc.Assert {
		n := 0
		if c.Called != nil {
			n++
			if err := validateCount("called", c.Called.Times, c.Called.Min, c.Called.Max); err != nil {
				return nil, fmt.Errorf("scenario %s: assert[%d]: %w", path, i, err)
			}
		}
		if c.Attempted != nil {
			n++
			if err := validateCount("attempted", c.Attempted.Times, c.Attempted.Min, c.Attempted.Max); err != nil {
				return nil, fmt.Errorf("scenario %s: assert[%d]: %w", path, i, err)
			}
		}
		if c.NeverCalled != "" {
			n++
		}
		if len(c.Sequence) > 0 {
			n++
		}
		if c.TaintNever != nil {
			n++
		}
		if c.FlowDenied != nil {
			n++
			if c.FlowDenied.From == "" || c.FlowDenied.To == "" {
				return nil, fmt.Errorf("scenario %s: assert[%d]: flow_denied needs both 'from' and 'to'", path, i)
			}
		}
		if c.FlowAttempted != nil {
			n++
			if c.FlowAttempted.From == "" || c.FlowAttempted.To == "" {
				return nil, fmt.Errorf("scenario %s: assert[%d]: flow_attempted needs both 'from' and 'to'", path, i)
			}
		}
		if c.ResultContains != nil {
			n++
		}
		if c.Allowed != "" {
			n++
		}
		if c.Denied != "" {
			n++
		}
		if c.Invariant != "" {
			n++
		}
		if c.MaxDepth != nil {
			n++
			if *c.MaxDepth < 1 {
				return nil, fmt.Errorf("scenario %s: assert[%d]: max_depth must be >= 1", path, i)
			}
		}
		if n != 1 {
			return nil, fmt.Errorf("scenario %s: assert[%d] must set exactly one check", path, i)
		}
	}
	return &sc, nil
}

// validateCount rejects contradictory count bounds: `times` is exact and
// cannot combine with the `min_times`/`max_times` bounds.
func validateCount(check string, times, min, max *int) error {
	if times != nil && (min != nil || max != nil) {
		return fmt.Errorf("%s: times is exact — use min_times/max_times for bounds, not both", check)
	}
	if min != nil && max != nil && *min > *max {
		return fmt.Errorf("%s: min_times (%d) exceeds max_times (%d)", check, *min, *max)
	}
	for _, v := range []*int{times, min, max} {
		if v != nil && *v < 0 {
			return fmt.Errorf("%s: counts must be >= 0", check)
		}
	}
	return nil
}
