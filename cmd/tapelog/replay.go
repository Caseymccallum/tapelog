package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Caseymccallum/tapelog/internal/replay"
	"github.com/Caseymccallum/tapelog/internal/session"
)

func newReplayCmd() *cobra.Command {
	var match string
	var strict bool
	var allowUnverified bool
	var allowInvalidStructure bool
	var allowAmbiguousIDs bool
	cmd := &cobra.Command{
		Use:   "replay <session.jsonl>",
		Short: "Replay a recorded session as a hermetic MCP server",
		Long: `Serves recorded tool traffic over stdio: any MCP client (e.g. an agent
harness in CI) can connect and receive exactly the recorded responses.

The session's hash chain is verified first: replay will not quietly
serve an altered cassette as "the recorded session" (use
--allow-unverified to replay anyway, e.g. for tamper-testing workflows).
The file must also be one complete valid session log — one session id,
session/start first, session/end last (use --allow-invalid-structure to
replay anyway).

Matching is done on redacted canonical arguments (--match exact|subset|tool).
Each recording is consumed once; calls without a matching recording are
answered with error -32011 (fail-loud) and counted.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode, err := replay.ParseMatchMode(match)
			if err != nil {
				return err
			}
			tapelog, chain, err := replay.LoadVerifiedAttributable(args[0], allowUnverified, allowInvalidStructure, allowAmbiguousIDs)
			if err != nil {
				return err
			}
			if chain != nil && !chain.OK() {
				fmt.Fprintf(os.Stderr, "tapelog: WARNING — replaying an UNVERIFIED log (first bad event: seq %d: %s); this is not the recorded session\n",
					chain.FirstBadSeq, chain.Problem)
			}
			// An override must be visible, not only typed. --allow-unverified says
			// so on stderr; --allow-invalid-structure used to silence the refusal
			// and then serve the file with no marker at all (an empty file even
			// printed an empty session id), which reads as a normal replay.
			if allowInvalidStructure {
				if f, oerr := os.Open(args[0]); oerr == nil {
					sr, serr := session.InspectStructure(f)
					_ = f.Close()
					if serr == nil && (!sr.Valid() || !sr.Complete()) {
						problem := "not one complete valid session"
						if p := sr.FirstProblem(); p != nil {
							problem = p.Describe()
						}
						fmt.Fprintf(os.Stderr, "tapelog: WARNING — replaying evidence that is NOT one complete valid session (%s); "+
							"serve it only as a forensic double, never as the recorded session\n", problem)
					}
				}
			}
			// --allow-ambiguous-ids used to be the one override that stayed silent:
			// the refusal says replay "will not present a guess as the recorded
			// outcome", and the flag exists to permit exactly that guess, so it has
			// to say so on stderr like its two siblings.
			if allowAmbiguousIDs && len(tapelog.AmbiguousIDs) > 0 {
				fmt.Fprintf(os.Stderr, "tapelog: WARNING — replaying evidence with ambiguous request id(s) %s: "+
					"the transport cannot say which call a result answered, so served answers may be paired with the "+
					"wrong call; this is not the recorded pairing\n",
					strings.Join(tapelog.AmbiguousIDs, ", "))
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
	cmd.Flags().BoolVar(&allowUnverified, "allow-unverified", false, "replay even if the hash chain fails verification (tamper-testing escape hatch; default refuses altered evidence)")
	cmd.Flags().BoolVar(&allowInvalidStructure, "allow-invalid-structure", false, "replay even if the file is not one complete valid session log (missing start/end, mixed session ids)")
	cmd.Flags().BoolVar(&allowAmbiguousIDs, "allow-ambiguous-ids", false, "replay even if a request id was shared by more than one outstanding call, so a result cannot be tied to one of them")
	return cmd
}
