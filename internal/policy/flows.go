package policy

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
)

// Flow is a cross-tool data-flow rule ("toxic flow" guard): data produced
// by `from` tools (sources) must not reach `to` tools (sinks).
//
// Two modes (CaMeL-inspired, experimental):
//   - session (default): session-scoped taint — conservative and honest.
//     Once a source tool has run, its output is presumed present in the
//     agent's context, so ANY later sink call matches.
//   - value: fires only when the sink call's arguments are actually
//     contaminated by recorded values from a source tool's result
//     (internal/taint). Precise, but heuristic — transformations evade
//     value matching, so never rely on it alone for must-not-happen flows.
type Flow struct {
	ID     string          `yaml:"id"`
	From   StringOrStrings `yaml:"from"`
	To     StringOrStrings `yaml:"to"`
	Mode   string          `yaml:"mode"`   // session (default) | value
	Action string          `yaml:"action"` // deny | confirm
	Reason string          `yaml:"reason"`

	fromMatch []*regexp.Regexp
	toMatch   []*regexp.Regexp
}

// FlowChecker is implemented by evaluators that support flow rules.
type FlowChecker interface {
	// CheckFlow returns the first session-mode flow-rule decision that
	// applies to a call to `tool` under the given taint state.
	// applied=false means no flow rule restricts this call (fall through
	// to regular policy).
	CheckFlow(state *TaintState, tool string) (dec Decision, applied bool)
}

// ValueFlowChecker is implemented by evaluators with value-mode flow
// rules. contaminatedBy names the source tools whose recorded values were
// found in the call's arguments (internal/taint).
type ValueFlowChecker interface {
	CheckFlowValues(state *TaintState, tool string, contaminatedBy []string) (dec Decision, applied bool)
}

// TaintState tracks which source tools have produced results during a
// session. Safe for concurrent use.
type TaintState struct {
	mu      sync.Mutex
	sources []string
}

// Record marks a tool's output as entering the agent's context. Called
// when a tool call is permitted — at call time, not result time: call
// ordering in the proxy is serialized, so taint transitions are
// deterministic. Conservative by design: a permitted call is presumed to
// contribute data even if its result later errors.
func (s *TaintState) Record(tool string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.sources {
		if t == tool {
			return
		}
	}
	s.sources = append(s.sources, tool)
}

// Sources returns the tools whose outputs are in context.
func (s *TaintState) Sources() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.sources...)
}

// CheckFlow implements FlowChecker for Policy (session-mode rules only).
func (p *Policy) CheckFlow(state *TaintState, tool string) (Decision, bool) {
	sources := state.Sources()
	if len(sources) == 0 {
		return Decision{}, false // nothing tainted yet: no flow can apply
	}
	for _, f := range p.Flows {
		if f.Mode == "value" {
			continue
		}
		if !matchesAny(f.toMatch, tool) {
			continue
		}
		matchedSources := make([]string, 0, len(sources))
		for _, src := range sources {
			if matchesAny(f.fromMatch, src) {
				matchedSources = append(matchedSources, src)
			}
		}
		if len(matchedSources) == 0 {
			continue
		}
		return flowDecision(f, matchedSources), true
	}
	return Decision{}, false
}

// CheckFlowValues implements ValueFlowChecker for Policy (value-mode
// rules only): fires when the tool matches a sink AND the call's
// arguments are contaminated by one of the rule's sources.
func (p *Policy) CheckFlowValues(state *TaintState, tool string, contaminatedBy []string) (Decision, bool) {
	if len(contaminatedBy) == 0 {
		return Decision{}, false
	}
	for _, f := range p.Flows {
		if f.Mode != "value" {
			continue
		}
		if !matchesAny(f.toMatch, tool) {
			continue
		}
		matchedSources := make([]string, 0, len(contaminatedBy))
		for _, src := range contaminatedBy {
			if matchesAny(f.fromMatch, src) {
				matchedSources = append(matchedSources, src)
			}
		}
		if len(matchedSources) == 0 {
			continue
		}
		return flowDecision(f, matchedSources), true
	}
	return Decision{}, false
}

// HasValueFlows reports whether any flow rule uses value-level mode.
func (p *Policy) HasValueFlows() bool {
	for _, f := range p.Flows {
		if f.Mode == "value" {
			return true
		}
	}
	return false
}

// flowDecision renders the explainable decision for a matched flow rule.
func flowDecision(f Flow, matchedSources []string) Decision {
	reason := f.Reason
	if reason == "" {
		reason = fmt.Sprintf("flow rule %q", f.ID)
	}
	kind := "taint sources"
	if f.Mode == "value" {
		kind = "contaminated by"
	}
	reason += fmt.Sprintf(" [%s: %s]", kind, strings.Join(matchedSources, ", "))
	return Decision{Verdict: Verdict(f.Action), RuleID: f.ID, Reason: reason}
}

// normalizeFlow validates and compiles one flow rule.
func (f *Flow) normalize(idx int) error {
	if f.ID == "" {
		f.ID = fmt.Sprintf("flow-%d", idx+1)
	}
	if f.Action != string(VerdictDeny) && f.Action != string(VerdictConfirm) {
		return fmt.Errorf("flow %q: invalid action %q (want deny|confirm)", f.ID, f.Action)
	}
	switch f.Mode {
	case "", "session", "value":
		if f.Mode == "" {
			f.Mode = "session"
		}
	default:
		return fmt.Errorf("flow %q: invalid mode %q (want session|value)", f.ID, f.Mode)
	}
	if len(f.From) == 0 || len(f.To) == 0 {
		return fmt.Errorf("flow %q: both 'from' and 'to' patterns are required", f.ID)
	}
	f.fromMatch = compileGlobs(f.From)
	f.toMatch = compileGlobs(f.To)
	return nil
}

// compileGlobs compiles a pattern list to anchored regexps.
func compileGlobs(pats StringOrStrings) []*regexp.Regexp {
	out := make([]*regexp.Regexp, 0, len(pats))
	for _, pat := range pats {
		if re, err := globToRegex(pat); err == nil {
			out = append(out, re)
		}
	}
	return out
}

// matchesAny reports whether any matcher matches s.
func matchesAny(matchers []*regexp.Regexp, s string) bool {
	for _, re := range matchers {
		if re.MatchString(s) {
			return true
		}
	}
	return false
}
