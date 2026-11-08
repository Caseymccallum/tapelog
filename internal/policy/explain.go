package policy

// This file renders a parsed policy as a read-only, explainable description
// for the Policy Studio. It changes no evaluation behaviour: every sentence
// is derived from the same fields Evaluate() reads. Where the engine's
// semantics cannot be stated simply, the raw Cedar expression is shown
// instead of an invented simplification, and anything the engine ignores is
// reported as unsupported rather than dropped.

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/Caseymccallum/tapelog/internal/limits"
)

// ActionLabel is the Studio's vocabulary for a verdict.
func ActionLabel(action string) string {
	switch Verdict(action) {
	case VerdictAllow:
		return "Allow"
	case VerdictConfirm:
		return "Require approval"
	case VerdictDeny:
		return "Block"
	}
	return action
}

// ActionEffect explains what the boundary actually does for a verdict. Each
// sentence states behaviour implemented by the mediator, not a policy wish.
func ActionEffect(action string) string {
	switch Verdict(action) {
	case VerdictAllow:
		return "The call proceeds without asking anyone. The boundary still applies limits, result scanning, redaction and schema checks."
	case VerdictConfirm:
		return "The call is parked until a human chooses allow once, allow for the session, or deny. With no terminal and no --auto-confirm the call is denied."
	case VerdictDeny:
		return "The call is refused and the request is not forwarded to the server. Denial alone does not prove a tool never ran, because a denied call may still have a recorded result."
	}
	return ""
}

// ConditionView describes one `where:` expression.
type ConditionView struct {
	Expr       string `json:"expr"`
	Summary    string `json:"summary,omitempty"`
	Simplified bool   `json:"simplified"`
	Note       string `json:"note,omitempty"`
	Problem    string `json:"problem,omitempty"`
}

// RuleView is one rule described in plain language.
type RuleView struct {
	Index        int             `json:"index"`
	Position     int             `json:"position"`
	ID           string          `json:"id"`
	Action       string          `json:"action"`
	ActionLabel  string          `json:"action_label"`
	ActionEffect string          `json:"action_effect"`
	Tools        []string        `json:"tools"`
	ToolSummary  string          `json:"tool_summary"`
	Summary      string          `json:"summary"`
	Reason       string          `json:"reason,omitempty"`
	Tasks        []string        `json:"tasks,omitempty"`
	Expires      string          `json:"expires,omitempty"`
	ScopeSummary string          `json:"scope_summary"`
	Conditions   []ConditionView `json:"conditions,omitempty"`
	WhereRaw     []string        `json:"where_raw,omitempty"`
	Broad        bool            `json:"broad"`
	BroadWhy     []string        `json:"broad_why,omitempty"`
	Unreachable  string          `json:"unreachable,omitempty"`
}

// FlowView is one cross-tool data-flow rule described in plain language.
type FlowView struct {
	Index        int      `json:"index"`
	Position     int      `json:"position"`
	ID           string   `json:"id"`
	From         []string `json:"from"`
	To           []string `json:"to"`
	Mode         string   `json:"mode"`
	Action       string   `json:"action"`
	ActionLabel  string   `json:"action_label"`
	ActionEffect string   `json:"action_effect"`
	Reason       string   `json:"reason,omitempty"`
	Summary      string   `json:"summary"`
}

// LimitsView describes session limits. They are enforced by the boundary
// (internal/limits) and are not part of rule evaluation.
type LimitsView struct {
	Enabled          bool           `json:"enabled"`
	MaxCalls         int            `json:"max_calls,omitempty"`
	MaxPerMinute     int            `json:"max_per_minute,omitempty"`
	MaxResponseBytes int            `json:"max_response_bytes,omitempty"`
	PerTool          map[string]int `json:"per_tool,omitempty"`
	Summary          []string       `json:"summary"`
	Note             string         `json:"note"`
}

// InjectionView describes result-side injection scanning.
type InjectionView struct {
	Mode          string   `json:"mode"`
	Patterns      int      `json:"patterns"`
	ExtraPatterns []string `json:"extra_patterns,omitempty"`
	Summary       string   `json:"summary"`
}

// Analysis is a read-only description of one policy document.
type Analysis struct {
	Version          int            `json:"version"`
	Default          string         `json:"default"`
	DefaultLabel     string         `json:"default_label"`
	DefaultEffect    string         `json:"default_effect"`
	Rules            []RuleView     `json:"rules"`
	Flows            []FlowView     `json:"flows"`
	Limits           LimitsView     `json:"limits"`
	Injection        InjectionView  `json:"injection"`
	Unsupported      []string      `json:"unsupported"`
	Warnings         []string       `json:"warnings"`
	BroadPermissions int            `json:"broad_permissions"`
	ShadowedRules    int            `json:"shadowed_rules"`
	DeadConditions   int            `json:"dead_conditions"`
	RuleIDs          []string       `json:"rule_ids"`
	FlowIDs          []string       `json:"flow_ids"`
}


