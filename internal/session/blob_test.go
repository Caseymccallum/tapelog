package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBlobRoundTrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "s.jsonl.blobs")
	store := NewBlobStore(dir)
	big := json.RawMessage(`{"text":"` + strings.Repeat("x", 256) + `"}`)
	small := json.RawMessage(`{"text":"tiny"}`)

	// Under threshold / disabled: passthrough unchanged.
	if out, err := store.Offload(big, 1024); err != nil || string(out) != string(big) {
		t.Fatalf("under-threshold payload must pass through: %v %s", err, out)
	}
	if out, err := store.Offload(big, 0); err != nil || string(out) != string(big) {
		t.Fatalf("zero threshold must pass through: %v %s", err, out)
	}
	var nilStore *BlobStore
	if out, err := nilStore.Offload(big, 16); err != nil || string(out) != string(big) {
		t.Fatalf("nil store must pass through: %v %s", err, out)
	}

	// Over threshold: placeholder stored, content resolvable + verified.
	ref, err := store.Offload(big, 64)
	if err != nil {
		t.Fatal(err)
	}
	if !IsBlobRef(ref) {
		t.Fatalf("want blob placeholder, got %s", ref)
	}
	if IsBlobRef(small) {
		t.Fatalf("plain payload must not look like a ref")
	}
	got, err := store.Resolve(ref)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(big) {
		t.Fatalf("resolved content mismatch: %s", got)
	}
	// Non-refs resolve to themselves.
	if got, err := store.Resolve(small); err != nil || string(got) != string(small) {
		t.Fatalf("plain resolve: %v %s", err, got)
	}

	// Tamper: rewrite the blob file -> digest mismatch, fail loud.
	var probe struct {
		Blob BlobRef `json:"$blob"`
	}
	_ = json.Unmarshal(ref, &probe)
	path := filepath.Join(dir, probe.Blob.SHA256+".blob")
	if err := os.WriteFile(path, []byte(`{"text":"EVIL"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Resolve(ref); err == nil || !strings.Contains(err.Error(), "digest mismatch") {
		t.Fatalf("tampered blob must fail digest check, got %v", err)
	}

	// Missing blob -> fail loud with the store path named.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Resolve(ref); err == nil || !strings.Contains(err.Error(), "replay needs the blob store") {
		t.Fatalf("missing blob must fail loud, got %v", err)
	}

	// Ref without any store configured -> fail loud (replay needs it).
	if _, err := (*BlobStore)(nil).Resolve(ref); err == nil {
		t.Fatal("nil store must refuse to resolve refs")
	}
}

func TestVerifyBlobs(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "s.jsonl")
	store := NewBlobStore(DefaultBlobDir(logPath))
	big := json.RawMessage(`{"text":"` + strings.Repeat("y", 128) + `"}`)
	ref, err := store.Offload(big, 32)
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]json.RawMessage{"args": ref, "id": json.RawMessage(`1`)})
	w, err := NewWriter(logPath, "blob-test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Append(EventToolCall, json.RawMessage(payload)); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()

	// Healthy store: all refs verified.
	refs, problems, warnings, err := VerifyBlobs(logPath, store)
	if err != nil || refs != 1 || len(problems) != 0 || len(warnings) != 0 {
		t.Fatalf("healthy: refs=%d problems=%v warnings=%v err=%v", refs, problems, warnings, err)
	}

	// Store dir missing entirely: warning (chain intact), not a failure.
	refs, problems, warnings, err = VerifyBlobs(logPath, NewBlobStore(logPath+".nowhere"))
	if err != nil || refs != 1 || len(problems) != 0 || len(warnings) != 1 {
		t.Fatalf("absent store: refs=%d problems=%v warnings=%v err=%v", refs, problems, warnings, err)
	}

	// Tampered blob in an existing store: problem.
	var probe struct {
		Blob BlobRef `json:"$blob"`
	}
	_ = json.Unmarshal(ref, &probe)
	blobPath := filepath.Join(store.Dir(), probe.Blob.SHA256+".blob")
	if err := os.WriteFile(blobPath, []byte("swapped"), 0o600); err != nil {
		t.Fatal(err)
	}
	refs, problems, _, err = VerifyBlobs(logPath, store)
	if err != nil || len(problems) != 1 {
		t.Fatalf("tampered: refs=%d problems=%v err=%v", refs, problems, err)
	}
}

func TestResolvePayload(t *testing.T) {
	store := NewBlobStore(filepath.Join(t.TempDir(), "blobs"))
	big := json.RawMessage(`{"text":"` + strings.Repeat("z", 96) + `"}`)
	ref, err := store.Offload(big, 16)
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]json.RawMessage{"result": ref})

	out := ResolvePayload(payload, store)
	if IsBlobRef(mustField(t, out, "result")) {
		t.Fatalf("resolved payload must carry content, got %s", out)
	}
	if string(mustField(t, out, "result")) != string(big) {
		t.Fatalf("content mismatch: %s", out)
	}

	// Unresolvable ref becomes a marked error, never silent placeholder
	// passthrough pretending to be content.
	out = ResolvePayload(payload, NewBlobStore(filepath.Join(t.TempDir(), "gone")))
	if got := string(mustField(t, out, "result")); !strings.Contains(got, "$blob_error") {
		t.Fatalf("unresolvable ref must surface an error marker, got %s", got)
	}
}

func mustField(t *testing.T, payload json.RawMessage, key string) json.RawMessage {
	t.Helper()
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(payload, &obj); err != nil {
		t.Fatal(err)
	}
	return obj[key]
}