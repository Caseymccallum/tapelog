package policy

import (
	"strings"
	"testing"
)

func draft(t *testing.T, d RuleDraft) RuleDraft {
	t.Helper()
	if err := ValidateRuleDraft(&d); err != nil {
		t.Fatalf("draft rejected: %v", err)
	}
	return d
}

func mustParse(t *testing.T, src []byte) *Policy {
	t.Helper()
	p, err := Parse(src, "candidate")
	if err != nil {
		t.Fatalf("generated policy does not compile: %v\n%s", err, src)
	}
	return p
}

// A generated rule must compile through the real loader and decide exactly
// the tool it names.
func TestNewCandidateIsNarrowAndCompiles(t *testing.T) {
	d := draft(t, RuleDraft{Action: "deny", Tools: []string{"files__write"}, Reason: "fictional: writes need review"})
	if d.ID != "deny-files-write" {
		t.Fatalf("id: %q", d.ID)
	}
	src, err := NewCandidate("allow", d)
	if err != nil {
		t.Fatal(err)
	}
	p := mustParse(t, src)
	if len(p.Rules) != 1 || p.Rules[0].ID != d.ID || p.Rules[0].Action != "deny" {
		t.Fatalf("rules: %+v", p.Rules)
	}
	if got := p.Evaluate(Request{Tool: "files__write", Args: []byte(`{}`)}); got.Verdict != VerdictDeny || got.RuleID != d.ID {
		t.Fatalf("decision: %+v", got)
	}
	// A neighbouring tool of the same server is untouched.
	if got := p.Evaluate(Request{Tool: "files__write_all", Args: []byte(`{}`)}); got.Verdict != VerdictAllow {
		t.Fatalf("rule was broader than the recorded tool: %+v", got)
	}
}

func TestGeneratedConditionsMeanWhatTheySay(t *testing.T) {
	cases := []struct {
		name  string
		cond  ConditionDraft
		args  string
		match bool
	}{
		{"starts_with match", ConditionDraft{Arg: "path", Op: "starts_with", Value: "/tmp/"}, `{"path":"/tmp/report"}`, true},
		{"starts_with miss", ConditionDraft{Arg: "path", Op: "starts_with", Value: "/tmp/"}, `{"path":"/etc/report"}`, false},
		{"contains match", ConditionDraft{Arg: "path", Op: "contains", Value: "secret"}, `{"path":"a/secret/b"}`, true},
		{"equals match", ConditionDraft{Arg: "mode", Op: "equals", Value: "rw"}, `{"mode":"rw"}`, true},
		{"ends_with match", ConditionDraft{Arg: "path", Op: "ends_with", Value: ".pem"}, `{"path":"/keys/x.pem"}`, true},
		{"number match", ConditionDraft{Arg: "amount", Kind: "number", Op: "equals", Value: "500"}, `{"amount":500}`, true},
		{"bool match", ConditionDraft{Arg: "force", Kind: "bool", Op: "equals", Value: "true"}, `{"force":true}`, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := draft(t, RuleDraft{Action: "deny", Tools: []string{"write_file"}, Conditions: []ConditionDraft{c.cond}})
			src, err := NewCandidate("allow", d)
			if err != nil {
				t.Fatal(err)
			}
			p := mustParse(t, src)
			got := p.Evaluate(Request{Tool: "write_file", Args: []byte(c.args)})
			if c.match && got.Verdict != VerdictDeny {
				t.Fatalf("condition should match: %+v\n%s", got, src)
			}
			if !c.match && got.Verdict != VerdictAllow {
				t.Fatalf("condition should not match: %+v\n%s", got, src)
			}
		})
	}
}

// A recorded argument value is data. It must never become policy syntax or a
// wildcard, and a missing argument must not grant or deny by accident.
func TestConditionValuesCannotInjectOrWiden(t *testing.T) {
	literal := []string{
		`/tmp/*`,
		`x" || true || "y`,
		`back\slash"quote`,
		`${injection}`,
		`']; drop table --`,
	}
	for _, value := range literal {
		d := draft(t, RuleDraft{Action: "deny", Tools: []string{"write_file"}, Conditions: []ConditionDraft{{Arg: "path", Op: "starts_with", Value: value}}})
		src, err := NewCandidate("allow", d)
		if err != nil {
			t.Fatalf("%q: %v", value, err)
		}
		p := mustParse(t, src)
		// The exact value still matches.
		args := `{"path":` + quoteJSON(t, value+"-suffix") + `}`
		if got := p.Evaluate(Request{Tool: "write_file", Args: []byte(args)}); got.Verdict != VerdictDeny {
			t.Fatalf("%q should still match its own value: %+v\n%s", value, got, src)
		}
		// A different value must not match, which is what a leaked wildcard
		// or injected boolean would break.
		if got := p.Evaluate(Request{Tool: "write_file", Args: []byte(`{"path":"/elsewhere"}`)}); got.Verdict != VerdictAllow {
			t.Fatalf("%q widened the rule: %+v\n%s", value, got, src)
		}
		// Missing argument: the condition errors and the engine skips it.
		if got := p.Evaluate(Request{Tool: "write_file", Args: []byte(`{"other":1}`)}); got.Verdict != VerdictAllow {
			t.Fatalf("%q changed behaviour when the argument is absent: %+v", value, got)
		}
	}
}

