package policy

// Candidate generation for the Policy Studio. Two rules govern this file:
//
//  1. Only engine-supported features are expressible, and every generated
//     document is re-parsed through Parse() (the same normalize/compile path
//     the boundary uses) before it is offered to an operator.
//  2. An existing policy is never round-tripped through a struct. Comments,
//     unknown fields and hand-written conditions survive byte-for-byte
//     because a new rule is *inserted as text*; nothing else is rewritten.

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// ConditionDraft is one supported argument condition.
type ConditionDraft struct {
	Arg   string `json:"arg"`
	Op    string `json:"op"`   // equals | starts_with | ends_with | contains
	Value string `json:"value"`
	Kind  string `json:"kind"` // string | number | bool
}

// RuleDraft is a structured rule request from the Studio.
type RuleDraft struct {
	ID         string           `json:"id"`
	Tools      []string         `json:"tools"`
	Action     string           `json:"action"`
	Reason     string           `json:"reason"`
	Tasks      []string         `json:"tasks,omitempty"`
	Expires    string           `json:"expires,omitempty"`
	Conditions []ConditionDraft `json:"conditions,omitempty"`
}

var (
	argNameRe   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,63}$`)
	ruleIDRe    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,63}$`)
	integerRe   = regexp.MustCompile(`^-?[0-9]{1,18}$`)
	controlRune = regexp.MustCompile(`[\x00-\x1f\x7f]`)
)

// ValidateRuleDraft checks a draft against the features the engine supports
// and fills in a safe default id/reason. It never widens what was asked for.
func ValidateRuleDraft(d *RuleDraft) error {
	if d == nil {
		return errors.New("no rule was supplied")
	}
	if !validAction(d.Action) {
		return fmt.Errorf("choose allow, deny or require approval (got %q)", d.Action)
	}
	if len(d.Tools) == 0 {
		return errors.New("a rule needs at least one tool name or pattern")
	}
	if len(d.Tools) > 32 {
		return errors.New("a rule supports at most 32 tool patterns")
	}
	seen := map[string]bool{}
	tools := make([]string, 0, len(d.Tools))
	for _, t := range d.Tools {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if len(t) > 128 {
			return errors.New("tool names must be 128 characters or fewer")
		}
		if controlRune.MatchString(t) {
			return fmt.Errorf("tool name %q contains control characters", short(t))
		}
		if seen[t] {
			continue
		}
		seen[t] = true
		tools = append(tools, t)
	}
	if len(tools) == 0 {
		return errors.New("a rule needs at least one tool name or pattern")
	}
	d.Tools = tools

	if len(d.Tasks) > 16 {
		return errors.New("a rule supports at most 16 task labels")
	}
	for _, task := range d.Tasks {
		if strings.TrimSpace(task) == "" || len(task) > 128 || controlRune.MatchString(task) {
			return fmt.Errorf("task label %q is not usable", short(task))
		}
	}
	if d.Expires != "" {
		t, err := time.Parse(time.RFC3339, d.Expires)
		if err != nil {
			return errors.New("expiry must be an RFC 3339 timestamp, for example 2026-12-31T23:59:59Z")
		}
		d.Expires = t.UTC().Format(time.RFC3339)
	}
	if len(d.Conditions) > 8 {
		return errors.New("a rule supports at most 8 argument conditions")
	}
	for i := range d.Conditions {
		if err := validateCondition(&d.Conditions[i]); err != nil {
			return fmt.Errorf("condition %d: %w", i+1, err)
		}
	}
	if d.Reason == "" {
		d.Reason = defaultReason(d)
	}
	if len(d.Reason) > 300 {
		return errors.New("the explanation must be 300 characters or fewer")
	}
	if controlRune.MatchString(d.Reason) {
		return errors.New("the explanation may not contain control characters")
	}
	if d.ID == "" {
		d.ID = suggestedID(d)
	}
	if !ruleIDRe.MatchString(d.ID) {
		return errors.New("rule id must start with a letter or digit and use only letters, digits, dot, dash, colon or underscore")
	}
	return nil
}

// MatchableArgumentKey reports whether the visual editor can express a
// condition on this top-level argument name. Cedar record fields must be
// identifiers, so unusual names stay a source-editor concern.
func MatchableArgumentKey(key string) bool { return argNameRe.MatchString(key) }

