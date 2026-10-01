package checkpoint

// Signed tree head ("checkpoint") verification — the signed-note format
// Rekor serves in LogInfo.signedTreeHead and InclusionProof.checkpoint:
//
//	rekor.sigstore.dev - <treeID>\n
//	<treeSize>\n
//	<base64 rootHash>\n
//	\n
//	— <key name> <base64(4-byte keyhint || ECDSA sig)>\n
//
// The signature covers the note text (including its trailing newline)
// via SHA-256 — per sigstore/rekor pkg/util/signed_note.go.

import (
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
)

// signedNote is a parsed checkpoint note.
type signedNote struct {
	Text      string // exactly the signed bytes
	Origin    string // e.g. "rekor.sigstore.dev - 3904496407287907110"
	TreeSize  int64
	RootHash  []byte // decoded from base64 in the note
	SigName   string
	Signature []byte // raw ECDSA signature
	SigHint   uint32 // 4-byte key hint preceding the signature
}

// parseSignedNote splits a checkpoint note into text and signatures and
// decodes the tree parameters. Signature line layout (signed_note.go):
// "— <name> <base64(4-byte hint || sig)>".
func parseSignedNote(note string) (*signedNote, error) {
	drop := strings.LastIndex(note, "\n\n")
	if drop < 0 {
		return nil, errors.New("malformed checkpoint note: no signature block")
	}
	text, sigBlock := note[:drop+1], note[drop+2:]
	sn := &signedNote{Text: text}

	for _, line := range strings.Split(strings.TrimRight(sigBlock, "\n"), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 3 || fields[0] != "\u2014" { // em dash
			continue
		}
		sn.SigName = fields[1]
		raw, err := base64.StdEncoding.DecodeString(fields[2])
		if err != nil {
			return nil, fmt.Errorf("decode signature: %w", err)
		}
		if len(raw) < 5 {
			return nil, errors.New("signature too short")
		}
		// First 4 bytes are a key hint (not covered); the rest is the sig.
		sn.SigHint = binary.BigEndian.Uint32(raw[:4])
		sn.Signature = raw[4:]
		break
	}
	if sn.Signature == nil {
		return nil, errors.New("checkpoint note carries no signature")
	}

	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(lines) != 3 {
		return nil, fmt.Errorf("checkpoint note has %d lines, want 3 (origin/size/root)", len(lines))
	}
	sn.Origin = lines[0]
	var err error
	if sn.TreeSize, err = parseInt64(lines[1]); err != nil {
		return nil, fmt.Errorf("checkpoint tree size: %w", err)
	}
	if sn.RootHash, err = base64.StdEncoding.DecodeString(lines[2]); err != nil {
		return nil, fmt.Errorf("checkpoint root hash: %w", err)
	}
	return sn, nil
}

func parseInt64(s string) (int64, error) {
	var n int64
	_, err := fmt.Sscanf(strings.TrimSpace(s), "%d", &n)
	return n, err
}

// verifySignedNote checks the log's signature over the note text and the
// key hint (cosmetic cross-check; the crypto check is the signature).
func verifySignedNote(logPub *ecdsa.PublicKey, sn *signedNote, logPKIX []byte) error {
	if want := keyHint(logPKIX); sn.SigHint != want {
		return fmt.Errorf("checkpoint signed by key hint %08x, expected %08x", sn.SigHint, want)
	}
	digest := sha256.Sum256([]byte(sn.Text))
	if !ecdsa.VerifyASN1(logPub, digest[:], sn.Signature) {
		return fmt.Errorf("checkpoint note signature invalid (signed by %q)", sn.SigName)
	}
	return nil
}

// keyHint matches a signature's 4-byte hint to a public key (cosmetic
// cross-check; the crypto check is the signature itself).
func keyHint(pkix []byte) uint32 {
	sum := sha256.Sum256(pkix)
	return binary.BigEndian.Uint32(sum[:4])
}