// Analyze describes a parsed policy. source is the original document text,
// used only to report fields the engine ignores.
func Analyze(p *Policy, source []byte) *Analysis {
	if p == nil {
		return &Analysis{Rules: []RuleView{}, Flows: []FlowView{}}
	}
	a := &Analysis{
		Version:       p.Version,
		Default:       p.Default,
		DefaultLabel:  ActionLabel(p.Default),
		DefaultEffect: ActionEffect(p.Default),
		Rules:         make([]RuleView, 0, len(p.Rules)),
		Flows:         make([]FlowView, 0, len(p.Flows)),
		RuleIDs:       make([]string, 0, len(p.Rules)),
		FlowIDs:       make([]string, 0, len(p.Flows)),
	}

	type unconditional struct {
		id    string
		pos   int
		tools []string
		any   bool
	}
	var previous []unconditional

	for i := range p.Rules {
		r := &p.Rules[i]
		view := RuleView{
			Index:        i,
			Position:     i + 1,
			ID:           r.ID,
			Action:       r.Action,
			ActionLabel:  ActionLabel(r.Action),
			ActionEffect: ActionEffect(r.Action),
			Tools:        append([]string(nil), r.Tool...),
			Reason:       r.Reason,
			Tasks:        append([]string(nil), r.Tasks...),
			Expires:      r.Expires,
			WhereRaw:     append([]string(nil), r.Where...),
		}
		view.ToolSummary = describeTools(view.Tools)
		view.ScopeSummary = describeScope(r)
		for _, expr := range r.Where {
			view.Conditions = append(view.Conditions, describeCondition(expr))
		}
		for _, c := range view.Conditions {
			if c.Problem != "" {
				a.DeadConditions++
			}
		}
		view.Broad, view.BroadWhy = broadTools(view.Tools)
		if view.Broad && view.Action == string(VerdictAllow) && len(r.Where) == 0 {
			view.BroadWhy = append(view.BroadWhy, "this rule allows those calls with no argument conditions")
		}
		if view.Broad {
			a.BroadPermissions++
		}
		// A rule is unreachable only when an earlier unconditional rule
		// provably covers every call this rule could match.
		for _, prev := range previous {
			if prev.any || sameStrings(prev.tools, view.Tools) {
				view.Unreachable = fmt.Sprintf("Rule %q (position %d) is evaluated first and already matches every call this rule can match, so this rule can never be reached.", prev.id, prev.pos)
				a.ShadowedRules++
				break
			}
		}
		if len(r.Tasks) == 0 && r.Expires == "" && len(r.Where) == 0 {
			anyTool := false
			for _, pat := range r.Tool {
				if matchesEverything(pat) {
					anyTool = true
					break
				}
			}
			previous = append(previous, unconditional{id: r.ID, pos: view.Position, tools: view.Tools, any: anyTool})
		}
		view.Summary = ruleSummary(&view)
		a.Rules = append(a.Rules, view)
		a.RuleIDs = append(a.RuleIDs, r.ID)
	}

	for i := range p.Flows {
		f := &p.Flows[i]
		v := FlowView{
			Index:        i,
			Position:     i + 1,
			ID:           f.ID,
			From:         append([]string(nil), f.From...),
			To:           append([]string(nil), f.To...),
			Mode:         f.Mode,
			Action:       f.Action,
			ActionLabel:  ActionLabel(f.Action),
			ActionEffect: ActionEffect(f.Action),
			Reason:       f.Reason,
		}
		v.Summary = flowSummary(&v)
		a.Flows = append(a.Flows, v)
		a.FlowIDs = append(a.FlowIDs, f.ID)
	}

	a.Limits = describeLimits(p.Limits)
	a.Injection = describeInjection(p.Injection)
	a.Unsupported = unsupportedFields(source)

	if a.Default == string(VerdictAllow) {
		a.Warnings = append(a.Warnings, "The policy default is allow: any call that matches no rule proceeds. Only a rule that matches stops it.")
	}
	if len(a.Rules) == 0 && len(a.Flows) == 0 {
		a.Warnings = append(a.Warnings, fmt.Sprintf("This policy has no rules and no flows; every call gets the default (%s).", a.DefaultLabel))
	}
	if a.DeadConditions > 0 {
		a.Warnings = append(a.Warnings, fmt.Sprintf("%d condition(s) contain a pattern that cannot match the arguments they appear to target. A rule with only dead conditions silently stops applying.", a.DeadConditions))
	}
	if a.ShadowedRules > 0 {
		a.Warnings = append(a.Warnings, fmt.Sprintf("%d rule(s) can never be reached because an earlier rule matches first.", a.ShadowedRules))
	}
	for _, f := range a.Flows {
		if f.Mode == "value" {
			a.Warnings = append(a.Warnings, "Value-level taint is experimental: transformations in the model evade value matching, so it is not a complete guard on its own.")
			break
		}
	}
	if a.Injection.Mode == "off" {
		a.Warnings = append(a.Warnings, "Result injection scanning is off: tool results are not scanned for prompt-injection patterns.")
	}
	return a
}


