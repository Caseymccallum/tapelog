package policy

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
)

// Flow is a cross-tool data-flow rule ("toxic flow" guard): data produced
// by `from` tools (sources) must not reach `to` tools (sinks). The model is
// session-scoped taint — conservative and honest about its limits: once a
// source tool has run, its output is presumed present in the agent's
// context, so ANY later sink call matches. Value-level tracking through
// the model (CaMeL-style) is explicitly out of scope (docs/THREAT_MODEL.md).
type Flow struct {
	ID     string         `yaml:"id"`
	From   StringOrStrings `yaml:"from"`
	To     StringOrStrings `yaml:"to"`
	Action string         `yaml:"action"` // deny | confirm
	Reason string         `yaml:"reason"`

	fromMatch []*regexp.Regexp
	toMatch   []*regexp.Regexp
}

// FlowChecker is implemented by evaluators that support flow rules.
type FlowChecker interface {
	// CheckFlow returns the first flow-rule decision that applies to a
	// call to `tool` under the given taint state. applied=false means no
	// flow rule restricts this call (fall through to regular policy).
	CheckFlow(state *TaintState, tool string) (dec Decision, applied bool)
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

// CheckFlow implements FlowChecker for Policy.
func (p *Policy) CheckFlow(state *TaintState, tool string) (Decision, bool) {
	sources := state.Sources()
	if len(sources) == 0 {
		return Decision{}, false // nothing tainted yet: no flow can apply
	}
	for _, f := range p.Flows {
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
		reason := f.Reason
		if reason == "" {
			reason = fmt.Sprintf("flow rule %q", f.ID)
		}
		reason += fmt.Sprintf(" [taint sources: %s]", strings.Join(matchedSources, ", "))
		return Decision{Verdict: Verdict(f.Action), RuleID: f.ID, Reason: reason}, true
	}
	return Decision{}, false
}

// normalizeFlow validates and compiles one flow rule.
func (f *Flow) normalize(idx int) error {
	if f.ID == "" {
		f.ID = fmt.Sprintf("flow-%d", idx+1)
	}
	if f.Action != string(VerdictDeny) && f.Action != string(VerdictConfirm) {
		return fmt.Errorf("flow %q: invalid action %q (want deny|confirm)", f.ID, f.Action)
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
