package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Caseymccallum/tapelog/internal/checkpoint"
	"github.com/Caseymccallum/tapelog/internal/session"
)

func newVerifyCmd() *cobra.Command {
	var expect string
	var checkpointPath string
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
truncation, which an internal chain check alone cannot.

--checkpoint <file> verifies against a signed checkpoint instead: the
checkpoint's signature proves WHO attested the head and its transparency
witness proves WHEN — making "this trajectory existed in this exact form"
true rather than merely signed.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := session.VerifyFile(args[0])
			if err != nil {
				return err
			}
			if checkpointPath != "" {
				if err := verifyAgainstCheckpoint(checkpointPath, args[0], res); err != nil {
					fmt.Printf("FAILED: %s\n", args[0])
					fmt.Printf("  checkpoint: %v\n", err)
					return fmt.Errorf("session log failed checkpoint verification")
				}
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
			if checkpointPath != "" {
				ckpt, _ := checkpoint.Load(checkpointPath)
				if ckpt != nil {
					fmt.Printf("  checkpoint: seq %d, signed by %s (%s), witnessed by %s\n",
						ckpt.Seq, ckpt.Signer.Type, ckpt.Signer.Identity, ckpt.Witness.Type)
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&expect, "expect", "", "expected chain head (last event hash) recorded outside the log; detects whole-log rewrites and truncation")
	cmd.Flags().StringVar(&checkpointPath, "checkpoint", "", "signed checkpoint file (tapelog checkpoint) — verifies who attested the head and when")
	return cmd
}

// verifyAgainstCheckpoint checks the session log's chain against a
// signed checkpoint: full chain, head at the checkpointed seq, signature,
// and transparency witness.
func verifyAgainstCheckpoint(ckptPath, logPath string, res *session.VerifyResult) error {
	ckpt, err := checkpoint.Load(ckptPath)
	if err != nil {
		return err
	}
	// 1. Signature + witness (who / when).
	wit, err := ckpt.Verify(checkpoint.VerifyOptions{})
	if err != nil {
		return err
	}
	if wit == "none" {
		fmt.Println("  ⚠ WARNING: checkpoint is signed but NOT witnessed — the 'when' is UNPROVEN")
		fmt.Println("    (anyone with the signing key could have backdated this; re-checkpoint with --witness rekor)")
	}
	// 2. The log must contain the checkpointed event and its hash must be
	// the attested chain head (the log may legitimately continue past it).
	head, err := session.HashAt(logPath, ckpt.Seq)
	if err != nil {
		return err
	}
	if head != ckpt.ChainHead {
		return fmt.Errorf("log's chain head at seq %d is %s, checkpoint attested %s", ckpt.Seq, head, ckpt.ChainHead)
	}
	if res.SessionID != "" && ckpt.SessionID != res.SessionID {
		return fmt.Errorf("checkpoint attests session %q, log is %q", ckpt.SessionID, res.SessionID)
	}
	return nil
}