// describeTools renders a tool pattern list as a sentence fragment. Patterns
// are grouped so a long allow-list stays readable.
func describeTools(tools []string) string {
	if len(tools) == 0 {
		return "no tools (this rule cannot match)"
	}
	for _, pat := range tools {
		if matchesEverything(pat) {
			return "calls to every tool"
		}
	}
	var globs, literals []string
	for _, pat := range tools {
		if strings.ContainsAny(pat, "*?") {
			globs = append(globs, pat)
			continue
		}
		literals = append(literals, pat)
	}
	var parts []string
	switch len(globs) {
	case 0:
	case 1:
		parts = append(parts, "tools matching "+fmt.Sprintf("%q", globs[0]))
	default:
		parts = append(parts, "tools matching "+strings.Join(quoteAll(globs), ", "))
	}
	switch len(literals) {
	case 0:
	case 1:
		parts = append(parts, "the tool "+fmt.Sprintf("%q", literals[0]))
	default:
		parts = append(parts, "the tools "+strings.Join(quoteAll(literals), ", "))
	}
	if len(parts) == 0 {
		return "no tools (this rule cannot match)"
	}
	return "calls to " + strings.Join(parts, " and ")
}

// matchesEverything reports whether a glob pattern is only wildcards.
func matchesEverything(pat string) bool {
	if pat == "" {
		return false
	}
	for _, ch := range pat {
		if ch != '*' && ch != '?' {
			return false
		}
	}
	return true
}

// broadTools flags patterns that reach far beyond one named tool.
func broadTools(tools []string) (bool, []string) {
	var why []string
	for _, pat := range tools {
		if matchesEverything(pat) {
			why = append(why, fmt.Sprintf("the pattern %q matches every tool name", pat))
			continue
		}
		if i := strings.IndexAny(pat, "*?"); i >= 0 && i < 4 {
			why = append(why, fmt.Sprintf("the pattern %q matches every tool whose name starts with %q", pat, pat[:i]))
		}
	}
	return len(why) > 0, why
}

func describeScope(r *Rule) string {
	var parts []string
	if len(r.Tasks) == 0 {
		parts = append(parts, "any task label")
	} else {
		parts = append(parts, "only task labels "+strings.Join(quoteAll(r.Tasks), ", "))
	}
	if r.Expires == "" {
		parts = append(parts, "no expiry")
	} else {
		parts = append(parts, "expires at "+r.Expires)
	}
	if len(r.Where) > 0 {
		parts = append(parts, fmt.Sprintf("%d argument condition(s) that must all be true", len(r.Where)))
	} else {
		parts = append(parts, "no argument conditions")
	}
	return strings.Join(parts, "; ")
}

func quoteAll(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		out = append(out, fmt.Sprintf("%q", s))
	}
	return out
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	x := append([]string(nil), a...)
	y := append([]string(nil), b...)
	sort.Strings(x)
	sort.Strings(y)
	for i := range x {
		if x[i] != y[i] {
			return false
		}
	}
	return true
}

// argComparison matches exactly one supported comparison. Anything more
// complex is shown as raw Cedar instead of being guessed at. The operand must
// also be a *single* literal (checked below), so a compound expression that
// happens to end with a quote is never flattened into a simpler claim.
var argComparison = regexp.MustCompile(`^context\.args\.([A-Za-z_][A-Za-z0-9_]*) (like|==) (.+)$`)

var (
	likeLiteral          = regexp.MustCompile(`like\s+"((?:[^"\\]|\\.)*)"`)
	cedarStringLiteral   = regexp.MustCompile(`^"(?:[^"\\]|\\.)*"$`)
	cedarNumberLiteral   = regexp.MustCompile(`^-?[0-9]+$`)
	absentArgumentNote   = "A call whose arguments do not contain this key does not match: the condition errors and the engine skips the rule."
)