func validateCondition(c *ConditionDraft) error {
	if !argNameRe.MatchString(c.Arg) {
		return fmt.Errorf("argument %q cannot be matched in the visual editor; use the policy source for nested or unusual argument names", short(c.Arg))
	}
	if len(c.Value) > 512 {
		return errors.New("matched values must be 512 characters or fewer")
	}
	if controlRune.MatchString(c.Value) {
		return errors.New("matched values may not contain control characters")
	}
	switch c.Kind {
	case "", "string":
		c.Kind = "string"
		switch c.Op {
		case "equals", "starts_with", "ends_with", "contains":
			return nil
		}
		return fmt.Errorf("choose equals, starts with, ends with or contains (got %q)", c.Op)
	case "number":
		if !integerRe.MatchString(strings.TrimSpace(c.Value)) {
			return errors.New("numeric matching supports whole numbers only")
		}
		c.Value = strings.TrimSpace(c.Value)
		if c.Op != "equals" {
			return errors.New("numeric matching supports equals only")
		}
		return nil
	case "bool":
		if c.Value != "true" && c.Value != "false" {
			return errors.New("boolean matching supports true or false only")
		}
		if c.Op != "equals" {
			return errors.New("boolean matching supports equals only")
		}
		return nil
	}
	return fmt.Errorf("unsupported matched-value kind %q", c.Kind)
}

func defaultReason(d *RuleDraft) string {
	switch Verdict(d.Action) {
	case VerdictDeny:
		return "blocked by a policy proposal from the session workbench"
	case VerdictConfirm:
		return "a policy proposal requires a human decision for this tool"
	}
	return "allowed by a policy proposal from the session workbench"
}

// suggestedID derives a readable id. The Studio offers it for editing rather
// than silently committing to it.
func suggestedID(d *RuleDraft) string {
	base := strings.TrimSpace(d.Tools[0])
	if i := strings.IndexAny(base, "*?"); i > 0 {
		base = base[:i]
	}
	var b strings.Builder
	for _, ch := range base {
		switch {
		case ch >= 'a' && ch <= 'z', ch >= '0' && ch <= '9':
			b.WriteRune(ch)
		case ch >= 'A' && ch <= 'Z':
			b.WriteRune(ch + ('a' - 'A'))
		default:
			if b.Len() > 0 && !strings.HasSuffix(b.String(), "-") {
				b.WriteByte('-')
			}
		}
	}
	stem := strings.Trim(b.String(), "-")
	if stem == "" {
		stem = "rule"
	}
	if len(stem) > 40 {
		stem = strings.Trim(stem[:40], "-")
	}
	switch Verdict(d.Action) {
	case VerdictDeny:
		return "deny-" + stem
	case VerdictConfirm:
		return "confirm-" + stem
	}
	return "allow-" + stem
}

func short(s string) string {
	if len(s) <= 40 {
		return s
	}
	return s[:40] + "…"
}

// cedarString renders a Cedar string literal. Values from recordings and from
// operators are data, never syntax.
func cedarString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, ch := range s {
		switch ch {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteRune(ch)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// cedarLikeLiteral escapes a literal for a Cedar `like` pattern, where `*` is
// the only wildcard and `\` escapes it. A value that itself contains `*` must
// not become a wildcard.
func cedarLikeLiteral(s string) string {
	var b strings.Builder
	for _, ch := range s {
		switch ch {
		case '*':
			b.WriteString(`\*`)
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteRune(ch)
		}
	}
	return b.String()
}

// ConditionExpr renders one supported condition as a real Cedar expression.
func ConditionExpr(c ConditionDraft) (string, error) {
	field := "context.args." + c.Arg
	if c.Kind == "number" || c.Kind == "bool" {
		return field + " == " + c.Value, nil
	}
	switch c.Op {
	case "equals":
		return field + " == " + cedarString(c.Value), nil
	case "starts_with":
		return field + ` like "` + cedarLikeLiteral(c.Value) + `*"`, nil
	case "ends_with":
		return field + ` like "*` + cedarLikeLiteral(c.Value) + `"`, nil
	case "contains":
		return field + ` like "*` + cedarLikeLiteral(c.Value) + `*"`, nil
	}
	return "", fmt.Errorf("unsupported condition operator %q", c.Op)
}
