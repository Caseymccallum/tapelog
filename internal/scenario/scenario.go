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
	Times *int           `yaml:"times"` // exact count; nil = at least one
}

// AttemptSpec matches calls that reached the boundary (answered OR
// denied) — "the agent tried this", regardless of what the boundary did.
type AttemptSpec struct {
	Tool  string         `yaml:"tool"`
	Args  map[string]any `yaml:"args"`
	Times *int           `yaml:"times"`
}

// TaintSpec forbids a flow: no `To` call after a `From` call.
type TaintSpec struct {
	From string `yaml:"from"`
	To   string `yaml:"to"`
}

// ResultSpec asserts text appears in a tool's recorded result.
type ResultSpec struct {
	Tool string `yaml:"tool"`
	Text string `yaml:"text"`
}

// Check is one assertion. Exactly one field is set (enforced at parse):
//
//	called:        {tool, args?, times?}  (answered calls)
//	attempted:     {tool, args?, times?}  (answered + denied — boundary saw it)
//	never_called:  glob                  (answered calls)
//	sequence:      [tool, tool, ...]   (ordered subsequence of the trajectory)
//	taint_never:   {from, to}
//	result_contains: {tool, text}
//	allowed / denied: glob             (verdict assertions, answered + denied)
//	invariant:     name                (boundary invariant)
type Check struct {
	Called         *CalledSpec  `yaml:"called,omitempty"`
	Attempted      *AttemptSpec `yaml:"attempted,omitempty"`
	NeverCalled    string       `yaml:"never_called,omitempty"`
	Sequence       []string     `yaml:"sequence,omitempty"`
	TaintNever     *TaintSpec   `yaml:"taint_never,omitempty"`
	ResultContains *ResultSpec  `yaml:"result_contains,omitempty"`
	Allowed        string       `yaml:"allowed,omitempty"`
	Denied         string       `yaml:"denied,omitempty"`
	Invariant      string       `yaml:"invariant,omitempty"`
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
		}
		if c.Attempted != nil {
			n++
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
		if n != 1 {
			return nil, fmt.Errorf("scenario %s: assert[%d] must set exactly one check", path, i)
		}
	}
	return &sc, nil
}