// describeCondition renders one `where:` expression. Simplified is true only
// when the whole expression is a single comparison the Studio understands.
func describeCondition(expr string) ConditionView {
	v := ConditionView{Expr: expr}
	if problem := deadPattern(expr); problem != "" {
		v.Problem = problem
	}
	m := argComparison.FindStringSubmatch(strings.TrimSpace(expr))
	if m == nil {
		return v
	}
	key, op, raw := m[1], m[2], strings.TrimSpace(m[3])
	if op == "like" {
		if !cedarStringLiteral.MatchString(raw) {
			return v
		}
		lit, shape := cedarLikeText(raw[1 : len(raw)-1])
		switch shape {
		case "any":
			v.Summary = fmt.Sprintf("argument %q may be any value", key)
		case "starts":
			v.Summary = fmt.Sprintf("argument %q starts with %q", key, lit)
		case "ends":
			v.Summary = fmt.Sprintf("argument %q ends with %q", key, lit)
		case "contains":
			v.Summary = fmt.Sprintf("argument %q contains %q", key, lit)
		case "equals":
			v.Summary = fmt.Sprintf("argument %q equals %q", key, lit)
		default:
			return v
		}
		v.Simplified = true
		v.Note = absentArgumentNote
		return v
	}
	switch {
	case cedarStringLiteral.MatchString(raw):
		v.Summary = fmt.Sprintf("argument %q equals %q", key, raw[1:len(raw)-1])
	case raw == "true" || raw == "false":
		v.Summary = fmt.Sprintf("argument %q equals %s", key, raw)
	case cedarNumberLiteral.MatchString(raw):
		v.Summary = fmt.Sprintf("argument %q equals %s", key, raw)
	default:
		return v
	}
	v.Simplified = true
	v.Note = absentArgumentNote
	return v
}

// deadPattern reports a `like` pattern that cannot match what it appears to
// target. Cedar's only wildcard is `*`; `%` and `?` are literals.
func deadPattern(expr string) string {
	for _, m := range likeLiteral.FindAllStringSubmatch(expr, -1) {
		pattern := m[1]
		if strings.Contains(pattern, "%") {
			return "This pattern contains % , which Cedar's like operator treats as a literal character rather than a wildcard, so the condition only matches an argument that really contains a percent sign. Cedar's wildcard is * ."
		}
	}
	return ""
}

// cedarLikeText classifies a `like` pattern into the one shape it has, or
// reports that it cannot be stated simply.
func cedarLikeText(pattern string) (string, string) {
	var b strings.Builder
	lead, trail := false, false
	i, end := 0, len(pattern)
	if strings.HasPrefix(pattern, "*") {
		lead = true
		i = 1
	}
	if end > i && strings.HasSuffix(pattern, "*") && !strings.HasSuffix(pattern, "\\*") {
		trail = true
		end--
	}
	for ; i < end; i++ {
		switch pattern[i] {
		case '\\':
			if i+1 < end && (pattern[i+1] == '*' || pattern[i+1] == '\\') {
				b.WriteByte(pattern[i+1])
				i++
				continue
			}
			return "", "unsupported"
		case '*':
			return "", "unsupported"
		default:
			b.WriteByte(pattern[i])
		}
	}
	lit := b.String()
	switch {
	case lit == "" && (lead || trail):
		return "", "any"
	case lead && trail:
		return lit, "contains"
	case lead:
		return lit, "ends"
	case trail:
		return lit, "starts"
	}
	return lit, "equals"
}

func describeLimits(l limits.Limits) LimitsView {
	v := LimitsView{Enabled: l.Enabled(), MaxCalls: l.MaxCalls, MaxPerMinute: l.MaxPerMinute, MaxResponseBytes: l.MaxResponseBytes, PerTool: l.MaxCallsPerTool, Summary: []string{}}
	if l.MaxCalls > 0 {
		v.Summary = append(v.Summary, fmt.Sprintf("at most %d tool-call attempts per session", l.MaxCalls))
	}
	if l.MaxPerMinute > 0 {
		v.Summary = append(v.Summary, fmt.Sprintf("at most %d attempts in any sliding minute", l.MaxPerMinute))
	}
	if len(l.MaxCallsPerTool) > 0 {
		pat := make([]string, 0, len(l.MaxCallsPerTool))
		for p := range l.MaxCallsPerTool {
			pat = append(pat, p)
		}
		sort.Strings(pat)
		for _, p := range pat {
			v.Summary = append(v.Summary, fmt.Sprintf("at most %d attempts for tools matching %q", l.MaxCallsPerTool[p], p))
		}
	}
	if l.MaxResponseBytes > 0 {
		v.Summary = append(v.Summary, fmt.Sprintf("tool results larger than %d bytes are replaced with an error", l.MaxResponseBytes))
	}
	v.Note = "Limits are enforced by the boundary around each call, not by rule evaluation, so they do not appear as a rule decision. Attempts denied by policy still count."
	return v
}

