package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/Caseymccallum/tapelog/internal/checkpoint"
)

func newCheckpointCmd() *cobra.Command {
	var (
		seq        uint64
		outPath    string
		signer     string
		sshKeyPath string
		witness    string
		rekorURL   string
	)
	cmd := &cobra.Command{
		Use:   "checkpoint <session.jsonl>",
		Short: "Sign the session state (who) and witness it in a transparency log (when)",
		Long: `Emits a checkpoint: {session, seq, chain_head, created_at} plus a
signature and a transparency-log witness.

The signature proves WHO signed the claim (SSH key, or cosign keyless).
The transparency-log witness proves WHEN — it makes "this trajectory
existed in this exact form" true rather than merely signed. Verify with
` + "`tapelog verify --checkpoint <file>`" + `.

Writers:
  --signer ssh    --key <path>     SSHSIG over the statement (ssh-keygen
                                   -Y verify interoperable)
  --signer cosign                  cosign sign-blob (keyless or
                                   --cosign-key), bundle carries its
                                   own Rekor witness

Witness:
  --witness rekor   submit to the transparency log (default; offline
                    verification needs no network afterwards)
  --witness none    explicit opt-out — verify flags it loudly
                    ("signed but NOT witnessed — when unproven")

Periodic checkpoints for long sessions: repeat with --seq N. The
zero-dependency anchor stays ` + "`verify --expect`" + `.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if outPath == "" {
				outPath = args[0] + ".checkpoint.json"
			}
			stmt, err := checkpoint.NewStatement(args[0], seq)
			if err != nil {
				return err
			}
			ckpt := &checkpoint.Checkpoint{Statement: stmt}

			switch signer {
			case "ssh":
				if sshKeyPath == "" {
					return fmt.Errorf("--signer ssh requires --key <private-key-path>")
				}
				keyPEM, err := os.ReadFile(sshKeyPath)
				if err != nil {
					return fmt.Errorf("read ssh key: %w", err)
				}
				if err := ckpt.SignSSH(keyPEM); err != nil {
					return err
				}
			case "cosign":
				if err := signCosign(ckpt); err != nil {
					return err
				}
			default:
				return fmt.Errorf("unknown --signer %q (want ssh or cosign)", signer)
			}

			switch witness {
			case "none":
				ckpt.Witness.Type = "none"
				fmt.Fprintln(os.Stderr, "tapelog: WARNING — checkpoint will NOT be witnessed; anyone who rewrites the log can backdate their own claim")
			case "rekor":
				ctx, cancel := context.WithTimeout(cmd.Context(), 90*time.Second)
				defer cancel()
				if err := ckpt.WitnessRekor(ctx, rekorURL); err != nil {
					return err
				}
				fmt.Fprintf(os.Stderr, "tapelog: witnessed in %s (entry %s, index %d, integrated %d)\n",
					ckpt.Witness.URL, ckpt.Witness.EntryUUID, ckpt.Witness.LogIndex, ckpt.Witness.IntegratedTime)
			default:
				return fmt.Errorf("unknown --witness %q (want rekor or none)", witness)
			}

			if err := ckpt.Save(outPath); err != nil {
				return err
			}
			fmt.Printf("checkpoint written: %s\n  session: %s\n  seq: %d\n  chain head: %s\n  signer: %s (%s)\n  witness: %s\n",
				outPath, stmt.SessionID, stmt.Seq, stmt.ChainHead,
				ckpt.Signer.Type, ckpt.Signer.Identity, ckpt.Witness.Type)
			return nil
		},
	}
	cmd.Flags().Uint64Var(&seq, "seq", 0, "event to pin (0 = final event)")
	cmd.Flags().StringVar(&outPath, "out", "", "checkpoint output path (default <session>.checkpoint.json)")
	cmd.Flags().StringVar(&signer, "signer", "ssh", "signer: ssh | cosign")
	cmd.Flags().StringVar(&sshKeyPath, "key", "", "SSH private key path (--signer ssh)")
	cmd.Flags().StringVar(&witness, "witness", "rekor", "witness: rekor | none")
	cmd.Flags().StringVar(&rekorURL, "rekor-url", checkpoint.DefaultRekorURL, "transparency log URL")
	return cmd
}

// signCosign signs the statement via the cosign CLI; the resulting
// sigstore bundle is stored as the checkpoint signature and carries its
// own transparency witness (the tlog entry inside the bundle).
func signCosign(ckpt *checkpoint.Checkpoint) error {
	if _, err := exec.LookPath("cosign"); err != nil {
		return fmt.Errorf("cosign signer requires the cosign CLI (https://docs.sigstore.dev/cosign/system_config/installation/): %w", err)
	}
	dir, err := os.MkdirTemp("", "tapelog-ckpt-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	stmtPath := filepath.Join(dir, "statement")
	bundlePath := filepath.Join(dir, "bundle.json")
	if err := os.WriteFile(stmtPath, ckpt.CanonicalBytes(), 0o600); err != nil {
		return err
	}
	args := []string{"sign-blob", "--bundle", bundlePath, "--yes", stmtPath}
	cmd := exec.Command("cosign", args...)
	cmd.Stdin = os.Stdin
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("cosign sign-blob: %w", err)
	}
	bundle, err := os.ReadFile(bundlePath)
	if err != nil {
		return err
	}
	info, err := checkpoint.SignerInfoFromBundle(bundle)
	if err != nil {
		return err
	}
	ckpt.Signer = info
	ckpt.Signature = string(bundle)
	ckpt.Witness = checkpoint.Witness{Type: "cosign-bundle"}
	return nil
}
