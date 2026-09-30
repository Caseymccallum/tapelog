package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Caseymccallum/tapelog/internal/tui"
)

func newInspectCmd() *cobra.Command {
	var plain bool
	cmd := &cobra.Command{
		Use:   "inspect <session.jsonl>",
		Short: "Inspect a session log (interactive TUI)",
		Long: `Two-pane session viewer: the event timeline on the left (allow/deny
verdicts color-coded, drift flagged), the full event payload on the right.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if plain {
				items, sum, err := tui.Load(args[0])
				if err != nil {
					return err
				}
				fmt.Print(tui.Plain(items, sum))
				return nil
			}
			return tui.Inspect(args[0])
		},
	}
	cmd.Flags().BoolVar(&plain, "plain", false, "print a plain listing instead of the TUI")
	return cmd
}
