package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Caseymccallum/tapelog/internal/policy"
	"github.com/Caseymccallum/tapelog/internal/replay"
)

func newPolicyCmd() *cobra.Command {
	policyCmd := &cobra.Command{
		Use:   "policy",
		Short: "Work with tool-call policies",
	}
	policyCmd.AddCommand(newPolicyTestCmd())
	policyCmd.AddCommand(newPolicyWhatIfCmd())
	policyCmd.AddCommand(newPolicyCompileCmd())
	return policyCmd
}

// sampleCall is one line of a policy-test calls file.
type sampleCall struct {
	Tool string          `json:"tool"`
	Args json.RawMessage `json:"args"`
}

func newPolicyTestCmd() *cobra.Command {
	var policyPath, callsPath string
	cmd := &cobra.Command{
		Use:   "test",
		Short: "Evaluate sample tool calls against a policy",
		Long: `Evaluates JSONL sample calls ({"tool": "...", "args": {...}}) against a
policy and prints the verdict for each — validate a policy before deploying
it. Reads stdin when --calls is omitted.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if policyPath == "" {
				return fmt.Errorf("--policy is required")
			}
			p, err := policy.Load(policyPath)
			if err != nil {
				return err
			}

			in := os.Stdin
			if callsPath != "" {
				f, err := os.Open(callsPath)
				if err != nil {
					return fmt.Errorf("open calls file: %w", err)
				}
				defer f.Close()
				in = f
			}

			fmt.Printf("%-8s %-30s %-14s %s\n", "VERDICT", "TOOL", "RULE", "REASON")
			sc := bufio.NewScanner(in)
			n := 0
			for sc.Scan() {
				line := strings.TrimSpace(sc.Text())
				if line == "" || strings.HasPrefix(line, "#") {
					continue
				}
				var call sampleCall
				if err := json.Unmarshal([]byte(line), &call); err != nil {
					return fmt.Errorf("bad call on line %d: %w", n+1, err)
				}
				dec := p.Evaluate(policy.Request{Tool: call.Tool, Args: call.Args})
				fmt.Printf("%-8s %-30s %-14s %s\n", dec.Verdict, call.Tool, dec.RuleID, dec.Reason)
				n++
			}
			if err := sc.Err(); err != nil {
				return fmt.Errorf("read calls: %w", err)
			}
			fmt.Printf("\n%d calls evaluated against %s\n", n, policyPath)
			return nil
		},
	}
	cmd.Flags().StringVar(&policyPath, "policy", "", "policy YAML file (required)")
	cmd.Flags().StringVar(&callsPath, "calls", "", "JSONL sample calls file (default: stdin)")
	return cmd
}

func newPolicyWhatIfCmd() *cobra.Command {
	var policyPath, task string
	cmd := &cobra.Command{
		Use:   "whatif <session.jsonl>",
		Short: "Re-evaluate a recorded session against a candidate policy",
		Long: `Loads a session log and re-evaluates every recorded tool call against
the given policy, then diffs the verdicts against what was recorded at
run time. Exits non-zero if any verdict would change — CI-ready policy
regression testing.

Note: re-evaluation runs on the recorded (redacted) arguments.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if policyPath == "" {
				return fmt.Errorf("--policy is required")
			}
			p, err := policy.Load(policyPath)
			if err != nil {
				return err
			}
			tapelog, err := replay.Load(args[0])
			if err != nil {
				return err
			}

			type call struct {
				order         int
				tool          string
				recordedVerdict, recordedRule string
				args          json.RawMessage
			}
			var calls []call
			for _, it := range tapelog.Interactions {
				calls = append(calls, call{it.Order, it.Tool, it.Verdict, it.RuleID, it.Args})
			}
			for _, it := range tapelog.Unanswered {
				calls = append(calls, call{it.Order, it.Tool, it.Verdict, it.RuleID, it.Args})
			}
			sort.Slice(calls, func(i, j int) bool { return calls[i].order < calls[j].order })

			// Sequence-aware what-if: walk the session in order, applying
			// flow rules (toxic-flow guards) with taint built up by calls
			// the candidate policy would have permitted.
			var taint policy.TaintState
			flowChecker, hasFlows := policy.Evaluator(p).(policy.FlowChecker)

			changed := 0
			for _, c := range calls {
				var dec policy.Decision
				decided := false
				if hasFlows {
					if fdec, applied := flowChecker.CheckFlow(&taint, c.tool); applied {
						dec, decided = fdec, true
					}
				}
				if !decided {
					dec = p.Evaluate(policy.Request{Tool: c.tool, Args: c.args, Task: task})
				}
				if dec.Verdict == policy.VerdictAllow {
					taint.Record(c.tool)
				}
				if string(dec.Verdict) == c.recordedVerdict {
					continue
				}
				changed++
				fmt.Printf("~ %s %s\n", c.tool, c.args)
				fmt.Printf("    recorded: %-7s (%s)\n", c.recordedVerdict, c.recordedRule)
				fmt.Printf("    what-if:  %-7s (%s) — %s\n", dec.Verdict, dec.RuleID, dec.Reason)
			}
			fmt.Printf("\n%d calls re-evaluated against %s: %d same, %d changed\n",
				len(calls), policyPath, len(calls)-changed, changed)
			if changed > 0 {
				return fmt.Errorf("%d verdict(s) would change", changed)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&policyPath, "policy", "", "candidate policy YAML file (required)")
	cmd.Flags().StringVar(&task, "task", "", "task label to evaluate with (for task-scoped rules)")
	return cmd
}

func newPolicyCompileCmd() *cobra.Command {
	var policyPath string
	cmd := &cobra.Command{
		Use:   "compile",
		Short: "Export a YAML policy as portable Cedar policy text",
		Long: `Renders the policy as Cedar source for interop with Cedar-aware
infrastructure. Note the documented approximations in docs/POLICY.md.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if policyPath == "" {
				return fmt.Errorf("--policy is required")
			}
			p, err := policy.Load(policyPath)
			if err != nil {
				return err
			}
			fmt.Print(policy.CompileCedar(p))
			return nil
		},
	}
	cmd.Flags().StringVar(&policyPath, "policy", "", "policy YAML file (required)")
	return cmd
}