func quoteJSON(t *testing.T, s string) string {
	t.Helper()
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// Extending an existing policy must not reformat it: comments, unknown fields
// and hand-written conditions have to survive byte for byte, and the existing
// rules must keep their order and ids.
func TestInsertRulePreservesExistingSource(t *testing.T) {
	original := `# Team policy — do not reorder lightly
version: 1
default: deny
metadata:
  owner: fictional-team

rules:
  # reads are routine
  - id: allow-reads
    tool: ["read_*", "get_*"]
    action: allow
    reason: "read-only tools are routine"

  - id: tmp-writes
    tool: "write_file"
    action: allow
    reason: "writes restricted to /tmp"
    where: 'context.args.path like "/tmp/*"'

flows:
  - id: no-exfil
    from: ["read_*"]
    to: ["send_*"]
    action: deny
`
	d := draft(t, RuleDraft{Action: "deny", Tools: []string{"files__write"}, Reason: "fictional proposal"})
	out, err := InsertRule([]byte(original), d)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	text := string(out)
	for _, keep := range []string{
		"# Team policy — do not reorder lightly",
		"metadata:\n  owner: fictional-team",
		"  # reads are routine",
		`where: 'context.args.path like "/tmp/*"'`,
		"flows:\n  - id: no-exfil",
	} {
		if !strings.Contains(text, keep) {
			t.Fatalf("existing source was modified; missing %q\n%s", keep, text)
		}
	}
	p := mustParse(t, out)
	if len(p.Rules) != 3 {
		t.Fatalf("rule count %d\n%s", len(p.Rules), text)
	}
	if p.Rules[0].ID != d.ID {
		t.Fatalf("the new rule must be evaluated first, got %q", p.Rules[0].ID)
	}
	if p.Rules[1].ID != "allow-reads" || p.Rules[2].ID != "tmp-writes" {
		t.Fatalf("existing rule order changed: %s, %s", p.Rules[1].ID, p.Rules[2].ID)
	}
	if len(p.Flows) != 1 {
		t.Fatalf("flows were lost: %+v", p.Flows)
	}
	// Unknown fields are still present, and still reported as ignored.
	unknown := Analyze(p, out).Unsupported
	if len(unknown) != 1 || !strings.Contains(unknown[0], "metadata") {
		t.Fatalf("unknown fields: %+v", unknown)
	}
}

// A shape the visual editor cannot edit safely must be refused, not guessed.
func TestInsertRuleRefusesInlineRules(t *testing.T) {
	_, err := InsertRule([]byte("version: 1\ndefault: deny\nrules: [{id: a, tool: x, action: allow}]\n"), draft(t, RuleDraft{Action: "deny", Tools: []string{"t"}}))
	if err == nil || !strings.Contains(err.Error(), "inline") {
		t.Fatalf("want an inline-rules refusal, got %v", err)
	}
}

func TestInsertRuleAppendsWhenNoRulesKey(t *testing.T) {
	out, err := InsertRule([]byte("version: 1\ndefault: deny\n"), draft(t, RuleDraft{Action: "deny", Tools: []string{"t"}}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(out), "version: 1\ndefault: deny\n") {
		t.Fatalf("prefix changed:\n%s", out)
	}
	p := mustParse(t, out)
	if len(p.Rules) != 1 || p.Rules[0].Tool[0] != "t" {
		t.Fatalf("rules: %+v", p.Rules)
	}
}

func TestInsertRuleKeepsCustomIndentation(t *testing.T) {
	out, err := InsertRule([]byte("version: 1\ndefault: deny\nrules:\n    - id: a\n      tool: x\n      action: allow\n"), draft(t, RuleDraft{Action: "deny", Tools: []string{"y"}}))
	if err != nil {
		t.Fatal(err)
	}
	p := mustParse(t, out)
	if len(p.Rules) != 2 || p.Rules[0].Tool[0] != "y" || p.Rules[1].ID != "a" {
		t.Fatalf("rules: %+v\n%s", p.Rules, out)
	}
}

func TestValidateRuleDraftRejectsWhatTheEngineCannotExpress(t *testing.T) {
	base := func(mut func(*RuleDraft)) error {
		d := RuleDraft{Action: "deny", Tools: []string{"t"}}
		mut(&d)
		return ValidateRuleDraft(&d)
	}
	cases := map[string]func(*RuleDraft){
		"nested argument":    func(d *RuleDraft) { d.Conditions = []ConditionDraft{{Arg: "a.b", Op: "equals", Value: "x"}} },
		"hyphen argument":    func(d *RuleDraft) { d.Conditions = []ConditionDraft{{Arg: "my-key", Op: "equals", Value: "x"}} },
		"bad action":         func(d *RuleDraft) { d.Action = "permit" },
		"no tools":           func(d *RuleDraft) { d.Tools = nil },
		"newline in tool":    func(d *RuleDraft) { d.Tools = []string{"a\nb"} },
		"newline in reason":  func(d *RuleDraft) { d.Reason = "a\nb" },
		"newline in value":   func(d *RuleDraft) { d.Conditions = []ConditionDraft{{Arg: "a", Op: "equals", Value: "x\ny"}} },
		"decimal number":     func(d *RuleDraft) { d.Conditions = []ConditionDraft{{Arg: "a", Kind: "number", Op: "equals", Value: "1.5"}} },
		"number with like":   func(d *RuleDraft) { d.Conditions = []ConditionDraft{{Arg: "a", Kind: "number", Op: "starts_with", Value: "1"}} },
		"bad operator":       func(d *RuleDraft) { d.Conditions = []ConditionDraft{{Arg: "a", Op: "regex", Value: "x"}} },
		"bad expiry":         func(d *RuleDraft) { d.Expires = "next tuesday" },
		"bad id":             func(d *RuleDraft) { d.ID = "has spaces" },
		"too many conditions": func(d *RuleDraft) {
			for i := 0; i < 9; i++ {
				d.Conditions = append(d.Conditions, ConditionDraft{Arg: "a", Op: "equals", Value: "x"})
			}
		},
	}
	for name, mut := range cases {
		if err := base(mut); err == nil {
			t.Fatalf("%s: expected a refusal", name)
		}
	}
}

func TestUniqueIDAvoidsExistingIDs(t *testing.T) {
	taken := map[string]bool{"deny-x": true, "deny-x-2": true}
	if got := UniqueID("deny-x", taken); got != "deny-x-3" {
		t.Fatalf("got %q", got)
	}
}

// Session limits are generated only from the fields the boundary enforces,
// and they must compile through the same path.
func TestNewCandidateWithLimits(t *testing.T) {
	d := draft(t, RuleDraft{Action: "confirm", Tools: []string{"files__write"}})
	src, err := NewCandidateWithLimits("deny", d, LimitsDraft{MaxCalls: 300, MaxPerMinute: 60, MaxResponseBytes: 2097152})
	if err != nil {
		t.Fatal(err)
	}
	p := mustParse(t, src)
	if p.Limits.MaxCalls != 300 || p.Limits.MaxPerMinute != 60 || p.Limits.MaxResponseBytes != 2097152 {
		t.Fatalf("limits: %+v\n%s", p.Limits, src)
	}
	if !p.Limits.Enabled() {
		t.Fatal("limits should be enabled")
	}
	// Zero means omitted, not a zero limit.
	src, err = NewCandidateWithLimits("deny", d, LimitsDraft{MaxCalls: 0, MaxPerMinute: 10})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(src), "max_calls") || strings.Contains(string(src), "max_response_bytes") {
		t.Fatalf("unset limits must be omitted:\n%s", src)
	}
	p = mustParse(t, src)
	if p.Limits.MaxCalls != 0 || p.Limits.MaxPerMinute != 10 {
		t.Fatalf("limits: %+v", p.Limits)
	}
	// Nothing set means no limits block at all.
	src, err = NewCandidateWithLimits("deny", d, LimitsDraft{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(src), "limits:") {
		t.Fatalf("no limits block expected:\n%s", src)
	}
	for name, lim := range map[string]LimitsDraft{
		"negative calls":   {MaxCalls: -1},
		"huge calls":       {MaxCalls: 2000000},
		"huge per minute":  {MaxPerMinute: 200000},
		"huge byte cap":    {MaxResponseBytes: 2 << 30},
	} {
		if err := lim.Validate(); err == nil {
			t.Fatalf("%s: expected a refusal", name)
		}
	}
	if err := (LimitsDraft{MaxCalls: 1}).Validate(); err != nil {
		t.Fatalf("a single limit should be valid: %v", err)
	}
}

