package policy

import (
	"strings"
	"testing"
)

const explainFixture = `# fictional policy used by the Policy Studio tests
version: 1
default: deny
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

  - id: deploy
    tool: "deploy"
    action: confirm
    reason: "deployments need a human"
    tasks: ["release"]
    expires: "2026-12-31T23:59:59Z"

  - id: broken
    tool: "exec*"
    action: deny
    reason: "catastrophe"
    where: 'context.args.command like "%rm -rf /%"'

  - id: advanced
    tool: "shell"
    action: deny
    reason: "compound expression"
    where: 'context.args.command like "*sudo *" && context.args.user == "root"'

flows:
  - id: no-exfil
    from: ["read_*"]
    to: ["send_*"]
    action: deny
    reason: "data must not leave"

limits:
  max_calls: 300
  max_per_minute: 60
`

func TestAnalyzeDescribesSupportedSemantics(t *testing.T) {
	p := mustParse(t, []byte(explainFixture))
	a := Analyze(p, []byte(explainFixture))
	if a.Default != "deny" || a.DefaultLabel != "Block" {
		t.Fatalf("default: %+v", a.Default)
	}
	if len(a.Rules) != 5 || len(a.Flows) != 1 {
		t.Fatalf("counts: %d rules %d flows", len(a.Rules), len(a.Flows))
	}
	reads := a.Rules[0]
	if reads.ActionLabel != "Allow" || !strings.Contains(reads.Summary, "Allow calls to tools matching") {
		t.Fatalf("read rule: %+v", reads)
	}
	if !strings.Contains(reads.ScopeSummary, "any task label") || !strings.Contains(reads.ScopeSummary, "no argument conditions") {
		t.Fatalf("scope: %q", reads.ScopeSummary)
	}
	if reads.Broad {
		t.Fatalf("read_* must not be treated as broad: %+v", reads.BroadWhy)
	}
	writes := a.Rules[1]
	if len(writes.Conditions) != 1 || !writes.Conditions[0].Simplified {
		t.Fatalf("a simple like condition should be explained: %+v", writes.Conditions)
	}
	if !strings.Contains(writes.Conditions[0].Summary, "starts with") {
		t.Fatalf("condition summary: %q", writes.Conditions[0].Summary)
	}
	deploy := a.Rules[2]
	if deploy.ActionLabel != "Require approval" || !strings.Contains(deploy.ScopeSummary, "release") || !strings.Contains(deploy.ScopeSummary, "2026-12-31") {
		t.Fatalf("deploy rule: %+v", deploy)
	}
	if a.Flows[0].Action != "deny" || !strings.Contains(a.Flows[0].Summary, "Blocks") {
		t.Fatalf("flow: %+v", a.Flows[0])
	}
	if !a.Limits.Enabled || len(a.Limits.Summary) != 2 {
		t.Fatalf("limits: %+v", a.Limits)
	}
	if !strings.Contains(a.Limits.Note, "not by rule evaluation") {
		t.Fatalf("limits note: %q", a.Limits.Note)
	}
}

// The Studio must not claim a condition blocks something the engine never
// matches. This is the verified Cedar `like` finding: `%` is a literal.
func TestAnalyzeFlagsConditionsThatCannotMatch(t *testing.T) {
	p := mustParse(t, []byte(explainFixture))
	a := Analyze(p, []byte(explainFixture))
	broken := a.Rules[3]
	if len(broken.Conditions) != 1 || broken.Conditions[0].Problem == "" {
		t.Fatalf("the %% pattern should be flagged: %+v", broken.Conditions)
	}
	if !strings.Contains(broken.Conditions[0].Problem, "wildcard") {
		t.Fatalf("problem text: %q", broken.Conditions[0].Problem)
	}
	if a.DeadConditions != 1 {
		t.Fatalf("dead conditions: %d", a.DeadConditions)
	}
	// The analyzer must agree with the engine: the rule really does not fire.
	if got := p.Evaluate(Request{Tool: "exec_cmd", Args: []byte(`{"command":"rm -rf /"}`)}); got.RuleID == "broken" {
		t.Fatalf("engine matched a condition the analyzer called dead: %+v", got)
	}
	// A compound expression is never simplified into a simpler claim.
	advanced := a.Rules[4]
	if advanced.Conditions[0].Simplified || advanced.Conditions[0].Summary != "" {
		t.Fatalf("compound expression must stay raw: %+v", advanced.Conditions[0])
	}
}

