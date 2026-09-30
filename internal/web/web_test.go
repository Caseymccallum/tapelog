package web

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Caseymccallum/tapelog/internal/approval"
	"github.com/Caseymccallum/tapelog/internal/session"
)

func parkOne(t *testing.T, q *approval.Queue) int {
	t.Helper()
	done := make(chan approval.Choice, 1)
	go func() { done <- q.Confirm("write_file", json.RawMessage(`{"path":"/x"}`)) }()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if items := q.List(); len(items) == 1 {
			return items[0].ID
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("call did not park")
	return 0
}

func TestDashboardServesWithSecurityHeaders(t *testing.T) {
	h := Handler(approval.NewQueue(time.Minute), "", "")
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, httptest.NewRequest("GET", "http://127.0.0.1:8923/", nil))
	if rw.Code != 200 {
		t.Fatalf("GET / status %d", rw.Code)
	}
	csp := rw.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "default-src 'self'") || strings.Contains(csp, "unsafe-inline") {
		t.Fatalf("CSP too weak: %q", csp)
	}
	if rw.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("logs must not be cached")
	}
	if rw.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("nosniff missing")
	}
}

func TestHostPinningBlocksDNSRebinding(t *testing.T) {
	h := Handler(approval.NewQueue(time.Minute), "", "")
	req := httptest.NewRequest("GET", "http://127.0.0.1:8923/api/pending", nil)
	req.Host = "evil.example.com" // rebound domain
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, req)
	if rw.Code != 403 {
		t.Fatalf("non-loopback Host without token must be 403, got %d", rw.Code)
	}
	// Loopback Host is fine.
	for _, host := range []string{"127.0.0.1:8923", "localhost:8923", "[::1]:8923"} {
		req2 := httptest.NewRequest("GET", "http://127.0.0.1:8923/api/pending", nil)
		req2.Host = host
		rw2 := httptest.NewRecorder()
		h.ServeHTTP(rw2, req2)
		if rw2.Code != 200 {
			t.Fatalf("host %s must be allowed, got %d", host, rw2.Code)
		}
	}
}

func TestCSRFRefusesCrossOriginPost(t *testing.T) {
	q := approval.NewQueue(time.Minute)
	h := Handler(q, "", "")
	req := httptest.NewRequest("POST", "http://127.0.0.1:8923/api/decide", strings.NewReader(`{"id":1,"verdict":"allow"}`))
	req.Header.Set("Origin", "https://evil.example") // cross-site form/fetch
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, req)
	if rw.Code != 403 {
		t.Fatalf("cross-origin POST must be 403, got %d", rw.Code)
	}
	// Same-origin POST is fine.
	req2 := httptest.NewRequest("POST", "http://127.0.0.1:8923/api/decide", strings.NewReader(`{"id":1,"verdict":"allow"}`))
	req2.Header.Set("Origin", "http://"+req2.Host)
	rw2 := httptest.NewRecorder()
	h.ServeHTTP(rw2, req2) // unknown id -> 404 proves it passed the guard
	if rw2.Code != 404 {
		t.Fatalf("same-origin POST should pass the guard (404 expected), got %d", rw2.Code)
	}
}

func TestTokenAuth(t *testing.T) {
	h := Handler(approval.NewQueue(time.Minute), "", "sekret")

	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, httptest.NewRequest("GET", "http://127.0.0.1:8923/api/pending", nil))
	if rw.Code != 401 {
		t.Fatalf("no token must be 401, got %d", rw.Code)
	}
	rw2 := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "http://127.0.0.1:8923/api/pending", nil)
	req.Header.Set("Authorization", "Bearer sekret")
	h.ServeHTTP(rw2, req)
	if rw2.Code != 200 {
		t.Fatalf("valid token must pass, got %d", rw2.Code)
	}
	// With a token, non-loopback Host is acceptable (operator opted in).
	rw3 := httptest.NewRecorder()
	req3 := httptest.NewRequest("GET", "http://127.0.0.1:8923/api/pending", nil)
	req3.Host = "ops.internal:8923"
	req3.Header.Set("Authorization", "Bearer sekret")
	h.ServeHTTP(rw3, req3)
	if rw3.Code != 200 {
		t.Fatalf("token + LAN host must pass, got %d", rw3.Code)
	}
}

