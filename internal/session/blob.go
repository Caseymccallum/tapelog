package session

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// BlobRef is the in-event placeholder for an out-of-band payload (spec/
// session-log-v0.md "Large payloads"). When `args` or `result` is too
// large to inline, the event carries {"$blob": {...}} and the bytes live
// in the blob store — content-addressed by SHA-256, so the ref in the
// hash chain binds the content: a swapped blob fails digest verification
// and a rewritten ref fails chain verification.
type BlobRef struct {
	SHA256 string `json:"sha256"` // hex SHA-256 of the blob bytes
	Size   int    `json:"size"`   // byte length of the blob
}

// BlobStore is a directory of content-addressed blobs (<digest>.blob).
type BlobStore struct {
	dir string
}

// NewBlobStore creates a store rooted at dir (created on first Put).
func NewBlobStore(dir string) *BlobStore { return &BlobStore{dir: dir} }

// DefaultBlobDir returns the conventional store location for a log:
// the log path with a ".blobs" suffix. Readers auto-resolve from here.
func DefaultBlobDir(logPath string) string { return logPath + ".blobs" }

// Dir returns the store directory.
func (b *BlobStore) Dir() string { return b.dir }

// IsBlobRef reports whether raw is a blob placeholder object.
func IsBlobRef(raw json.RawMessage) bool {
	var probe struct {
		Blob *BlobRef `json:"$blob"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return false
	}
	return probe.Blob != nil && probe.Blob.SHA256 != ""
}

// Offload stores raw in the store and returns its placeholder JSON, or
// raw unchanged when it is under threshold (or the store is nil). The
// placeholder is what gets hashed into the chain.
func (b *BlobStore) Offload(raw json.RawMessage, threshold int) (json.RawMessage, error) {
	if b == nil || threshold <= 0 || len(raw) <= threshold {
		return raw, nil
	}
	sum := sha256.Sum256(raw)
	digest := hex.EncodeToString(sum[:])
	if err := os.MkdirAll(b.dir, 0o700); err != nil {
		return nil, fmt.Errorf("create blob store: %w", err)
	}
	path := filepath.Join(b.dir, digest+".blob")
	if _, err := os.Stat(path); err == nil {
		return blobRefJSON(BlobRef{SHA256: digest, Size: len(raw)})
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return nil, fmt.Errorf("write blob %s: %w", digest, err)
	}
	return blobRefJSON(BlobRef{SHA256: digest, Size: len(raw)})
}

// Resolve returns the content behind raw: the bytes themselves when raw
// is not a placeholder, the verified blob contents when it is. A missing
// store or digest mismatch is an error — replay needs the blob store
// (spec/FAQ.md) and never invents content.
func (b *BlobStore) Resolve(raw json.RawMessage) (json.RawMessage, error) {
	if !IsBlobRef(raw) {
		return raw, nil
	}
	if b == nil {
		return nil, fmt.Errorf("payload is a blob reference but no blob store is configured (replay needs the blob store)")
	}
	var probe struct {
		Blob *BlobRef `json:"$blob"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil, fmt.Errorf("parse blob reference: %w", err)
	}
	ref := probe.Blob
	data, err := os.ReadFile(filepath.Join(b.dir, ref.SHA256+".blob"))
	if err != nil {
		return nil, fmt.Errorf("blob store %s: %w (replay needs the blob store)", b.dir, err)
	}
	sum := sha256.Sum256(data)
	if got := hex.EncodeToString(sum[:]); got != ref.SHA256 {
		return nil, fmt.Errorf("blob %s: content digest mismatch (tampered or truncated: got %s)", ref.SHA256, got)
	}
	if ref.Size != 0 && len(data) != ref.Size {
		return nil, fmt.Errorf("blob %s: size mismatch (want %d, got %d)", ref.SHA256, ref.Size, len(data))
	}
	return json.RawMessage(data), nil
}

// VerifyBlobs re-reads a log and checks every blob reference in it
// against the store. Returns how many refs were seen; `problems` are
// evidence failures (missing blob in an existing store, digest
// mismatch — tampering or loss) and `warnings` are check gaps (no store
// at all: the chain is intact but the blobs cannot be re-hashed).
func VerifyBlobs(logPath string, store *BlobStore) (refs int, problems, warnings []string, err error) {
	f, err := os.Open(logPath)
	if err != nil {
		return 0, nil, nil, err
	}
	defer f.Close()
	missingStore := 0
	storeMissing := false
	if store != nil {
		if _, serr := os.Stat(store.dir); serr != nil {
			storeMissing = true
		}
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var e Event
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue // chain verification owns malformed-line reporting
		}
		for _, field := range extractBlobFields(e.Payload) {
			refs++
			if store == nil || storeMissing {
				missingStore++
				continue
			}
			if _, perr := store.Resolve(field); perr != nil {
				problems = append(problems, perr.Error())
			}
		}
	}
	if err := sc.Err(); err != nil {
		return refs, problems, warnings, err
	}
	if missingStore > 0 {
		warnings = append(warnings, fmt.Sprintf(
			"%d blob reference(s) cannot be checked: no blob store at %s (the chain is intact but replay needs the blob store)",
			missingStore, DefaultBlobDir(logPath)))
	}
	return refs, problems, warnings, nil
}

// extractBlobFields returns the args/result payload values that are blob
// placeholders.
func extractBlobFields(payload json.RawMessage) []json.RawMessage {
	var p struct {
		Args   json.RawMessage `json:"args"`
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		return nil
	}
	var out []json.RawMessage
	for _, raw := range []json.RawMessage{p.Args, p.Result} {
		if IsBlobRef(raw) {
			out = append(out, raw)
		}
	}
	return out
}

// ResolvePayload returns payload with any args/result blob references
// replaced by their verified content. Readers that DISPLAY payloads
// (inspect) use this so a placeholder is never shown as if it were
// content (spec/session-log-v0.md §6.2 rule 2). Unresolvable references
// become a clearly-marked marker object instead of silent passthrough.
func ResolvePayload(payload json.RawMessage, store *BlobStore) json.RawMessage {
	if len(payload) == 0 || store == nil {
		return payload
	}
	var p struct {
		Args   json.RawMessage `json:"args"`
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		return payload
	}
	if !IsBlobRef(p.Args) && !IsBlobRef(p.Result) {
		return payload
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(payload, &obj); err != nil {
		return payload
	}
	for _, key := range []string{"args", "result"} {
		raw, ok := obj[key]
		if !ok || !IsBlobRef(raw) {
			continue
		}
		content, err := store.Resolve(raw)
		if err != nil {
			obj[key] = json.RawMessage(fmt.Sprintf(`{"$blob_error":%q}`, err.Error()))
			continue
		}
		obj[key] = content
	}
	out, err := json.Marshal(obj)
	if err != nil {
		return payload
	}
	return out
}

// blobRefJSON renders the in-event placeholder.
func blobRefJSON(ref BlobRef) (json.RawMessage, error) {
	var b strings.Builder
	fmt.Fprintf(&b, `{"$blob":{"sha256":%q,"size":%d}}`, ref.SHA256, ref.Size)
	return json.RawMessage(b.String()), nil
}