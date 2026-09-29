// Package policy implements cassette's tool-call policy: a small,
// human-readable YAML scheme (v0) behind a stable Evaluator interface.
// Decisions are explainable — every verdict carries a rule id and reason,
// which are recorded in the session log (docs/THREAT_MODEL.md claim #1).
//
// Engine note (ADR 0002): v0 evaluates YAML rules natively; Cedar becomes
// the internal engine later without changing this interface.
package policy

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Verdict is the outcome of a policy evaluation.
type Verdict string

const (
	VerdictAllow   Verdict = "allow"
	VerdictConfirm Verdict = "confirm"
	VerdictDeny    Verdict = "deny"
)

// Request describes a tool call awaiting a decision.
type Request struct {
	SessionID string
	Tool      string
	Args      json.RawMessage
}

// Decision is an explainable verdict for one Request.
type Decision struct {
	Verdict   Verdict `json:"verdict" yaml:"verdict"`
	RuleID    string  `json:"rule_id" yaml:"rule_id"`
	Reason    string  `json:"reason" yaml:"reason"`
	IsDefault bool    `json:"default,omitempty" yaml:"default,omitempty"`
}

// Evaluator decides whether a tool call may proceed. Implementations must
// be safe for concurrent use.
type Evaluator interface {
	Evaluate(Request) Decision
}

// StringOrStrings accepts a YAML scalar or sequence for rule tool patterns.
type StringOrStrings []string

// UnmarshalYAML implements yaml.Unmarshaler.
func (s *StringOrStrings) UnmarshalYAML(value *yaml.Node) error {
	var one string
	if err := value.Decode(&one); err == nil {
		*s = StringOrStrings{one}
		return nil
	}
	var many []string
	if err := value.Decode(&many); err != nil {
		return fmt.Errorf("tool must be a string or a list of strings")
	}
	*s = StringOrStrings(many)
	return nil
}

// Rule is one ordered policy rule. The first rule whose tool pattern(s)
// match wins.
type Rule struct {
	ID     string         `yaml:"id"`
	Tool   StringOrStrings `yaml:"tool"`
	Action string         `yaml:"action"`
	Reason string         `yaml:"reason"`

	matchers []*regexp.Regexp // compiled from Tool at load time
}

// Policy is a parsed YAML policy file.
type Policy struct {
	Version int    `yaml:"version"`
	Default string `yaml:"default"`
	Rules   []Rule `yaml:"rules"`
}

// Load reads, parses, validates and compiles a YAML policy file.
func Load(path string) (*Policy, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read policy: %w", err)
	}
	var p Policy
	if err := yaml.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parse policy YAML: %w", err)
	}
	if err := p.normalize(); err != nil {
		return nil, err
	}
	return &p, nil
}

// normalize validates and compiles the policy.
func (p *Policy) normalize() error {
	if p.Version != 1 {
		return fmt.Errorf("unsupported policy version %d (want 1)", p.Version)
	}
	if p.Default == "" {
		p.Default = string(VerdictDeny) // fail-closed; set `default: allow` for observe-only
	}
	if !validAction(p.Default) {
		return fmt.Errorf("invalid default action %q (want allow|deny|confirm)", p.Default)
	}
	for i := range p.Rules {
		r := &p.Rules[i]
		if r.ID == "" {
			r.ID = fmt.Sprintf("rule-%d", i+1)
		}
		if !validAction(r.Action) {
			return fmt.Errorf("rule %q: invalid action %q (want allow|deny|confirm)", r.ID, r.Action)
		}
		if len(r.Tool) == 0 {
			return fmt.Errorf("rule %q: at least one tool pattern is required", r.ID)
		}
		r.matchers = nil
		for _, pat := range r.Tool {
			re, err := globToRegex(pat)
			if err != nil {
				return fmt.Errorf("rule %q: bad pattern %q: %w", r.ID, pat, err)
			}
			r.matchers = append(r.matchers, re)
		}
	}
	return nil
}

// Evaluate returns the decision of the first matching rule, or the
// policy default when no rule matches.
func (p *Policy) Evaluate(req Request) Decision {
	for _, r := range p.Rules {
		for _, re := range r.matchers {
			if re.MatchString(req.Tool) {
				reason := r.Reason
				if reason == "" {
					reason = fmt.Sprintf("matched rule %q", r.ID)
				}
				return Decision{Verdict: Verdict(r.Action), RuleID: r.ID, Reason: reason}
			}
		}
	}
	return Decision{
		Verdict:   Verdict(p.Default),
		RuleID:    "default",
		Reason:    fmt.Sprintf("no rule matched; policy default is %q", p.Default),
		IsDefault: true,
	}
}

// AllowAll is an observe-only evaluator used when no policy file is given:
// everything is permitted (and recorded) but nothing is enforced.
type AllowAll struct{}

// Evaluate implements Evaluator.
func (AllowAll) Evaluate(Request) Decision {
	return Decision{
		Verdict: VerdictAllow,
		RuleID:  "observe",
		Reason:  "no policy configured; observe-only recording",
	}
}

// validAction reports whether s is a legal rule action.
func validAction(s string) bool {
	switch Verdict(s) {
	case VerdictAllow, VerdictConfirm, VerdictDeny:
		return true
	}
	return false
}

// globToRegex compiles a tool-name glob (* and ? wildcards) to an anchored
// regular expression.
func globToRegex(pattern string) (*regexp.Regexp, error) {
	var sb strings.Builder
	sb.WriteString("^")
	for _, ch := range pattern {
		switch ch {
		case '*':
			sb.WriteString(".*")
		case '?':
			sb.WriteString(".")
		default:
			sb.WriteString(regexp.QuoteMeta(string(ch)))
		}
	}
	sb.WriteString("$")
	return regexp.Compile(sb.String())
}