func TestLogAPIAndDecideRoundtrip(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "session.jsonl")
	lines := `{"v":0,"seq":1,"ts":"2026-01-01T00:00:00Z","type":"session/start","session_id":"s","payload":{}}
{"v":0,"seq":2,"ts":"2026-01-01T00:00:01Z","type":"policy/decision","session_id":"s","payload":{"id":3,"verdict":"deny","rule_id":"no-x","reason":"nope"}}
`
	if err := os.WriteFile(logPath, []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}

	q := approval.NewQueue(time.Minute)
	h := Handler(q, logPath, "")

	// Log API: after=0 returns both; after=1 returns only seq 2.
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, httptest.NewRequest("GET", "http://127.0.0.1:8923/api/log?after=0", nil))
	var body struct {
		Events []json.RawMessage `json:"events"`
		Next   int               `json:"next"`
	}
	if err := json.Unmarshal(rw.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Events) != 2 || body.Next != 2 {
		t.Fatalf("after=0: %d events next=%d", len(body.Events), body.Next)
	}
	rw2 := httptest.NewRecorder()
	h.ServeHTTP(rw2, httptest.NewRequest("GET", "http://127.0.0.1:8923/api/log?after=1", nil))
	if err := json.Unmarshal(rw2.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Events) != 1 || body.Next != 2 {
		t.Fatalf("after=1: %d events next=%d", len(body.Events), body.Next)
	}

	// Decide roundtrip through the web API.
	id := parkOne(t, q)
	req := httptest.NewRequest("POST", "http://127.0.0.1:8923/api/decide",
		strings.NewReader(`{"id":`+itoa(id)+`,"verdict":"allow","note":"via web"}`))
	rw3 := httptest.NewRecorder()
	h.ServeHTTP(rw3, req)
	if rw3.Code != 200 {
		t.Fatalf("decide via web: %d %s", rw3.Code, rw3.Body.String())
	}
	if len(q.List()) != 0 {
		t.Fatal("queue should be empty after decide")
	}
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func TestDirModeSessionsAndChainBadges(t *testing.T) {
	dir := t.TempDir()

	// A real hash-chained log (Writer)...
	w, err := session.NewWriter(filepath.Join(dir, "good.jsonl"), "s-good")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if _, err := w.Append(session.EventType("tools/call"), map[string]any{"tool": "t"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	// ...and the same log with one event modified after writing.
	good, err := os.ReadFile(filepath.Join(dir, "good.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	tampered := strings.Replace(string(good), `"tool":"t"`, `"tool":"x"`, 1)
	if err := os.WriteFile(filepath.Join(dir, "tampered.jsonl"), []byte(tampered), 0o600); err != nil {
		t.Fatal(err)
	}

	h := DirHandler(dir, "")
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, httptest.NewRequest("GET", "http://127.0.0.1:8923/api/sessions", nil))
	var body struct {
		Sessions []SessionInfo `json:"sessions"`
	}
	if err := json.Unmarshal(rw.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	byFile := map[string]SessionInfo{}
	for _, s := range body.Sessions {
		byFile[s.File] = s
	}
	if g, ok := byFile["good.jsonl"]; !ok || !g.ChainOK || g.FirstBadSeq != 0 {
		t.Fatalf("good.jsonl must verify: %+v", byFile)
	}
	tp, ok := byFile["tampered.jsonl"]
	if !ok {
		t.Fatalf("tampered.jsonl missing: %+v", byFile)
	}
	if tp.ChainOK || tp.FirstBadSeq == 0 {
		t.Fatalf("tampered.jsonl must be flagged broken: %+v", tp)
	}
	if tp.Problem == "" {
		t.Fatalf("broken log must carry a problem description: %+v", tp)
	}
}

// The chain verdict must ride on /api/log so a tamper surfaces live in
// BOTH dashboard modes (single-session record dashboard included) on the
// next poll — no restart, no hard refresh.
func TestLogAPIReportsChainStatus(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "session.jsonl")
	w, err := session.NewWriter(logPath, "s")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if _, err := w.Append(session.EventType("tools/call"), map[string]any{"tool": "t"}); err != nil {
			t.Fatal(err)
		}
	}
	w.Close()

	h := Handler(nil, logPath, "")
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, httptest.NewRequest("GET", "http://127.0.0.1:8923/api/log?after=0", nil))
	var body struct {
		ChainOK     bool   `json:"chain_ok"`
		FirstBadSeq uint64 `json:"first_bad_seq"`
		Problem     string `json:"problem"`
	}
	if err := json.Unmarshal(rw.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.ChainOK || body.FirstBadSeq != 0 {
		t.Fatalf("intact log must report chain_ok: %+v (%s)", body, rw.Body.String())
	}

	// Tamper mid-session: the very next poll must flag it.
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	tampered := strings.Replace(string(data), `"tool":"t"`, `"tool":"x"`, 1)
	if err := os.WriteFile(logPath, []byte(tampered), 0o600); err != nil {
		t.Fatal(err)
	}
	rw2 := httptest.NewRecorder()
	h.ServeHTTP(rw2, httptest.NewRequest("GET", "http://127.0.0.1:8923/api/log?after=0", nil))
	var body2 struct {
		ChainOK     bool   `json:"chain_ok"`
		FirstBadSeq uint64 `json:"first_bad_seq"`
		Problem     string `json:"problem"`
	}
	if err := json.Unmarshal(rw2.Body.Bytes(), &body2); err != nil {
		t.Fatal(err)
	}
	if body2.ChainOK || body2.FirstBadSeq == 0 || body2.Problem == "" {
		t.Fatalf("tampered log must be flagged on the next poll: %+v (%s)", body2, rw2.Body.String())
	}
}


func TestDirModeSessionsAndTraversalDefense(t *testing.T) {
	dir := t.TempDir()
	logA := `{"v":0,"seq":1,"ts":"2026-01-01T00:00:00Z","type":"session/start","session_id":"aaa","payload":{}}
{"v":0,"seq":2,"ts":"2026-01-01T00:00:01Z","type":"policy/decision","session_id":"aaa","payload":{"id":1,"verdict":"deny","rule_id":"r","reason":"no"}}
`
	logB := `{"v":0,"seq":1,"ts":"2026-01-02T00:00:00Z","type":"session/start","session_id":"bbb","payload":{}}
`
	os.WriteFile(filepath.Join(dir, "a.jsonl"), []byte(logA), 0o600)
	os.WriteFile(filepath.Join(dir, "b.jsonl"), []byte(logB), 0o600)
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("ignore me"), 0o600)

	h := DirHandler(dir, "")

	// Sessions listing: both logs, newest first, denies counted.
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, httptest.NewRequest("GET", "http://127.0.0.1:8923/api/sessions", nil))
	var body struct {
		Sessions []SessionInfo `json:"sessions"`
	}
	if err := json.Unmarshal(rw.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Sessions) != 2 || body.Sessions[0].File != "b.jsonl" || body.Sessions[1].Denies != 1 {
		t.Fatalf("sessions listing wrong: %+v", body.Sessions)
	}

	// Per-session log fetch.
	rw2 := httptest.NewRecorder()
	h.ServeHTTP(rw2, httptest.NewRequest("GET", "http://127.0.0.1:8923/api/log?file=a.jsonl&after=0", nil))
	if !strings.Contains(rw2.Body.String(), `"session_id":"aaa"`) && !strings.Contains(rw2.Body.String(), `"verdict":"deny"`) {
		t.Fatalf("log fetch wrong: %s", rw2.Body.String())
	}

	// Path traversal / bad names are refused (400).
	for _, evil := range []string{"../evil.jsonl", "..%2Fevil.jsonl", "sub/evil.jsonl", "evil.txt", "a.jsonl%00"} {
		rw3 := httptest.NewRecorder()
		h.ServeHTTP(rw3, httptest.NewRequest("GET", "http://127.0.0.1:8923/api/log?file="+evil, nil))
		if rw3.Code != 400 {
			t.Fatalf("bad file %q must be 400, got %d", evil, rw3.Code)
		}
	}

	// Standalone mode has no queue.
	rw4 := httptest.NewRecorder()
	h.ServeHTTP(rw4, httptest.NewRequest("GET", "http://127.0.0.1:8923/api/pending", nil))
	if !strings.Contains(rw4.Body.String(), `"pending":[]`) {
		t.Fatalf("pending should be empty: %s", rw4.Body.String())
	}
	rw5 := httptest.NewRecorder()
	h.ServeHTTP(rw5, httptest.NewRequest("POST", "http://127.0.0.1:8923/api/decide", strings.NewReader(`{"id":1,"verdict":"allow"}`)))
	if rw5.Code != 404 {
		t.Fatalf("decide without queue must 404, got %d", rw5.Code)
	}
}
