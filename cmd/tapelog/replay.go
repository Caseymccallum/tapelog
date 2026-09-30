package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/Caseymccallum/tapelog/internal/replay"
)

func newReplayCmd() *cobra.Command {
	var match string
	var strict bool
	cmd := &cobra.Command{
		Use:   "replay <session.jsonl>",
		Short: "Replay a recorded session as a hermetic MCP server",
		Long: `Serves recorded tool traffic over stdio: any MCP client (e.g. an agent
harness in CI) can connect and receive exactly the recorded responses.

Matching is done on redacted canonical arguments (--match exact|subset|tool).
Each recording is consumed once; calls without a matching recording are
answered with error -32011 (fail-loud) and counted.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode, err := replay.ParseMatchMode(match)
			if err != nil {
				return err
			}
			tapelog, err := replay.Load(args[0])
			if err != nil {
				return err
			}
			player := replay.NewPlayer(tapelog, mode)
			server := &replay.Server{Player: player, Version: version}

			fmt.Fprintf(os.Stderr, "tapelog: replaying %s (session %s, %d interactions, match=%s)\n",
				args[0], tapelog.SessionID, len(tapelog.Interactions), mode)

			serveErr := server.Serve(context.Background(), os.Stdin, os.Stdout)

			played, unused, misses := player.Stats()
			fmt.Fprintf(os.Stderr, "\ntapelog: replay summary: %d played, %d missed, %d unused recordings\n",
				played, len(misses), unused)
			for _, m := range misses {
				fmt.Fprintf(os.Stderr, "  MISS %s %s\n", m.Tool, m.Args)
			}
			if strict && len(misses) > 0 {
				return fmt.Errorf("strict mode: %d calls had no recording", len(misses))
			}
			return serveErr
		},
	}
	cmd.Flags().StringVar(&match, "match", "exact", "argument matching: exact|subset|tool")
	cmd.Flags().BoolVar(&strict, "strict", false, "exit non-zero if any call had no recording")
	return cmd
}
