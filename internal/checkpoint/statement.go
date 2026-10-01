// Package checkpoint implements tapelog's signed session checkpoints
// (docs/ROADMAP.md v0.4 "Trusted evidence"): a checkpoint pins
// {session, seq, chain_head, created_at} with a signature (proves WHO)
// and a transparency-log witness (proves WHEN — what makes "this
// trajectory existed in this exact form" true rather than merely signed).
//
// Signers:
//   - ssh: SSHSIG signature (PROTOCOL.sshsig, namespace "file") over the
//     canonical statement — interoperable with `ssh-keygen -Y verify`
//   - cosign: cosign sign-blob subprocess (keyless or key-based); the
//     sigstore bundle carries its own Rekor witness
//
// Witness: Rekor (https://rekor.sigstore.dev by default). For ssh-signed
// checkpoints a `rekord` entry is submitted carrying the statement, the
// signature, and the public key; verification is fully offline (entry
// binding + signed entry timestamp + RFC 6962 inclusion proof + signed
// tree head) against the log's pinned public key. `--witness none` is an
// explicit opt-out that `verify` flags loudly: signed but NOT witnessed
// — the "when" is unproven.
//
// `verify --expect` remains the zero-dependency anchor; checkpoints are
// the stronger, self-describing artifact.
package checkpoint

import (
	"fmt"
)

// CurrentVersion is the checkpoint statement format version.
const CurrentVersion = 1

// Statement is the signed claim: at created_at, session SessionID's hash
// chain had head ChainHead after event Seq. A checkpoint may pin a
// mid-session prefix (Seq < final event) — periodic checkpoints for long
// sessions.
type Statement struct {
	Version   int    `json:"version"`
	SessionID string `json:"session_id"`
	Seq       uint64 `json:"seq"`
	ChainHead string `json:"chain_head"`
	CreatedAt string `json:"created_at"`
}

// CanonicalBytes returns the exact ASCII bytes signed and witnessed.
// Layout mirrors the session-log canonical form (spec/session-log-v0.md
// §4): one field per line, self-describing first line so a checkpoint
// signature can never be confused with an arbitrary file signature.
func (s Statement) CanonicalBytes() []byte {
	return []byte(fmt.Sprintf(
		"tapelog-checkpoint-v%d\nsession_id=%s\nseq=%d\nchain_head=%s\ncreated_at=%s\n",
		s.Version, s.SessionID, s.Seq, s.ChainHead, s.CreatedAt))
}
