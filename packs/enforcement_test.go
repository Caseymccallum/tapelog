// Behavioural tests for the shipped policy packs.
//
// M5 found that packs_test.go only proved a pack *compiles*. A
// `like "%rm -rf /%"` condition compiles perfectly and never matches, so every
// deny in three of the packs was inert while CI stayed green. These tests
// assert enforcement instead: the intended operation is denied, the intended
// rule is the one that matched, the decision does not come from the default,
// and a legitimate neighbouring operation still receives the documented
// treatment.
package packs

import (
	"strings"
	"testing"

	"github.com/Caseymccallum/tapelog/internal/policy"
)

// caseSpec is one documented expectation about a shipped pack.
type caseSpec struct {
	name      string
	pack      string
	tool      string
	args      string
	verdict   policy.Verdict // what the pack documents for this call
	rule      string         // exact rule id; "default" means no rule matched
	rationale string
}

var packCache = map[string]*policy.Policy{}

func loadPack(t *testing.T, name string) *policy.Policy {
	t.Helper()
	if cached, ok := packCache[name]; ok {
		return cached
	}
	p, err := policy.Load(name + ".yaml")
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	packCache[name] = p
	return p
}

// runCases is the single enforcement harness shared by every table.
func runCases(t *testing.T, cases []caseSpec) {
	t.Helper()
	for _, c := range cases {
		c := c
		t.Run(c.pack+"/"+c.name, func(t *testing.T) {
			p := loadPack(t, c.pack)
			dec := p.Evaluate(policy.Request{Tool: c.tool, Args: []byte(c.args)})
			if dec.Verdict != c.verdict {
				t.Fatalf("want verdict %s, got %s by rule %q — %s\nargs: %s",
					c.verdict, dec.Verdict, dec.RuleID, c.rationale, c.args)
			}
			if dec.RuleID != c.rule {
				t.Fatalf("want rule %q, got %q (verdict %s)", c.rule, dec.RuleID, dec.Verdict)
			}
			if c.verdict == policy.VerdictDeny {
				// "Blocked before execution": the decision must come from a
				// rule, not a permissive default, and it must carry the
				// explanation the mediator records.
				if dec.IsDefault || dec.RuleID == "default" {
					t.Fatal("deny came from the default, not a rule")
				}
				if strings.TrimSpace(dec.Reason) == "" {
					t.Fatal("a deny must carry an explainable reason")
				}
			}
		})
	}
}
