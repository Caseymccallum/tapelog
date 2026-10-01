package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Caseymccallum/tapelog/internal/session"
)

func newVerifyCmd() *cobra.Command {
	var expect string
	cmd := &cobra.Command{
		Use:   "verify <session.jsonl>",
		Short: "Verify the hash chain of a session log",
		Long: `Recomputes the hash chain of a session log and reports the first
sequence number where the log was modified, deleted, or reordered.

The chain alone anchors at an empty prev_hash — a determined attacker who
rewrites the ENTIRE log and recomputes every hash produces a self-consistent
file. To close that gap, record the chain head (printed at the end of each
run) somewhere the attacker cannot rewrite — a CI log, a ticket, a chat
message — and pass it back with --expect. --expect also catches tail
truncation, which an internal chain check alone cannot.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := session.VerifyFile(args[0])
			if err != nil {
				return err
			}
			if res.OK() && expect != "" && res.LastHash != expect {
				fmt.Printf("FAILED: %s\n", args[0])
				fmt.Printf("  chain is internally consistent but the head does not match --expect\n")
				fmt.Printf("  chain head: %s\n  expected:   %s\n", res.LastHash, expect)
				fmt.Printf("  the log was rewritten wholesale or truncated after you recorded the head\n")
				return fmt.Errorf("session log failed verification (chain head mismatch)")
			}
			if !res.OK() {
				fmt.Printf("FAILED: %s\n", args[0])
				fmt.Printf("  first bad event: seq %d\n  problem: %s\n", res.FirstBadSeq, res.Problem)
				fmt.Printf("  verified %d events before the problem\n", res.FirstBadSeq-1)
				return fmt.Errorf("session log failed verification")
			}
			fmt.Printf("OK: %s\n  %d events, chain intact\n  chain head: %s\n", args[0], res.Events, res.LastHash)
			return nil
		},
	}
	cmd.Flags().StringVar(&expect, "expect", "", "expected chain head (last event hash) recorded outside the log; detects whole-log rewrites and truncation")
	return cmd
}
