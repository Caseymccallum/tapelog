package fuzz

import (
	"fmt"

	"github.com/Caseymccallum/tapelog/internal/policy"
	"github.com/Caseymccallum/tapelog/internal/replay"
)

// Options configures a fuzz run.
type Options struct {
	Operators   []string // empty = all
	MaxFindings int      // 0 = unlimited
}

// Run mutates every recorded call and reports boundary bypasses: mutants
// whose verdict flips deny -> allow/confirm (the policy hole a real
// attacker could use). The oracle is comparative — a mutation of a denied
// call that sails through is a finding; one that stays denied is "held".
func Run(tape *replay.Tape, ev policy.Evaluator, opts Options) []Finding {
	enabled := map[string]bool{}
	if len(opts.Operators) == 0 {
		for _, op := range Operators {
			enabled[op] = true
		}
	} else {
		for _, op := range opts.Operators {
			enabled[op] = true
		}
	}

	seq := append([]replay.Interaction(nil), tape.Interactions...)
	base := verdicts(seq, ev)

	var findings []Finding
	emit := func(f Finding) bool {
		findings = append(findings, f)
		return opts.MaxFindings == 0 || len(findings) < opts.MaxFindings
	}

	for i := range seq {
		if base[i] != "deny" {
			continue // bypass oracle only fires on denied calls
		}
		it := seq[i]
		ruleID := ruleFor(ev, it.Tool, it.Args)

		for _, m := range mutate(it, enabled) {
			mutant := it
			if m.Tool != "" {
				mutant.Tool = m.Tool
			}
			if m.Args != nil {
				mutant.Args = m.Args
			}
			probe := append([]replay.Interaction(nil), seq...)
			probe[i] = mutant
			now := verdicts(probe, ev)[i]
			if now != "deny" {
				if !emit(mkFinding(m, it, "deny", now, ruleID)) {
					return findings
				}
			}
		}

		// Argument tampering and name mutations handled above.
		_ = it
	}

	// Toxic-flow reordering: swap each adjacent pair and re-walk. A deny
	// that flips to allow after the swap evades an order-dependent rule.
	if enabled["swap_adjacent"] {
		for j := 0; j+1 < len(seq); j++ {
			probe := append([]replay.Interaction(nil), seq...)
			probe[j], probe[j+1] = probe[j+1], probe[j]
			after := verdicts(probe, ev)
			for _, pos := range []int{j, j + 1} {
				if base[pos] == "deny" && after[pos] != "deny" {
					m := Mutation{Operator: "swap_adjacent",
						Desc:  fmt.Sprintf("swap positions %d and %d", j+1, j+2),
						ATLAS: "Exfiltration"}
					if !emit(mkFinding(m, seq[pos], "deny", after[pos], ruleFor(ev, seq[pos].Tool, seq[pos].Args))) {
						return findings
					}
				}
			}
		}
	}
	return findings
}
