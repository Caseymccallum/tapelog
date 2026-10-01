package checkpoint

// Rekor v1 submission client: POST /api/v1/entries with a `rekord`
// proposal carrying the statement hash + the checkpoint's SSH signature
// and key, then capture the full log entry (body, SET, inclusion proof,
// signed tree head) for the checkpoint's witness.

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// SubmitRekor uploads a rekord entry for an ssh-signed checkpoint and
// returns the witness carrying everything offline verification needs.
func SubmitRekor(ctx context.Context, url string, statement, signature []byte, publicKey string) (*Witness, error) {
	if url == "" {
		url = DefaultRekorURL
	}
	proposal := map[string]any{
		"apiVersion": "0.0.1",
		"kind":       "rekord",
		"spec": map[string]any{
			"signature": map[string]any{
				"format":  "ssh",
				"content": base64.StdEncoding.EncodeToString(signature),
				"publicKey": map[string]any{
					"content": base64.StdEncoding.EncodeToString([]byte(publicKey)),
				},
			},
			// The rekord type verifies the signature over the content and
			// stores only its hash (content is writeOnly, hash readOnly),
			// so the persisted body binds the exact statement bytes.
			// Note: data.content is a FLAT base64 string (DecodeEntry).
			"data": map[string]any{
				"content": base64.StdEncoding.EncodeToString(statement),
			},
		},
	}
	body, err := json.Marshal(proposal)
	if err != nil {
		return nil, err
	}
	// The v1 spec mounts entries at /api/v1/entries; the current public
	// deployment (post-migration) serves /api/v1/log/entries. Try both.
	var raw []byte
	var status string
	for _, path := range []string{"/api/v1/entries", "/api/v1/log/entries"} {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url+path, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")

		client := &http.Client{Timeout: 60 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("submit to %s%s: %w", url, path, err)
		}
		raw, err = io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		if err != nil {
			return nil, err
		}
		status = resp.Status
		if resp.StatusCode == http.StatusCreated || resp.StatusCode == http.StatusOK {
			break
		}
		if resp.StatusCode == http.StatusNotFound {
			continue // try the next mount point
		}
		return nil, fmt.Errorf("rekor rejected the entry (%s): %s", status, truncate(string(raw), 400))
	}

	// Response: {"<uuid>": LogEntryAnon}.
	var entries map[string]logEntryAnon
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, fmt.Errorf("parse rekor response: %w", err)
	}
	for uuid, e := range entries {
		w := &Witness{
			Type:                 "rekor",
			URL:                  url,
			EntryUUID:            uuid,
			LogIndex:             e.LogIndex,
			IntegratedTime:       e.IntegratedTime,
			LogID:                e.LogID,
			Body:                 e.Body,
			SignedEntryTimestamp: string(e.Verification.SignedEntryTimestamp),
		}
		if e.Verification.InclusionProof != nil {
			ip := e.Verification.InclusionProof
			w.InclusionProof = &InclusionProof{
				LogIndex:   ip.LogIndex,
				RootHash:   ip.RootHash,
				TreeSize:   ip.TreeSize,
				Hashes:     ip.Hashes,
				Checkpoint: ip.Checkpoint,
			}
		}
		return w, nil
	}
	return nil, fmt.Errorf("rekor response contained no entry")
}

// logEntryAnon is the v1 API's log entry shape.
type logEntryAnon struct {
	Body           string `json:"body"` // base64
	IntegratedTime int64  `json:"integratedTime"`
	LogID          string `json:"logID"`
	LogIndex       int64  `json:"logIndex"`
	Verification   struct {
		SignedEntryTimestamp string          `json:"signedEntryTimestamp"` // base64
		InclusionProof       *inclusionProof `json:"inclusionProof"`
	} `json:"verification"`
}

type inclusionProof struct {
	LogIndex   int64    `json:"logIndex"`
	RootHash   string   `json:"rootHash"`
	TreeSize   int64    `json:"treeSize"`
	Hashes     []string `json:"hashes"`
	Checkpoint string   `json:"checkpoint"`
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
