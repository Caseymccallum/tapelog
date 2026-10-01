package checkpoint

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/Caseymccallum/tapelog/internal/session"
)

// SignerInfo identifies who signed a checkpoint.
type SignerInfo struct {
	// Type is "ssh" or "cosign".
	Type string `json:"type"`
	// Identity is the signer's stable identity: the SSH key's SHA256
	// fingerprint (ssh) or the Fulcio certificate identity (cosign).
	Identity string `json:"identity"`
	// OidcIssuer is the Fulcio OIDC issuer (cosign keyless) — verified
	// alongside Identity.
	OidcIssuer string `json:"oidc_issuer,omitempty"`
	// PublicKey is the authorized_keys line (ssh) or the PEM code-signing
	// certificate (cosign). Carried in the file so verification needs no
	// external key store — pin it out-of-band if identity binding matters.
	PublicKey string `json:"public_key"`
}

// Witness is the transparency-log evidence for the checkpoint's "when".
type Witness struct {
	// Type is "rekor" or "none". "none" is an explicit opt-out that
	// verify flags loudly: signed but NOT witnessed.
	Type string `json:"type"`
	// URL of the transparency log.
	URL string `json:"url,omitempty"`
	// EntryUUID / LogIndex / IntegratedTime identify the log entry.
	EntryUUID      string `json:"entry_uuid,omitempty"`
	LogIndex       int64  `json:"log_index,omitempty"`
	IntegratedTime int64  `json:"integrated_time,omitempty"`
	LogID          string `json:"log_id,omitempty"`
	// Body is the base64 canonicalized entry body (the hashedrekord or
	// rekord entry as the log stores it) — the bytes the SET covers.
	Body string `json:"body,omitempty"`
	// SignedEntryTimestamp is the log's signature over the entry (the
	// "when" claim), base64.
	SignedEntryTimestamp string `json:"signed_entry_timestamp,omitempty"`
	// InclusionProof (RFC 6962) proves the entry is in the tree whose
	// signed tree head is Checkpoint.
	InclusionProof *InclusionProof `json:"inclusion_proof,omitempty"`
}

// InclusionProof is the RFC 6962 inclusion proof plus the signed tree
// head it is relative to.
type InclusionProof struct {
	LogIndex   int64    `json:"log_index"`
	RootHash   string   `json:"root_hash"`
	TreeSize   int64    `json:"tree_size"`
	Hashes     []string `json:"hashes"`
	Checkpoint string   `json:"checkpoint"` // signed tree head (signed note)
}

// Checkpoint is the on-disk artifact: the statement plus the signature
// (who) and the witness (when).
type Checkpoint struct {
	Statement
	Signer    SignerInfo `json:"signer"`
	Signature string     `json:"signature"` // armored SSHSIG or cosign bundle ref
	Witness   Witness    `json:"witness"`
}

// Save writes the checkpoint as pretty JSON.
func (c *Checkpoint) Save(path string) error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal checkpoint: %w", err)
	}
	if err := os.WriteFile(path, append(b, '\n'), 0o644); err != nil {
		return fmt.Errorf("write checkpoint: %w", err)
	}
	return nil
}

// Load reads a checkpoint file.
func Load(path string) (*Checkpoint, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read checkpoint: %w", err)
	}
	var c Checkpoint
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("parse checkpoint: %w", err)
	}
	return &c, nil
}

// NewStatement pins the state of a verified session log at seq (0 = the
// final event): the chain must be intact through seq and the event at seq
// is the anchor. The log itself is NOT modified.
func NewStatement(logPath string, seq uint64) (Statement, error) {
	res, err := session.VerifyFile(logPath)
	if err != nil {
		return Statement{}, err
	}
	if !res.OK() {
		return Statement{}, fmt.Errorf("refusing to checkpoint an invalid log: first bad event seq %d (%s)",
			res.FirstBadSeq, res.Problem)
	}
	if seq == 0 {
		seq = uint64(res.Events)
	}
	if seq > uint64(res.Events) {
		return Statement{}, fmt.Errorf("seq %d beyond the log's %d events", seq, res.Events)
	}
	head := res.LastHash
	if seq < uint64(res.Events) {
		// Pinning a mid-session prefix: the head is the hash of the
		// event at seq. VerifyFile's LastHash is the end of the log, so
		// re-read the prefix.
		head, err = session.HashAt(logPath, seq)
		if err != nil {
			return Statement{}, err
		}
	}
	return Statement{
		Version:   CurrentVersion,
		SessionID: res.SessionID,
		Seq:       seq,
		ChainHead: head,
		CreatedAt: time.Now().UTC().Format("2006-01-02T15:04:05.000Z"),
	}, nil
}
