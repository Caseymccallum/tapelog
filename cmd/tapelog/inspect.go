package main

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Caseymccallum/tapelog/internal/tui"
)

// Exit-code sentinels for `inspect --check`. main maps them to 3 and 4.
var (
	errIntegrityFailed     = errors.New("session log failed integrity verification")
	errStructureFailed     = errors.New("session log is not one valid session")
	errIncompleteStructure = errors.New("session log has no observed session/end")
)

func newInspectCmd() *cobra.Command {
	var plain, summary, check bool
	cmd := &cobra.Command{
		Use:   "inspect <session.jsonl>",
		Short: "Inspect a session log (interactive TUI)",
		Long: `Two-pane session viewer: the event timeline on the left (allow/deny
verdicts color-coded, drift flagged), the full event payload on the right.

The overview reports four separate verdicts: cryptographic integrity (hash
chain), structural validity (one session id, one start first, any end last),
completeness (was a session/end observed) and eligibility (not assessed here).
inspect renders any file it can parse; a file whose JSON cannot be parsed is
reported on stderr and exits 1, because there is nothing to render. For a
parseable file, --check makes a failed verdict exit non-zero: 3 integrity,
4 structure invalid, 5 incomplete.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if plain || summary {
				items, sum, story, err := tui.Load(args[0])
				if err != nil {
					return err
				}
				if summary {
					fmt.Fprint(cmd.OutOrStdout(), tui.Overview(sum))
				} else {
					fmt.Fprint(cmd.OutOrStdout(), tui.Plain(items, sum, story))
				}
				return checkVerdict(sum, check)
			}
			if check {
				_, sum, _, err := tui.Load(args[0])
				if err != nil {
					return err
				}
				if err := checkVerdict(sum, true); err != nil {
					return err
				}
			}
			return tui.Inspect(args[0])
		},
	}
	cmd.Flags().BoolVar(&plain, "plain", false, "print a plain listing instead of the TUI")
	cmd.Flags().BoolVar(&summary, "summary", false, "show observation counts, scope and next steps without the timeline")
	cmd.Flags().BoolVar(&check, "check", false, "exit non-zero when a verdict fails (3 integrity, 4 structure invalid, 5 incomplete)")
	return cmd
}

// checkVerdict turns the reported verdicts into exit codes when --check is
// set. inspect renders first regardless: refusing to open tampered evidence
// would defeat a forensic viewer. An unparseable file never reaches this
// function — Load returns an error and the command exits 1.
func checkVerdict(sum tui.Summary, check bool) error {
	if !check {
		return nil
	}
	if sum.Integrity != "verified" {
		return errIntegrityFailed
	}
	if sum.Structure == "incomplete" {
		return errIncompleteStructure
	}
	if sum.Structure != "valid" {
		return errStructureFailed
	}
	return nil
}
