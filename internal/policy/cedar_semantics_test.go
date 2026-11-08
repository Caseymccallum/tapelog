package policy

import "testing"

// These assertions pin the Cedar `like` semantics the shipped policy packs
// depend on. M5.1 existed because nobody had written them down: three packs
// used `%` as a wildcard, which compiles cleanly and never matches.
//
// If cedar-go ever changes any of this, the packs silently stop enforcing
// again. This test is the alarm.
func TestCedarLikeSemanticsThePacksRelyOn(t *testing.T) {
	cases := []struct {
		name string
		expr string
		args string
		want bool
	}{
		// Exactly one wildcard exists.
		{"star matches any sequence", `context.args.c like "*DROP *"`, `{"c":"DROP TABLE t"}`, true},
		{"star is anchored at both ends", `context.args.c like "DROP *"`, `{"c":"x DROP TABLE t"}`, false},
		{"a pattern with no star is an equality test", `context.args.c like "rw"`, `{"c":"rw"}`, true},
		{"a pattern with no star is not a substring test", `context.args.c like "rw"`, `{"c":"x rw y"}`, false},

		// The characters that were mistaken for wildcards are literal.
		{"percent is literal", `context.args.c like "%DROP %"`, `{"c":"DROP TABLE t"}`, false},
		{"percent matches a real percent sign", `context.args.c like "*%DROP *"`, `{"c":"%DROP x"}`, true},
		{"a character class is literal text", `context.args.u like "http://172.1[6-9]*"`, `{"u":"http://172.16.0.1/"}`, false},
		{"a character class matches its own characters", `context.args.u like "http://172.1[6-9]*"`, `{"u":"http://172.1[6-9].0.1"}`, true},
		{"question mark is literal", `context.args.c like "?rm"`, `{"c":"xrm"}`, false},

		// Matching is case-sensitive, which is why the SQL pack enumerates cases
		// and why URL hosts are documented as a known gap.
		{"like is case sensitive", `context.args.c like "DROP *"`, `{"c":"drop table t"}`, false},
		{"scheme wildcard covers scheme casing", `context.args.u like "*://localhost*"`, `{"u":"HTTPS://localhost/"}`, true},
		{"host casing is not covered", `context.args.u like "*://localhost*"`, `{"u":"http://LOCALHOST/"}`, false},

		// Whitespace: a tab must be spelled as an escape.
		{"tab needs an escape", `context.args.q like "*DROP *"`, `{"q":"DROP\tTABLE t"}`, false},
		{"tab escape matches a tab", `context.args.q like "*DROP\t*"`, `{"q":"DROP\tTABLE t"}`, true},

		// Presence and boolean operators, used to fail closed.
		{"has on a present key", `context.args has url`, `{"url":"x"}`, true},
		{"not has on a missing key", `!(context.args has url)`, `{"other":"x"}`, true},
		{"or tolerates an error on the other side", `!(context.args has url) || !(context.args.url like "https://*")`, `{"other":"x"}`, true},
		{"and over a missing attribute fails closed", `!(context.args has url) && !(context.args.url like "https://*")`, `{"other":"x"}`, false},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			cs, err := compileWhere("semantics", []string{c.expr})
			if err != nil {
				t.Fatalf("compile %s: %v", c.expr, err)
			}
			if got := cs.passes(Request{Tool: "t", Args: []byte(c.args)}); got != c.want {
				t.Fatalf("%s with %s: want %v, got %v", c.expr, c.args, c.want, got)
			}
		})
	}
}
