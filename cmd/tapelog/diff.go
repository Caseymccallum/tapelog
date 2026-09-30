package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Caseymccallum/tapelog/internal/replay"
)

func newDiffCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "diff <session-a.jsonl> <session-b.jsonl>",
		Short: "Compare two recorded sessions",
		Long: `Compares the tool-call sequences and outcomes of two tapelogs —
e.g. a fresh live run against a replay, or a run before and after a change.
Exits non-zero when the sessions differ.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := replay.Load(args[0])
			if err != nil {
				return err
			}
			b, err := replay.Load(args[1])
			if err != nil {
				return err
			}
			res := replay.Diff(a, b)

			fmt.Printf("--- %s (%d interactions)\n+++ %s (%d interactions)\n",
				args[0], len(a.Interactions), args[1], len(b.Interactions))
			for _, k := range res.OnlyInA {
				fmt.Printf("  - %s\n", k)
			}
			for _, k := range res.OnlyInB {
				fmt.Printf("  + %s\n", k)
			}
			for _, k := range res.Changed {
				fmt.Printf("  ~ %s  (same call, different result)\n", k)
			}
			if res.OrderDiffers {
				fmt.Println("  * call order differs")
			}
			if res.Same {
				fmt.Println("sessions are equivalent")
				return nil
			}
			return fmt.Errorf("sessions differ")
		},
	}
}
