package checkpoint

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Caseymccallum/tapelog/internal/session"
	"golang.org/x/crypto/ssh"
)

// makeSessionLog writes a small hash-chained session log and returns its
// path (via internal/session — the real writer).
func makeSessionLog(t *testing.T, events int) string {
	t.Helper()
	dir := t.TempDir()
	// Build via the session package's own writer through a tiny helper
	// script — simplest is direct JSON construction with correct hashes.
	// Use the session.VerifyFile contract: construct with session.NewWriter.
	return buildLog(t, dir, events)
}

// fakeRekor is a minimal Rekor v1 double: accepts rekord entries, serves
// log info + public key, and returns entries with a VALID SET (signed by
// a test key) and a VALID RFC 6962 inclusion proof over a 1-leaf tree.
type fakeRekor struct {
	key     *ecdsa.PublicKey
	keyDER  []byte
	signer  *ecdsa.PrivateKey
	entries map[string]fakeEntry
	served  bool // corrupt the served entry body
	badSET  bool // sign the SET with the wrong key
	noProof bool
}

type fakeEntry struct {
	Body           string
	IntegratedTime int64
	LogIndex       int64
	LogID          string
	SET            string
}

func newFakeRekor(t *testing.T) *fakeRekor {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	return &fakeRekor{
		key: &priv.PublicKey, keyDER: der, signer: priv,
		entries: map[string]fakeEntry{},
	}
}

func (f *fakeRekor) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/entries", func(w http.ResponseWriter, r *http.Request) {
		var prop struct {
			Spec struct {
				Signature struct {
					Content   string                   `json:"content"`
					PublicKey struct{ Content string } `json:"publicKey"`
				} `json:"signature"`
				Data struct {
					Content string `json:"content"` // flat base64 (DecodeEntry)
					Hash    *struct {
						Algorithm string `json:"algorithm"`
						Value     string `json:"value"`
					} `json:"hash"`
				} `json:"data"`
			} `json:"spec"`
		}
		if err := json.NewDecoder(r.Body).Decode(&prop); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		hashAlgo, hashVal := "sha256", ""
		if prop.Spec.Data.Content != "" {
			raw, _ := base64.StdEncoding.DecodeString(prop.Spec.Data.Content)
			hashVal = hashHex(raw) // log stores only the hash
		} else if prop.Spec.Data.Hash != nil {
			hashAlgo, hashVal = prop.Spec.Data.Hash.Algorithm, prop.Spec.Data.Hash.Value
		}
		// Canonicalized entry body as the log stores it.
		body := fmt.Sprintf(`{"spec":{"data":{"hash":{"algorithm":%q,"value":%q}},"signature":{"content":%q,"format":"ssh","publicKey":{"content":%q}}},"apiVersion":"0.0.1","kind":"rekord"}`, hashAlgo, hashVal,
			prop.Spec.Signature.Content, prop.Spec.Signature.PublicKey.Content)
		bodyB64 := base64.StdEncoding.EncodeToString([]byte(body))

		// SET over RekorPayload{body, integratedTime, logID, logIndex}.
		payload := RekorPayload{Body: bodyB64, IntegratedTime: 1700000000,
			LogID: "test-log-id", LogIndex: 0}
		msg, _ := payload.CanonicalBytes()
		digest := sha256.Sum256(msg)
		signer := f.signer
		if f.badSET {
			other, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
			signer = other
		}
		sig, _ := signer.Sign(rand.Reader, digest[:], nil)
		leaf := hashLeaf([]byte(body))
		uuid := hex.EncodeToString(leaf)

		set := base64.StdEncoding.EncodeToString(sig)
		if f.served {
			bodyB64 = base64.StdEncoding.EncodeToString([]byte(body + " "))
		}
		resp := map[string]any{uuid: map[string]any{
			"body": bodyB64, "integratedTime": 1700000000,
			"logID": "test-log-id", "logIndex": 0,
			"verification": map[string]any{
				"signedEntryTimestamp": set,
				"inclusionProof":       f.proof(leaf),
			},
		}}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(201)
		_ = json.NewEncoder(w).Encode(resp)
	})
	return mux
}

