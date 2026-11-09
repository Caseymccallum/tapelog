package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/Caseymccallum/tapelog/internal/checkpoint"
	"github.com/Caseymccallum/tapelog/internal/session"
)

// errStructureRequired is returned when --require-structure is set and the
// chain is intact but the file is not one valid complete session log. main
// maps it to exit code 3. The wrapped message names the specific defect.
var errStructureRequired = errors.New("session log failed the structure check")

// errChainFailed is returned when the hash chain does not verify.
var errChainFailed = errors.New("session log failed verification")

// structureVerdict renders the structural verdict for verify. kind is "" for
// a valid file, "invalid" for a structural defect and "incomplete" for a
// missing end; bad is true for either defect. A missing end is reported as
// INCOMPLETE, never as invalid, so an open recording keeps its vocabulary.
func structureVerdict(data []byte) (line, kind string, bad bool) {
	structure, err := session.InspectStructure(bytes.NewReader(data))
	if err != nil {
		return "UNREADABLE", "invalid", true
	}
	p := structure.FirstProblem()
	if structure.Valid() && structure.Complete() {
		return "VALID", "", false
	}
	if !structure.Valid() {
		if p == nil {
			return "INVALID", "invalid", true
		}
		return "INVALID — " + p.Describe(), "invalid", true
	}
	if p == nil {
		return "INCOMPLETE", "incomplete", true
	}
	return "INCOMPLETE — " + p.Describe(), "incomplete", true
}

func newVerifyCmd() *cobra.Command {
	var expect string
	var checkpointPath string
	var requireStructure bool
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
			data, err := os.ReadFile(args[0])
			if err != nil {
				return fmt.Errorf("open session log: %w", err)
			}
			res, err := session.Verify(bytes.NewReader(data))
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
				if res.FirstBadSeq > 0 {
					fmt.Printf("  verified %d events before the problem\n", res.FirstBadSeq-1)
				} else {
					fmt.Printf("  the first event is already invalid\n")
				}
				return errChainFailed
			}
			// Out-of-band payloads (spec §6.2): the chain binds the digests,
			// so verify re-hashes every referenced blob when a store exists.
			refs, problems, warnings, err := session.VerifyBlobs(args[0], session.NewBlobStore(session.DefaultBlobDir(args[0])))
			if err != nil {
				return err
			}
			if len(problems) > 0 {
				fmt.Printf("FAILED: %s\n  chain intact but %d of %d blob reference(s) fail:\n", args[0], len(problems), refs)
				for _, p := range problems {
					fmt.Printf("  - %s\n", p)
				}
				return fmt.Errorf("session log failed blob verification")
			}
			structLine, structKind, structBad := structureVerdict(data)
			if requireStructure && structBad {
				fmt.Printf("FAILED: %s\n", args[0])
				if structKind == "incomplete" {
					fmt.Printf("  chain intact but the recording is incomplete: %s\n", structLine)
					fmt.Printf("  a missing session/end is a completeness defect, not a structural one\n")
				} else {
					fmt.Printf("  chain intact but the session structure is invalid: %s\n", structLine)
					fmt.Printf("  chain verification says nothing about session structure\n")
				}
				return fmt.Errorf("%w: %s", errStructureRequired, structLine)
			}
			if res.Events == 0 {
				fmt.Printf("OK: %s\n  0 events — the file is empty; there is no chain to verify\n", args[0])
			} else {
				fmt.Printf("OK: %s\n  %d events, chain intact\n  chain head: %s\n", args[0], res.Events, res.LastHash)
			}
			fmt.Printf("  Structure: %s\n", structLine)
			for _, w := range warnings {
				fmt.Printf("  warning: %s\n", w)
			}
			if refs > 0 {
				fmt.Printf("  %d blob reference(s), all digests verified\n", refs)
			}
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
	cmd.Flags().BoolVar(&requireStructure, "require-structure", false, "exit 3 when the chain is intact but the file is not one valid complete session log")
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