func describeInjection(c InjectionConfig) InjectionView {
	mode := c.Mode
	if mode == "" {
		mode = "log"
	}
	v := InjectionView{Mode: mode, Patterns: len(c.Patterns), ExtraPatterns: append([]string(nil), c.Patterns...)}
	switch mode {
	case "off":
		v.Summary = "Results are not scanned for prompt-injection patterns."
	case "log":
		v.Summary = "Results are scanned and matches are recorded, but the call is not blocked."
	case "confirm":
		v.Summary = "A result that matches an injection pattern requires human approval before it is delivered."
	case "deny":
		v.Summary = "A result that matches an injection pattern is withheld."
	}
	return v
}

// Field sets the engine actually reads. A policy may carry other keys; the
// YAML decoder ignores them, so the Studio reports them instead of letting
// an operator believe they are enforced.
var (
	policyFields  = map[string]bool{"version": true, "default": true, "rules": true, "flows": true, "limits": true, "injection": true}
	ruleFields    = map[string]bool{"id": true, "tool": true, "action": true, "reason": true, "tasks": true, "expires": true, "where": true}
	flowFields    = map[string]bool{"id": true, "from": true, "to": true, "mode": true, "action": true, "reason": true}
	limitFields   = map[string]bool{"max_calls": true, "max_calls_per_tool": true, "max_per_minute": true, "max_response_bytes": true}
	injectFields  = map[string]bool{"mode": true, "patterns": true}
)

// unsupportedFields lists keys present in the document that the engine does
// not read. Order follows the document.
func unsupportedFields(source []byte) []string {
	out := []string{}
	if len(source) == 0 {
		return out
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(source, &doc); err != nil {
		return out
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return out
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return out
	}
	collect := func(label string, n *yaml.Node, allowed map[string]bool) {
		for i := 0; i+1 < len(n.Content); i += 2 {
			key := n.Content[i].Value
			if !allowed[key] {
				out = append(out, fmt.Sprintf("%s: unsupported field %q is ignored by the engine", label, key))
			}
		}
	}
	collect("policy", root, policyFields)
	for i := 0; i+1 < len(root.Content); i += 2 {
		key, val := root.Content[i].Value, root.Content[i+1]
		switch key {
		case "rules":
			if val.Kind != yaml.SequenceNode {
				continue
			}
			for idx, item := range val.Content {
				if item.Kind == yaml.MappingNode {
					collect(fmt.Sprintf("rules[%d]", idx), item, ruleFields)
				}
			}
		case "flows":
			if val.Kind != yaml.SequenceNode {
				continue
			}
			for idx, item := range val.Content {
				if item.Kind == yaml.MappingNode {
					collect(fmt.Sprintf("flows[%d]", idx), item, flowFields)
				}
			}
		case "limits":
			if val.Kind == yaml.MappingNode {
				collect("limits", val, limitFields)
			}
		case "injection":
			if val.Kind == yaml.MappingNode {
				collect("injection", val, injectFields)
			}
		}
	}
	return out
}

// ruleSummary states a rule in one sentence, using a verb phrase per action.
func ruleSummary(v *RuleView) string {
	verb := "Allow"
	switch Verdict(v.Action) {
	case VerdictDeny:
		verb = "Block"
	case VerdictConfirm:
		verb = "Require human approval for"
	}
	s := verb + " " + v.ToolSummary
	if len(v.Tasks) > 0 {
		s += " only when the task label is " + strings.Join(v.Tasks, " or ")
	}
	if v.Expires != "" {
		s += " and only before " + v.Expires
	}
	if len(v.Conditions) > 0 {
		s += ", only when every argument condition below holds"
	}
	return s + "."
}

func flowSummary(v *FlowView) string {
	verb, mode := "Blocks", "once a permitted source call has run"
	if v.Action == string(VerdictConfirm) {
		verb = "Requires approval for"
	}
	if v.Mode == "value" {
		mode = "only when recorded values from that source appear in the call's arguments"
	}
	return fmt.Sprintf("%s %s %s (source tools %s).", verb, describeTools(v.To), mode, describeTools(v.From))
}