// proof builds a valid inclusion proof for a single-leaf tree: the root
// IS the leaf, the proof is empty, and the checkpoint note covers it.
func (f *fakeRekor) proof(leaf []byte) map[string]any {
	if f.noProof {
		return nil
	}
	note := fmt.Sprintf("test.rekor - 1\n1\n%s\n\n\u2014 test.rekor %s\n",
		base64.StdEncoding.EncodeToString(leaf),
		base64.StdEncoding.EncodeToString(append(f.hint(), f.noteSig(leaf)...)))
	return map[string]any{
		"logIndex": 0, "treeSize": 1,
		"rootHash":   hex.EncodeToString(leaf),
		"hashes":     []string{},
		"checkpoint": note,
	}
}

func (f *fakeRekor) hint() []byte {
	sum := sha256.Sum256(f.keyDER)
	return sum[:4]
}

func (f *fakeRekor) noteSig(leaf []byte) []byte {
	text := fmt.Sprintf("test.rekor - 1\n1\n%s\n", base64.StdEncoding.EncodeToString(leaf))
	digest := sha256.Sum256([]byte(text))
	sig, _ := f.signer.Sign(rand.Reader, digest[:], nil)
	return sig
}

// buildLog writes a real hash-chained session log via internal/session.
func buildLog(t *testing.T, dir string, events int) string {
	t.Helper()
	path := filepath.Join(dir, "session.jsonl")
	w, err := session.NewWriter(path, "ckpt-test")
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	for i := 0; i < events; i++ {
		if _, err := w.Append(session.EventToolCall, session.ToolCallPayload{
			ID: json.RawMessage(fmt.Sprintf(`%d`, i+1)), Tool: "read_file",
			Args: json.RawMessage(`{"path":"x"}`),
		}); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

// newSSHKey generates an ed25519 OpenSSH key pair in PEM form.
func newSSHKey(t *testing.T) (privatePEM []byte, authorizedKey string) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatal(err)
	}
	privatePEM = pem.EncodeToMemory(block)
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	authorizedKey = string(ssh.MarshalAuthorizedKey(signer.PublicKey()))
	return privatePEM, authorizedKey
}

func TestStatementCanonicalBytes(t *testing.T) {
	s := Statement{Version: 1, SessionID: "s1", Seq: 7, ChainHead: "abc", CreatedAt: "2026-10-01T00:00:00.000Z"}
	want := "tapelog-checkpoint-v1\nsession_id=s1\nseq=7\nchain_head=abc\ncreated_at=2026-10-01T00:00:00.000Z\n"
	if got := string(s.CanonicalBytes()); got != want {
		t.Errorf("canonical bytes:\n%q\nwant:\n%q", got, want)
	}
}

func TestSSHSIGRoundTrip(t *testing.T) {
	priv, authorized := newSSHKey(t)
	msg := []byte("tapelog-checkpoint-v1\nsession_id=x\nseq=1\nchain_head=deadbeef\ncreated_at=now\n")

	armor, keyOut, err := SignSSHSIG(priv, msg)
	if err != nil {
		t.Fatal(err)
	}
	if keyOut != authorized {
		t.Errorf("authorized key mismatch: %q vs %q", keyOut, authorized)
	}
	if !strings.Contains(string(armor), "BEGIN SSH SIGNATURE") {
		t.Errorf("armor missing SSH SIGNATURE header:\n%s", armor)
	}
	if err := VerifySSHSIG(armor, msg, authorized); err != nil {
		t.Errorf("verify failed: %v", err)
	}
	if err := VerifySSHSIG(armor, append(msg, ' '), authorized); err == nil {
		t.Error("tampered message verified")
	}
	_, otherKey := newSSHKey(t)
	if err := VerifySSHSIG(armor, msg, otherKey); err == nil {
		t.Error("signature verified against the wrong key")
	}
}

func TestCheckpointSignVerifyNoWitness(t *testing.T) {
	priv, _ := newSSHKey(t)
	logPath := makeSessionLog(t, 3)
	stmt, err := NewStatement(logPath, 0)
	if err != nil {
		t.Fatal(err)
	}
	if stmt.Seq != 3 {
		t.Errorf("Seq = %d, want 3", stmt.Seq)
	}
	ckpt := &Checkpoint{Statement: stmt}
	if err := ckpt.SignSSH(priv); err != nil {
		t.Fatal(err)
	}
	ckpt.Witness = Witness{Type: "none"}

	// Verifies, but reports "none" so the caller warns loudly.
	wit, err := ckpt.Verify(VerifyOptions{})
	if err != nil {
		t.Fatalf("unwitnessed checkpoint must verify (warning is the caller's job): %v", err)
	}
	if wit != "none" {
		t.Errorf("witness type = %q", wit)
	}

	ckpt.Seq = 99
	if _, err := ckpt.Verify(VerifyOptions{}); err == nil {
		t.Error("tampered statement verified")
	}
}

