package checkpoint

// Minimal Rekor (v1 API) witness client — submission plus FULLY OFFLINE
// verification. The "when" claim decomposes into four checks, all against
// the log's pinned public key (RekorPayload is the exact struct cosign
// signs in its SET; field order below is JCS sorted-key order):
//
//  1. entry binding: the stored entry body hashes the statement and
//     carries our exact signature + public key (no substitution)
//  2. SET: ECDSA over JCS({body, integratedTime, logID, logIndex}) — the
//     log vouches for this entry at this instant
//  3. RFC 6962 inclusion: the entry's leaf is in the tree whose root the
//     signed tree head covers
//  4. signed tree head (checkpoint note): ECDSA over the note text
//
// Wire facts pinned from primary sources (sigstore/cosign pkg/cosign/tlog.go,
// sigstore/sigstore-go pkg/tlog/entry.go, transparency-dev/merkle,
// sigstore/rekor pkg/types + pkg/util/signed_note.go).

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
)

// RekorPayload is the exact JSON body the signed entry timestamp covers,
// in JCS (RFC 8785) canonical form: keys sorted body < integratedTime <
// logID < logIndex.
type RekorPayload struct {
	Body           string `json:"body"` // base64 canonicalized entry body
	IntegratedTime int64  `json:"integratedTime"`
	LogID          string `json:"logID"` // hex
	LogIndex       int64  `json:"logIndex"`
}

// CanonicalBytes renders the JCS canonical form. All values are plain
// strings/int64 (base64 + hex + decimals need no exotic escaping), so
// marshaling each value and joining in sorted-key order is exact.
func (p RekorPayload) CanonicalBytes() ([]byte, error) {
	body, err := json.Marshal(p.Body)
	if err != nil {
		return nil, err
	}
	logID, err := json.Marshal(p.LogID)
	if err != nil {
		return nil, err
	}
	return fmt.Appendf(nil, `{"body":%s,"integratedTime":%d,"logID":%s,"logIndex":%d}`,
		body, p.IntegratedTime, logID, p.LogIndex), nil
}

// DefaultRekorURL is the public Sigstore transparency log.
const DefaultRekorURL = "https://rekor.sigstore.dev"

// DefaultRekorPublicKey is the public Rekor log's PKIX PEM, pinned as the
// default trust root for witness verification (fetched from
// /api/v1/log/publicKey). Override for other logs / key rotation via
// the --rekor-key flag.
const DefaultRekorPublicKey = `-----BEGIN PUBLIC KEY-----
MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAE2G2Y+2tabdTV5BcGiBIx0a9fAFwr
kBbmLSGtks4L3qX6yYY0zufBnhC8Ur/iy55GhWP/9A/bY2LhC30M9+RYtw==
-----END PUBLIC KEY-----`

// ParseLogPublicKey parses a Rekor log public key (PKIX PEM, ECDSA).
func ParseLogPublicKey(pemBytes []byte) (*ecdsa.PublicKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New("not a PEM block")
	}
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse log public key: %w", err)
	}
	ec, ok := pub.(*ecdsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("log public key is %T, want ECDSA", pub)
	}
	return ec, nil
}

// VerifySET checks the log's signed entry timestamp over the entry body
// (the "when" claim).
func VerifySET(logPub *ecdsa.PublicKey, w *Witness) error {
	if w.SignedEntryTimestamp == "" || w.Body == "" {
		return errors.New("witness missing SET or body")
	}
	payload := RekorPayload{
		Body:           w.Body,
		IntegratedTime: w.IntegratedTime,
		LogID:          w.LogID,
		LogIndex:       w.LogIndex,
	}
	msg, err := payload.CanonicalBytes()
	if err != nil {
		return err
	}
	set, err := base64.StdEncoding.DecodeString(w.SignedEntryTimestamp)
	if err != nil {
		return fmt.Errorf("decode SET: %w", err)
	}
	digest := sha256.Sum256(msg)
	if !ecdsa.VerifyASN1(logPub, digest[:], set) {
		return errors.New("SET verification failed (log did not vouch for this entry)")
	}
	return nil
}

// entryBinding is the parsed inner content of a Rekor `rekord` entry
// body: it must hash the checkpoint statement and carry the checkpoint's
// exact signature and signer key (no substitution).
type entryBinding struct {
	HashAlgo string `json:"algorithm"`
	HashHex  string `json:"value"`
}

