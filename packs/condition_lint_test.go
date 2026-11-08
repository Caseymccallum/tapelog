package packs

import (
	"strings"
	"testing"

	"github.com/Caseymccallum/tapelog/internal/policy"
)

// TestNoPackUsesPercentInACondition is the root-cause guard for M5.1.
//
// Cedar's `like` has exactly one wildcard, `*`. `%`, `?` and `[a-z]` are
// literal characters, so `like "%rm -rf /%"` compiles cleanly and can never
// match. Three shipped packs were written that way and every deny in them was
// inert while packs_test.go — which only compiles the file — stayed green.
//
// No pack in this directory needs a literal percent sign, so any `%` inside a
// condition is a mistaken wildcard.
func TestNoPackUsesPercentInACondition(t *testing.T) {
	for _, p := range All() {
		parsed, err := policy.Parse([]byte(p.Source), p.Name)
		if err != nil {
			t.Fatalf("%s: %v", p.Name, err)
		}
		for _, r := range parsed.Rules {
			for _, expr := range r.Where {
				if strings.Contains(expr, "%") {
					t.Errorf("%s rule %q: `%%` is a literal character in Cedar, not a wildcard — use `*`:\n  %s",
						p.Name, r.ID, expr)
				}
			}
		}
	}
}

// TestNoPackHasAConditionThatCannotMatch asks the same question through the
// M5 Policy Studio analyser, so the Studio and CI agree that the shipped packs
// are free of dead conditions.
func TestNoPackHasAConditionThatCannotMatch(t *testing.T) {
	for _, p := range All() {
		parsed, err := policy.Parse([]byte(p.Source), p.Name)
		if err != nil {
			t.Fatalf("%s: %v", p.Name, err)
		}
		a := policy.Analyze(parsed, []byte(p.Source))
		if a.DeadConditions != 0 {
			for _, r := range a.Rules {
				for _, c := range r.Conditions {
					if c.Problem != "" {
						t.Errorf("%s rule %q: %s\n  %s", p.Name, r.ID, c.Problem, c.Expr)
					}
				}
			}
			t.Fatalf("%s: %d condition(s) the Studio reports as unable to match", p.Name, a.DeadConditions)
		}
		for _, warning := range a.Warnings {
			if strings.Contains(warning, "cannot match") {
				t.Errorf("%s: the Studio still warns %q", p.Name, warning)
			}
		}
	}
}

// TestEveryDenyRuleCanFire proves the stronger property the M5 incident was
// about: for every rule with action deny in a pack, at least one documented
// call in this package's case tables actually reaches it. A rule that can never
// match is exactly the failure mode that shipped.
func TestEveryDenyRuleCanFire(t *testing.T) {
	reached := map[string]map[string]bool{}
	record := func(c caseSpec) {
		dec := loadPack(t, c.pack).Evaluate(policy.Request{Tool: c.tool, Args: []byte(c.args)})
		if dec.Verdict != policy.VerdictDeny {
			return
		}
		if reached[c.pack] == nil {
			reached[c.pack] = map[string]bool{}
		}
		reached[c.pack][dec.RuleID] = true
	}
	for _, table := range [][]caseSpec{shellCases, shellLegitimateCases, gitCases, postgresCases, fetchCases, filesystemAndSlackCases} {
		for _, c := range table {
			record(c)
		}
	}
	for _, p := range All() {
		parsed, err := policy.Parse([]byte(p.Source), p.Name)
		if err != nil {
			t.Fatalf("%s: %v", p.Name, err)
		}
		for _, r := range parsed.Rules {
			if r.Action != string(policy.VerdictDeny) {
				continue
			}
			if !reached[p.Name][r.ID] {
				t.Errorf("%s rule %q is a deny that no enforcement test ever reaches — either it cannot match or it is untested",
					p.Name, r.ID)
			}
		}
		for _, f := range parsed.Flows {
			if f.Action != string(policy.VerdictDeny) {
				continue
			}
			// Flows need a tainted session; covered by the flow tests in
			// internal/policy and by the Studio simulation test for slack.
			t.Logf("%s: flow %q is exercised by the flow tests, not by these tables", p.Name, f.ID)
		}
	}
}
