// Package policy implements tapelog's tool-call policy: a small,
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
	"time"

	"gopkg.in/yaml.v3"

	"github.com/Caseymccallum/tapelog/internal/limits"
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
	Task      string // optional task label for per-task scoping
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
// match — and whose scope (tasks/expires) and `where` conditions pass —
// wins.
type Rule struct {
	ID      string         `yaml:"id"`
	Tool    StringOrStrings `yaml:"tool"`
	Action  string         `yaml:"action"`
	Reason  string         `yaml:"reason"`
	Tasks   StringOrStrings `yaml:"tasks"`   // optional: rule applies only to these task labels
	Expires string         `yaml:"expires"` // optional: RFC 3339; expired rules are skipped
	Where   StringOrStrings `yaml:"where"`   // optional: Cedar conditions over context.tool / context.args

	matchers  []*regexp.Regexp
	expiresAt *time.Time
	cond      *conditionSet // Cedar-backed (nil when no `where`)
}

// inScope reports whether the rule applies to this request's task and is
// unexpired at time now.
func (r *Rule) inScope(task string, now time.Time) bool {
	if len(r.Tasks) > 0 {
		found := false
		for _, t := range r.Tasks {
			if t == task {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if r.expiresAt != nil && now.After(*r.expiresAt) {
		return false
	}
	return true
}

// matchesTool reports whether any tool glob matches the tool name.
func (r *Rule) matchesTool(tool string) bool {
	for _, re := range r.matchers {
		if re.MatchString(tool) {
			return true
		}
	}
	return false
}

// Policy is a parsed YAML policy file.
type Policy struct {
	Version int          `yaml:"version"`
	Default string       `yaml:"default"`
	Rules   []Rule       `yaml:"rules"`
	Flows   []Flow       `yaml:"flows"`   // optional cross-tool data-flow rules
	Limits  limits.Limits `yaml:"limits"` // optional session budgets / payload caps

	now func() time.Time // test seam; defaults to time.Now
}

// time returns the policy clock.
func (p *Policy) time() time.Time {
	if p.now != nil {
		return p.now()
	}
	return time.Now()
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
		if r.Expires != "" {
			t, err := time.Parse(time.RFC3339, r.Expires)
			if err != nil {
				return fmt.Errorf("rule %q: invalid expires %q (want RFC 3339): %w", r.ID, r.Expires, err)
			}
			r.expiresAt = &t
		}
		cond, err := compileWhere(r.ID, r.Where)
		if err != nil {
			return err
		}
		r.cond = cond
	}
	for i := range p.Flows {
		if err := p.Flows[i].normalize(i); err != nil {
			return err
		}
	}
	if p.Limits.Enabled() {
		if _, err := limits.NewTracker(p.Limits); err != nil {
			return err
		}
	}
	return nil
}

// Evaluate returns the decision of the first rule that is in scope
// (tasks/expires), whose tool patterns match and whose `where` conditions
// pass; otherwise the policy default.
func (p *Policy) Evaluate(req Request) Decision {
	now := p.time()
	for _, r := range p.Rules {
		if !r.inScope(req.Task, now) {
			continue
		}
		if !r.matchesTool(req.Tool) {
			continue
		}
		if !r.cond.passes(req) {
			continue
		}
		reason := r.Reason
		if reason == "" {
			reason = fmt.Sprintf("matched rule %q", r.ID)
		}
		return Decision{Verdict: Verdict(r.Action), RuleID: r.ID, Reason: reason}
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
