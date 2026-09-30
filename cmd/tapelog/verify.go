package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Caseymccallum/tapelog/internal/session"
)

func newVerifyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "verify <session.jsonl>",
		Short: "Verify the hash chain of a session log",
		Long: `Recomputes the hash chain of a session log and reports the first
sequence number where the log was modified, deleted, or reordered.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := session.VerifyFile(args[0])
			if err != nil {
				return err
			}
			if !res.OK() {
				fmt.Printf("FAILED: %s\n", args[0])
				fmt.Printf("  first bad event: seq %d\n  problem: %s\n", res.FirstBadSeq, res.Problem)
				fmt.Printf("  verified %d events before the problem\n", res.FirstBadSeq-1)
				return fmt.Errorf("session log failed verification")
			}
			fmt.Printf("OK: %s\n  %d events, chain intact\n", args[0], res.Events)
			return nil
		},
	}
}