// VerifyWitness runs the full offline witness chain for an ssh-signed
// checkpoint: entry binding → SET → RFC 6962 inclusion → signed tree
// head. logPKIX is the log public key in PKIX DER (for the note key hint).
func VerifyWitness(logPub *ecdsa.PublicKey, logPKIX []byte, w *Witness, statement, signature []byte, publicKey string) error {
	if w.Type != "rekor" {
		return fmt.Errorf("unknown witness type %q", w.Type)
	}
	body, err := base64.StdEncoding.DecodeString(w.Body)
	if err != nil {
		return fmt.Errorf("decode entry body: %w", err)
	}

	// 1. Entry binding: the stored rekord entry must reference the hash of
	// our exact statement bytes and carry our exact signature + key.
	if err := verifyEntryBinding(body, statement, signature, publicKey); err != nil {
		return err
	}

	// 2. SET: the log vouched for this entry body at IntegratedTime.
	if err := VerifySET(logPub, w); err != nil {
		return err
	}

	// 3+4. Inclusion proof against the signed tree head.
	if w.InclusionProof == nil {
		return errors.New("witness has no inclusion proof")
	}
	ip := w.InclusionProof
	leaf := hashLeaf(body)
	proof := make([][]byte, 0, len(ip.Hashes))
	for _, h := range ip.Hashes {
		hb, err := parseHexHash(h)
		if err != nil {
			return fmt.Errorf("inclusion proof hash: %w", err)
		}
		proof = append(proof, hb)
	}
	root, err := parseHexHash(ip.RootHash)
	if err != nil {
		return fmt.Errorf("proof root hash: %w", err)
	}
	if err := verifyInclusion(uint64(ip.LogIndex), uint64(ip.TreeSize), leaf, proof, root); err != nil {
		return fmt.Errorf("inclusion proof: %w", err)
	}

	// Signed tree head over the same root/size.
	sn, err := parseSignedNote(ip.Checkpoint)
	if err != nil {
		return err
	}
	if err := verifySignedNote(logPub, sn, logPKIX); err != nil {
		return err
	}
	if sn.TreeSize != ip.TreeSize || !bytes.Equal(sn.RootHash, root) {
		return fmt.Errorf("checkpoint note tree (%d, %x) does not match proof (%d, %x)",
			sn.TreeSize, sn.RootHash, ip.TreeSize, root)
	}
	return nil
}

// verifyEntryBinding checks the rekord entry body against the checkpoint
// statement + signature + key. The body is the log's canonicalized entry:
// {"spec":{"data":{"hash":...},"signature":{...}},"apiVersion":"0.0.1","kind":"rekord"}.
func verifyEntryBinding(body, statement, signature []byte, publicKey string) error {
	var entry struct {
		APIVersion string `json:"apiVersion"`
		Kind       string `json:"kind"`
		Spec       struct {
			Data struct {
				Hash *entryBinding `json:"hash"`
			} `json:"data"`
			Signature struct {
				Content   string `json:"content"`
				Format    string `json:"format"`
				PublicKey struct {
					Content string `json:"content"`
				} `json:"publicKey"`
			} `json:"signature"`
		} `json:"spec"`
	}
	if err := json.Unmarshal(body, &entry); err != nil {
		return fmt.Errorf("parse entry body: %w", err)
	}
	if entry.Kind != "rekord" {
		return fmt.Errorf("entry kind %q, want rekord", entry.Kind)
	}
	if entry.Spec.Data.Hash == nil {
		return errors.New("entry body has no data.hash")
	}
	sum := sha256.Sum256(statement)
	if entry.Spec.Data.Hash.HashHex != hex.EncodeToString(sum[:]) {
		return errors.New("entry hashes a different statement (checkpoint substitution)")
	}
	sig, err := base64.StdEncoding.DecodeString(entry.Spec.Signature.Content)
	if err != nil {
		return fmt.Errorf("decode entry signature: %w", err)
	}
	if !bytes.Equal(sig, signature) {
		return errors.New("entry carries a different signature than the checkpoint")
	}
	key, err := base64.StdEncoding.DecodeString(entry.Spec.Signature.PublicKey.Content)
	if err != nil {
		return fmt.Errorf("decode entry public key: %w", err)
	}
	if strings.TrimSpace(string(key)) != strings.TrimSpace(publicKey) {
		return errors.New("entry carries a different public key than the checkpoint")
	}
	return nil
}