func TestCheckpointBindsChainHead(t *testing.T) {
	priv, _ := newSSHKey(t)
	logPath := makeSessionLog(t, 4)
	stmt, err := NewStatement(logPath, 2) // mid-session prefix
	if err != nil {
		t.Fatal(err)
	}
	ckpt := &Checkpoint{Statement: stmt}
	if err := ckpt.SignSSH(priv); err != nil {
		t.Fatal(err)
	}
	ckpt.Witness = Witness{Type: "none"}

	// The attested head must be the event-2 hash, not the end of the log.
	at2, err := session.HashAt(logPath, 2)
	if err != nil {
		t.Fatal(err)
	}
	if stmt.ChainHead != at2 {
		t.Errorf("chain head %s, want event-2 hash %s", stmt.ChainHead, at2)
	}
	if _, err := ckpt.Verify(VerifyOptions{SkipWitness: true}); err != nil {
		t.Errorf("verify failed: %v", err)
	}
	if _, err := NewStatement(logPath, 99); err == nil {
		t.Error("seq beyond log must fail")
	}
}

// logPubPEM renders the fake log's public key as PKIX PEM.
func logPubPEM(t *testing.T, fr *fakeRekor) []byte {
	t.Helper()
	return pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: fr.keyDER})
}

func TestRekorWitnessFullChain(t *testing.T) {
	fr := newFakeRekor(t)
	srv := httptest.NewServer(fr.handler())
	defer srv.Close()

	priv, _ := newSSHKey(t)
	logPath := makeSessionLog(t, 2)
	stmt, err := NewStatement(logPath, 0)
	if err != nil {
		t.Fatal(err)
	}
	ckpt := &Checkpoint{Statement: stmt}
	if err := ckpt.SignSSH(priv); err != nil {
		t.Fatal(err)
	}
	w, err := SubmitRekor(context.Background(), srv.URL, ckpt.CanonicalBytes(),
		[]byte(ckpt.Signature), ckpt.Signer.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	ckpt.Witness = *w

	pkix, err := pkixFromPEM(logPubPEM(t, fr))
	if err != nil {
		t.Fatal(err)
	}
	if wit, err := ckpt.Verify(VerifyOptions{
		LogPublicKeyPEM: logPubPEM(t, fr), LogPublicKeyPKIX: pkix,
	}); err != nil || wit != "rekor" {
		t.Fatalf("full witness chain failed: wit=%q err=%v", wit, err)
	}
}

func TestRekorWitnessTamperCases(t *testing.T) {
	for name, corrupt := range map[string]func(*fakeRekor){
		"served body mismatch": func(f *fakeRekor) { f.served = true },
		"wrong SET key":        func(f *fakeRekor) { f.badSET = true },
		"missing inclusion":    func(f *fakeRekor) { f.noProof = true },
	} {
		t.Run(name, func(t *testing.T) {
			fr := newFakeRekor(t)
			corrupt(fr)
			srv := httptest.NewServer(fr.handler())
			defer srv.Close()

			priv, _ := newSSHKey(t)
			ckpt := &Checkpoint{Statement: Statement{
				Version: 1, SessionID: "s", Seq: 1, ChainHead: "aa", CreatedAt: "now"}}
			if err := ckpt.SignSSH(priv); err != nil {
				t.Fatal(err)
			}
			w, err := SubmitRekor(context.Background(), srv.URL, ckpt.CanonicalBytes(),
				[]byte(ckpt.Signature), ckpt.Signer.PublicKey)
			if err != nil {
				t.Fatal(err)
			}
			ckpt.Witness = *w
			pkix, _ := pkixFromPEM(logPubPEM(t, fr))
			if _, err := ckpt.Verify(VerifyOptions{
				LogPublicKeyPEM: logPubPEM(t, fr), LogPublicKeyPKIX: pkix,
			}); err == nil {
				t.Error("corrupted witness verified")
			}
		})
	}
}
