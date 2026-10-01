package checkpoint

// Sign/verify orchestration over the two signers and the witness.

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// hashHex returns the hex SHA-256 of b.
func hashHex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// SignSSH signs the statement with an SSH private key and fills the
// checkpoint's signer + signature fields.
func (c *Checkpoint) SignSSH(privateKeyPEM []byte) error {
	armor, authorizedKey, err := SignSSHSIG(privateKeyPEM, c.CanonicalBytes())
	if err != nil {
		return err
	}
	fp, err := SSHFingerprint(authorizedKey)
	if err != nil {
		return err
	}
	c.Signer = SignerInfo{Type: "ssh", Identity: fp, PublicKey: strings.TrimSpace(authorizedKey)}
	c.Signature = string(armor)
	return nil
}

// WitnessRekor submits the signed checkpoint to the transparency log.
func (c *Checkpoint) WitnessRekor(ctx context.Context, url string) error {
	if c.Signer.Type != "ssh" {
		return fmt.Errorf("rekor witness supports the ssh signer (got %q); cosign bundles carry their own witness", c.Signer.Type)
	}
	w, err := SubmitRekor(ctx, url, c.CanonicalBytes(), []byte(c.Signature), c.Signer.PublicKey)
	if err != nil {
		return err
	}
	c.Witness = *w
	return nil
}

// SignerInfoFromBundle extracts the signing identity from a cosign
// sigstore bundle: the Fulcio certificate (stored as the public key) and
// its SAN + OIDC issuer identity.
func SignerInfoFromBundle(bundle []byte) (SignerInfo, error) {
	var b struct {
		VerificationMaterial struct {
			Certificate struct {
				RawBytes string `json:"rawBytes"`
			} `json:"certificate"`
		} `json:"verificationMaterial"`
	}
	if err := json.Unmarshal(bundle, &b); err != nil {
		return SignerInfo{}, fmt.Errorf("parse cosign bundle: %w", err)
	}
	der, err := base64.StdEncoding.DecodeString(b.VerificationMaterial.Certificate.RawBytes)
	if err != nil || len(der) == 0 {
		return SignerInfo{}, fmt.Errorf("bundle has no certificate (key-based cosign? store the public key out-of-band)")
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return SignerInfo{}, fmt.Errorf("parse bundle certificate: %w", err)
	}
	info := SignerInfo{
		Type:      "cosign",
		PublicKey: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
	}
	if len(cert.EmailAddresses) > 0 {
		info.Identity = cert.EmailAddresses[0]
	} else if len(cert.URIs) > 0 {
		info.Identity = cert.URIs[0].String()
	}
	// Fulcio OIDC issuer extension (1.3.6.1.4.1.57264.1.1), ASCII.
	for _, ext := range cert.Extensions {
		if ext.Id.String() == "1.3.6.1.4.1.57264.1.1" {
			info.OidcIssuer = string(ext.Value)
		}
	}
	return info, nil
}

// VerifyOptions controls checkpoint verification.
type VerifyOptions struct {
	// LogPublicKeyPEM pins the transparency log's key (default: the
	// public Rekor key).
	LogPublicKeyPEM []byte
	// LogPublicKeyPKIX is the same key in PKIX DER (note key hint). When
	// empty it is derived from LogPublicKeyPEM.
	LogPublicKeyPKIX []byte
	// Now overrides the clock for signature timestamp sanity (tests).
	SkipWitness bool // verify the signature only (explicit narrow mode)
}

// Verify checks the checkpoint in full: the signature over the exact
// statement (WHO) and, unless explicitly skipped, the transparency-log
// witness (WHEN). Returns the witness type verified ("rekor" / "none" /
// "cosign-bundle") for reporting.
func (c *Checkpoint) Verify(opts VerifyOptions) (string, error) {
	stmt := c.CanonicalBytes()

	// WHO: signature over the exact statement bytes.
	switch c.Signer.Type {
	case "ssh":
		if err := VerifySSHSIG([]byte(c.Signature), stmt, c.Signer.PublicKey); err != nil {
			return "", err
		}
	case "cosign":
		if err := verifyCosignBundle(stmt, []byte(c.Signature), c.Signer); err != nil {
			return "", err
		}
	default:
		return "", fmt.Errorf("unknown signer type %q", c.Signer.Type)
	}

	if opts.SkipWitness {
		return c.Witness.Type, nil
	}

	// WHEN: transparency witness.
	switch c.Witness.Type {
	case "none":
		// Explicit opt-out. Not an error — the signature stands — but the
		// caller MUST surface that "when" is unproven (CLI prints a loud
		// warning; see verify.go).
		return "none", nil
	case "rekor":
		pemBytes := opts.LogPublicKeyPEM
		if len(pemBytes) == 0 {
			pemBytes = []byte(DefaultRekorPublicKey)
		}
		logPub, err := ParseLogPublicKey(pemBytes)
		if err != nil {
			return "", err
		}
		pkix := opts.LogPublicKeyPKIX
		if len(pkix) == 0 {
			pkix, err = pkixFromPEM(pemBytes)
			if err != nil {
				return "", err
			}
		}
		if err := VerifyWitness(logPub, pkix, &c.Witness, stmt, []byte(c.Signature), c.Signer.PublicKey); err != nil {
			return "", err
		}
		return "rekor", nil
	case "cosign-bundle":
		return "cosign-bundle", nil // verified with the signature above
	default:
		return "", fmt.Errorf("unknown witness type %q", c.Witness.Type)
	}
}

// pkixFromPEM extracts the DER SubjectPublicKeyInfo of a PEM public key.
func pkixFromPEM(pemBytes []byte) ([]byte, error) {
	pub, err := ParseLogPublicKey(pemBytes)
	if err != nil {
		return nil, err
	}
	return x509.MarshalPKIXPublicKey(pub)
}

// verifyCosignBundle checks a cosign checkpoint via the cosign CLI (the
// bundle's embedded tlog entry is its witness).
func verifyCosignBundle(statement []byte, bundle []byte, signer SignerInfo) error {
	if _, err := exec.LookPath("cosign"); err != nil {
		return fmt.Errorf("cosign checkpoint requires the cosign CLI to verify: %w", err)
	}
	dir, err := os.MkdirTemp("", "tapelog-ckpt-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	stmtPath := dir + "/statement"
	bundlePath := dir + "/bundle.json"
	if err := os.WriteFile(stmtPath, statement, 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(bundlePath, bundle, 0o600); err != nil {
		return err
	}
	args := []string{"verify-blob", "--bundle", bundlePath}
	if signer.Identity != "" {
		args = append(args, "--certificate-identity", signer.Identity)
	}
	if signer.OidcIssuer != "" {
		args = append(args, "--certificate-oidc-issuer", signer.OidcIssuer)
	}
	args = append(args, stmtPath)
	cmd := exec.Command("cosign", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("cosign verify-blob failed: %v: %s", err, truncate(string(out), 400))
	}
	return nil
}
