package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Caseymccallum/tapelog/internal/fuzz"
	"github.com/Caseymccallum/tapelog/internal/policy"
	"github.com/Caseymccallum/tapelog/internal/replay"
)

// newFuzzCmd is the boundary hardening lab: mutate a recorded session and
// report policy holes (deny verdicts that mutations bypass).
func newFuzzCmd() *cobra.Command {
	var (
		policyPath string
		operators  []string
		max        int
		asJSON     bool
	)
	cmd := &cobra.Command{
		Use:   "fuzz [flags] <session.jsonl>",
		Short: "Fuzz the policy boundary with mutated recorded sessions",
		Long: `Fuzz the policy boundary with mutated recorded sessions.

Every recorded call is mutated with attack-shaped transformations (tool-name
spoofing, homoglyphs, path traversal in arguments, toxic-flow reordering)
and re-evaluated against the policy. A mutation that turns a deny into an
allow is a boundary bypass — a policy hole a real attacker could use.
Findings are reproducible and carry indicative MITRE ATLAS tactics.

  tapelog fuzz --policy prod.yaml session.jsonl
  tapelog fuzz --policy prod.yaml --operators tool_case,tool_homoglyph session.jsonl
  tapelog fuzz --policy prod.yaml --json session.jsonl > findings.json

Exits non-zero when any finding is reported.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if policyPath == "" {
				return fmt.Errorf("--policy is required (the oracle is the policy)")
			}
			pol, err := policy.Load(policyPath)
			if err != nil {
				return err
			}
			tape, err := replay.Load(args[0])
			if err != nil {
				return err
			}
			findings := fuzz.Run(tape, pol, fuzz.Options{
				Operators: operators, MaxFindings: max,
			})

			if asJSON {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				_ = enc.Encode(findings)
			} else if len(findings) == 0 {
				nOps := len(operators)
				if nOps == 0 {
					nOps = len(fuzz.Operators)
				}
				fmt.Printf("no boundary bypasses found (%d calls mutated across %d operators)\n",
					len(tape.Interactions), nOps)
			} else {
				for _, f := range findings {
					fmt.Printf("[%s] %s: %s\n", f.Operator, f.Tool, f.Mutation)
					fmt.Printf("    deny -> %s  (rule: %s)  ATLAS: %s\n", f.NowVerdict, f.RuleID, f.ATLAS)
				}
				fmt.Printf("%d boundary bypass(es) found\n", len(findings))
			}
			if len(findings) > 0 {
				return fmt.Errorf("%d boundary bypass(es)", len(findings))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&policyPath, "policy", "", "policy YAML (the fuzz oracle)")
	cmd.Flags().StringArrayVar(&operators, "operator", nil, "mutation operator to run (repeatable; default all: "+strings.Join(fuzz.Operators, ", ")+")")
	cmd.Flags().IntVar(&max, "max-findings", 0, "stop after N findings (0 = unlimited)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit findings as JSON")
	return cmd
}