func TestAnalyzeFlagsBroadPermissionsAndShadowedRules(t *testing.T) {
	// A universal unconditional rule makes every later rule unreachable.
	universal := `version: 1
default: deny
rules:
  - id: everything
    tool: "*"
    action: allow
  - id: later-deny
    tool: "delete_all"
    action: deny
`
	p := mustParse(t, []byte(universal))
	a := Analyze(p, []byte(universal))
	if !a.Rules[0].Broad || a.BroadPermissions != 1 {
		t.Fatalf("broad: %d", a.BroadPermissions)
	}
	if !strings.Contains(strings.Join(a.Rules[0].BroadWhy, " "), "every tool") {
		t.Fatalf("broad why: %+v", a.Rules[0].BroadWhy)
	}
	if a.Rules[1].Unreachable == "" || !strings.Contains(a.Rules[1].Unreachable, "everything") {
		t.Fatalf("unreachable rule not reported: %+v", a.Rules[1])
	}
	if a.ShadowedRules != 1 {
		t.Fatalf("shadowed: %d", a.ShadowedRules)
	}

	// Without a universal rule, only a repeated identical pattern is
	// unreachable, and a short prefix is broad but still reachable.
	partial := `version: 1
default: deny
rules:
  - id: prefixed
    tool: "ab*"
    action: allow
  - id: another
    tool: "read_*"
    action: allow
  - id: duplicate
    tool: "ab*"
    action: deny
`
	p = mustParse(t, []byte(partial))
	a = Analyze(p, []byte(partial))
	if !a.Rules[0].Broad {
		t.Fatalf("a short prefix is broad: %+v", a.Rules[0].BroadWhy)
	}
	if a.Rules[1].Broad {
		t.Fatalf("read_* is not broad: %+v", a.Rules[1].BroadWhy)
	}
	if a.Rules[0].Unreachable != "" || a.Rules[1].Unreachable != "" {
		t.Fatalf("reachable rules reported unreachable: %+v %+v", a.Rules[0].Unreachable, a.Rules[1].Unreachable)
	}
	if a.Rules[2].Unreachable == "" || !strings.Contains(a.Rules[2].Unreachable, "prefixed") {
		t.Fatalf("duplicate pattern should be unreachable: %+v", a.Rules[2])
	}
	if a.ShadowedRules != 1 {
		t.Fatalf("shadowed: %d", a.ShadowedRules)
	}
}

func TestAnalyzeReportsFieldsTheEngineIgnores(t *testing.T) {
	src := `version: 1
default: deny
rules:
  - id: r
    tool: "t"
    action: allow
    when: "context.args.x == 1"
limits:
  max_calls: 10
  max_tokens: 5
`
	p := mustParse(t, []byte(src))
	a := Analyze(p, []byte(src))
	joined := strings.Join(a.Unsupported, "\n")
	if !strings.Contains(joined, `rules[0]`) || !strings.Contains(joined, `"when"`) {
		t.Fatalf("rule-level unsupported field lost: %+v", a.Unsupported)
	}
	if !strings.Contains(joined, `"max_tokens"`) {
		t.Fatalf("limits field lost: %+v", a.Unsupported)
	}
}

func TestAnalyzeWarnsAboutAPermissiveDefault(t *testing.T) {
	src := "version: 1\ndefault: allow\nrules:\n  - id: r\n    tool: \"t\"\n    action: deny\n"
	a := Analyze(mustParse(t, []byte(src)), []byte(src))
	if len(a.Warnings) == 0 || !strings.Contains(a.Warnings[0], "default is allow") {
		t.Fatalf("warnings: %+v", a.Warnings)
	}
}

func TestDescribeConditionOnlySimplifiesSingleComparisons(t *testing.T) {
	cases := []struct {
		expr       string
		simplified bool
		fragment   string
	}{
		{`context.args.path like "/tmp/*"`, true, "starts with"},
		{`context.args.path like "*.pem"`, true, "ends with"},
		{`context.args.body like "*secret*"`, true, "contains"},
		{`context.args.mode like "rw"`, true, "equals"},
		{`context.args.any like "*"`, true, "any value"},
		{`context.args.mode == "rw"`, true, "equals"},
		{`context.args.amount == 500`, true, "equals"},
		{`context.args.force == true`, true, "equals"},
		{`context.args.a like "*x*y*"`, false, ""},
		{`context.args.a like "*x*" || context.args.b == 1`, false, ""},
		// A compound expression whose last clause ends with a quote must not
		// be flattened into a single comparison. This is the shipped
		// packs/shell.yaml shape.
		{`context.args.command like "%rm -rf /%" || context.args.command like "%mkfs%"`, false, ""},
		{`context.args.a == "x" || context.args.b == "y"`, false, ""},
		{`context.args.a == "x" && context.args.b == "y"`, false, ""},
		{`context.args.a.b == 1`, false, ""},
		{`context.args has a`, false, ""},
	}
	for _, c := range cases {
		v := describeCondition(c.expr)
		if v.Simplified != c.simplified {
			t.Fatalf("%s: simplified=%v want %v (%+v)", c.expr, v.Simplified, c.simplified, v)
		}
		if c.simplified && !strings.Contains(v.Summary, c.fragment) {
			t.Fatalf("%s: summary %q lacks %q", c.expr, v.Summary, c.fragment)
		}
		if c.simplified && v.Note == "" {
			t.Fatalf("%s: a simplified condition must keep the missing-argument note", c.expr)
		}
		if !c.simplified && v.Summary != "" {
			t.Fatalf("%s: unsupported expressions must not be summarised (%q)", c.expr, v.Summary)
		}
	}
}

