package checkpoint

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/pem"
	"errors"
	"fmt"
	"hash"

	"golang.org/x/crypto/ssh"
)

// SSHSIG implementation per PROTOCOL.sshsig (OpenSSH) and Rekor's
// pkg/pki/ssh — same wire layout, so `ssh-keygen -Y verify` interops:
//
//	blob:      "SSHSIG" | uint32 1 | string pubkey | string namespace |
//	           string reserved | string hash_algorithm | string signature
//	signed:    "SSHSIG" | string namespace | string reserved |
//	           string hash_algorithm | string H(message)
//
// The namespace is "file" (OpenSSH's file-signing domain — the statement
// is a self-describing file whose first line names tapelog, so the domain
// cannot be confused with a host/user auth signature).
const (
	sshsigMagic   = "SSHSIG"
	sshsigVersion = 1
	sshsigNS      = "file"
	sshsigPEMType = "SSH SIGNATURE"
)

type sshSigBlob struct {
	MagicHeader   [6]byte
	Version       uint32
	PublicKey     string
	Namespace     string
	Reserved      string
	HashAlgorithm string
	Signature     string
}

type sshSigMessage struct {
	Namespace     string
	Reserved      string
	HashAlgorithm string
	Hash          string
}

var sshSigHashes = map[string]func() hash.Hash{
	"sha256": sha256.New,
	"sha512": sha512.New,
}

// SignSSHSIG signs message with an OpenSSH private key and returns the
// armored SSHSIG block plus the authorized_keys line of the signer.
func SignSSHSIG(privateKeyPEM, message []byte) (armor []byte, authorizedKey string, err error) {
	signer, err := ssh.ParsePrivateKey(privateKeyPEM)
	if err != nil {
		return nil, "", fmt.Errorf("parse ssh private key: %w", err)
	}
	alg, ok := signer.(ssh.AlgorithmSigner)
	if !ok {
		return nil, "", errors.New("ssh key does not support algorithm signing")
	}

	h := sha512.New()
	h.Write(message)
	digest := h.Sum(nil)
	wrapped := sshSigMessage{Namespace: sshsigNS, HashAlgorithm: "sha512", Hash: string(digest)}
	preimage := append([]byte(sshsigMagic), ssh.Marshal(wrapped)...)

	sigAlgo := ""
	if signer.PublicKey().Type() == ssh.KeyAlgoRSA {
		sigAlgo = ssh.KeyAlgoRSASHA256
	}
	sig, err := alg.SignWithAlgorithm(rand.Reader, preimage, sigAlgo)
	if err != nil {
		return nil, "", fmt.Errorf("ssh sign: %w", err)
	}

	blob := sshSigBlob{
		Version:       sshsigVersion,
		PublicKey:     string(signer.PublicKey().Marshal()),
		Namespace:     sshsigNS,
		HashAlgorithm: "sha512",
		Signature:     string(ssh.Marshal(sig)),
	}
	copy(blob.MagicHeader[:], sshsigMagic)

	armor = pem.EncodeToMemory(&pem.Block{Type: sshsigPEMType, Bytes: ssh.Marshal(blob)})
	return armor, string(ssh.MarshalAuthorizedKey(signer.PublicKey())), nil
}

// VerifySSHSIG checks an armored SSHSIG over message against an
// authorized_keys line. The armored block's embedded public key must be
// the expected key (no substitution).
func VerifySSHSIG(armor, message []byte, authorizedKey string) error {
	block, _ := pem.Decode(armor)
	if block == nil {
		return errors.New("not a PEM block")
	}
	if block.Type != sshsigPEMType {
		return fmt.Errorf("wrong PEM type %q, want %q", block.Type, sshsigPEMType)
	}
	var blob sshSigBlob
	if err := ssh.Unmarshal(block.Bytes, &blob); err != nil {
		return fmt.Errorf("decode SSHSIG blob: %w", err)
	}
	if string(blob.MagicHeader[:]) != sshsigMagic {
		return errors.New("bad SSHSIG magic")
	}
	if blob.Version != sshsigVersion {
		return fmt.Errorf("unsupported SSHSIG version %d", blob.Version)
	}
	if blob.Namespace != sshsigNS {
		return fmt.Errorf("unexpected SSHSIG namespace %q", blob.Namespace)
	}
	newHash, ok := sshSigHashes[blob.HashAlgorithm]
	if !ok {
		return fmt.Errorf("unsupported SSHSIG hash algorithm %q", blob.HashAlgorithm)
	}

	embedded, err := ssh.ParsePublicKey([]byte(blob.PublicKey))
	if err != nil {
		return fmt.Errorf("parse embedded public key: %w", err)
	}
	expected, _, _, _, err := ssh.ParseAuthorizedKey([]byte(authorizedKey))
	if err != nil {
		return fmt.Errorf("parse expected public key: %w", err)
	}
	if string(embedded.Marshal()) != string(expected.Marshal()) {
		return errors.New("SSHSIG signed by a different key than expected")
	}

	var inner ssh.Signature
	if err := ssh.Unmarshal([]byte(blob.Signature), &inner); err != nil {
		return fmt.Errorf("decode inner signature: %w", err)
	}
	h := newHash()
	h.Write(message)
	wrapped := sshSigMessage{Namespace: sshsigNS, Reserved: blob.Reserved,
		HashAlgorithm: blob.HashAlgorithm, Hash: string(h.Sum(nil))}
	preimage := append([]byte(sshsigMagic), ssh.Marshal(wrapped)...)
	if err := embedded.Verify(preimage, &inner); err != nil {
		return fmt.Errorf("SSHSIG verification failed: %w", err)
	}
	return nil
}

// SSHFingerprint returns the SHA256 fingerprint of an authorized_keys line.
func SSHFingerprint(authorizedKey string) (string, error) {
	pub, _, _, _, err := ssh.ParseAuthorizedKey([]byte(authorizedKey))
	if err != nil {
		return "", err
	}
	return ssh.FingerprintSHA256(pub), nil
}
