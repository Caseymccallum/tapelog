package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Caseymccallum/tapelog/internal/replay"
	"github.com/Caseymccallum/tapelog/internal/scenario"
	"github.com/Caseymccallum/tapelog/internal/session"
)

func writeWorkbenchFixture(t *testing.T, dir, name, verdict, result string, at time.Time) string {
	t.Helper()
	path := filepath.Join(dir, name)
	w, err := session.NewWriter(path, name)
	if err != nil {
		t.Fatal(err)
	}
	w.SetClock(func() time.Time { return at })
	appendEvent := func(kind session.EventType, p any) *session.Event {
		at = at.Add(100 * time.Millisecond)
		e, err := w.Append(kind, p)
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	appendEvent(session.EventSessionStart, map[string]any{"harness": "fictional", "task": "Audit project"})
	appendEvent(session.EventToolsList, session.ToolsListPayload{})
	c := appendEvent(session.EventToolCall, map[string]any{"id": 41, "tool": "files__read", "args": map[string]string{"secret": "FICTIONAL-SENSITIVE-ARG"}})
	appendEvent(session.EventPolicyDecision, map[string]any{"id": 41, "parent_seq": c.Seq, "verdict": "allow", "rule_id": "read-only", "reason": "read permitted"})
	appendEvent(session.EventToolResult, map[string]any{"id": 41, "parent_seq": c.Seq, "is_error": result == "error", "result": result})
	c = appendEvent(session.EventToolCall, map[string]any{"id": "delete", "tool": "files__delete", "args": map[string]string{"secret": "FICTIONAL-SENSITIVE-ARG"}})
	appendEvent(session.EventPolicyDecision, map[string]any{"id": "delete", "parent_seq": c.Seq, "verdict": verdict, "rule_id": "protect", "reason": "Fictional policy explanation"})
	appendEvent(session.EventSessionEnd, map[string]string{"reason": "fixture complete"})
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func requestJSON(t *testing.T, h http.Handler, method, path string, body any) (*httptest.ResponseRecorder, map[string]json.RawMessage) {
	t.Helper()
	var raw []byte
	if body != nil {
		raw, _ = json.Marshal(body)
	}
	req := httptest.NewRequest(method, "http://127.0.0.1:8923"+path, bytes.NewReader(raw))
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, req)
	var data map[string]json.RawMessage
	_ = json.Unmarshal(rw.Body.Bytes(), &data)
	return rw, data
}

func TestWorkbenchDiscoverySearchSortingAndPreservation(t *testing.T) {
	dir := t.TempDir()
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	old := writeWorkbenchFixture(t, dir, "z-old.jsonl", "deny", "ok", at)
	newer := writeWorkbenchFixture(t, dir, "a-new.jsonl", "allow", "error", at.Add(time.Hour))
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("not a recording"), 0600)
	beforeOld, _ := os.ReadFile(old)
	beforeNew, _ := os.ReadFile(newer)
	h := DirHandler(dir, "")
	rw, data := requestJSON(t, h, "GET", "/api/sessions", nil)
	if rw.Code != 200 {
		t.Fatal(rw.Body.String())
	}
	var items []SessionInfo
	json.Unmarshal(data["sessions"], &items)
	if len(items) != 2 || items[0].File != "a-new.jsonl" || items[0].Failures != 1 || items[1].Denies != 1 {
		t.Fatalf("wrong historical ordering/counts: %+v", items)
	}
	for _, query := range []string{"q=files", "q=fictional", "status=failed", "status=denied", "sort=oldest", "offset=10000", "q=does-not-exist"} {
		rw, _ := requestJSON(t, h, "GET", "/api/sessions?"+query, nil)
		if rw.Code != 200 {
			t.Fatal(query, rw.Body.String())
		}
	}
	_, data = requestJSON(t, h, "GET", "/api/sessions?status=denied", nil)
	json.Unmarshal(data["sessions"], &items)
	if len(items) != 1 || items[0].File != "z-old.jsonl" {
		t.Fatal("denial filter wrong")
	}
	_, data = requestJSON(t, h, "GET", "/api/activity?file=z-old.jsonl&status=denied&server=files", nil)
	var calls []activity
	json.Unmarshal(data["activities"], &calls)
	if len(calls) != 1 || calls[0].Tool != "files__delete" || calls[0].Rule != "protect" || calls[0].Reason != "Fictional policy explanation" {
		t.Fatalf("denial explanation lost: %+v", calls)
	}
	for _, p := range []string{"/api/events?file=z-old.jsonl", "/api/event?file=z-old.jsonl&seq=6", "/api/compare?a=z-old.jsonl&b=a-new.jsonl"} {
		rw, _ := requestJSON(t, h, "GET", p, nil)
		if rw.Code != 200 {
			t.Fatal(p, rw.Body.String())
		}
	}
	afterOld, _ := os.ReadFile(old)
	afterNew, _ := os.ReadFile(newer)
	if !bytes.Equal(beforeOld, afterOld) || !bytes.Equal(beforeNew, afterNew) {
		t.Fatal("viewing changed evidence")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 3 {
		t.Fatal("read APIs created files")
	}
}

func TestTimelineUsesExplicitLinksAndDoesNotGuess(t *testing.T) {
	events := []session.Event{
		{Seq: 1, TS: "2026-01-01T00:00:00Z", Type: session.EventToolCall, Payload: json.RawMessage(`{"id":1,"tool":"a"}`)},
		{Seq: 2, TS: "2026-01-01T00:00:01Z", Type: session.EventToolCall, Payload: json.RawMessage(`{"id":2,"tool":"b","parent_seq":1}`)},
		{Seq: 3, TS: "2026-01-01T00:00:02Z", Type: session.EventToolResult, Payload: json.RawMessage(`{"id":2,"parent_seq":2,"result":{},"is_error":true}`)},
		{Seq: 4, TS: "2026-01-01T00:00:03Z", Type: session.EventToolResult, Payload: json.RawMessage(`{"id":1,"parent_seq":1,"result":{}}`)},
	}
	rec := &recording{Events: events}
	deriveRecording(rec)
	if len(rec.Activities) != 2 || rec.Activities[0].Seq != 1 || rec.Activities[0].ResultSeq != 4 || rec.Activities[1].ResultSeq != 3 || *rec.Activities[0].DurationMS != 3000 || rec.Activities[1].Status != "failed" {
		t.Fatalf("overlap was paired incorrectly: %+v", rec.Activities)
	}
	if rec.Activities[0].Server != "Not recorded" {
		t.Fatal("invented server identity")
	}
	events[2].Payload = json.RawMessage(`{"id":2,"parent_seq":999,"result":{}}`)
	rec = &recording{Events: events}
	deriveRecording(rec)
	if !rec.Info.Ambiguous || rec.Activities[1].ResultSeq != 0 {
		t.Fatal("invalid parent silently fell back to an ID")
	}
	events[2].Payload = json.RawMessage(`{"id":2,"result":{}}`)
	events[2].TS = "2025-01-01T00:00:00Z"
	rec = &recording{Events: events}
	deriveRecording(rec)
	if rec.Activities[1].DurationMS != nil || rec.Activities[1].Link != "Unique preceding request ID (legacy)" {
		t.Fatal("legacy link or negative timing wrong")
	}
	events[1].Payload = json.RawMessage(`{"id":1,"tool":"b"}`)
	rec = &recording{Events: events}
	deriveRecording(rec)
	if !rec.Info.Ambiguous {
		t.Fatal("duplicate ID not marked ambiguous")
	}
}

func TestLibrarySortsActualInstantsAcrossTimezones(t *testing.T) {
	dir := t.TempDir()
	for name, ts := range map[string]string{"later.jsonl": "2026-01-01T00:00:00-05:00", "earlier.jsonl": "2026-01-01T04:00:00Z"} {
		e := session.Event{Version: 0, Seq: 1, TS: ts, Type: session.EventSessionStart, SessionID: name, Payload: json.RawMessage(`{}`)}
		e.Hash, _ = e.ComputeHash()
		raw, _ := json.Marshal(e)
		os.WriteFile(filepath.Join(dir, name), append(raw, '\n'), 0600)
	}
	h := DirHandler(dir, "")
	_, body := requestJSON(t, h, "GET", "/api/sessions", nil)
	var rows []SessionInfo
	json.Unmarshal(body["sessions"], &rows)
	if len(rows) != 2 || rows[0].File != "later.jsonl" {
		t.Fatal("timezone offsets were sorted as strings")
	}
	_, body = requestJSON(t, h, "GET", "/api/sessions?sort=oldest", nil)
	json.Unmarshal(body["sessions"], &rows)
	if rows[0].File != "earlier.jsonl" {
		t.Fatal("oldest sort ignores timezone")
	}
}

func TestEmptyInvalidMissingFieldsAndPartialTail(t *testing.T) {
	empty := decodeRecording(nil, "empty.jsonl")
	if empty.Info.Integrity != "empty" || empty.Info.ChainOK {
		t.Fatal("empty file must not be called verified")
	}
	missing := decodeRecording([]byte(`{"v":0,"seq":1,"payload":{}}`), "old.jsonl")
	if missing.Info.Integrity != "invalid" || missing.Info.Complete {
		t.Fatal("missing fields not visible")
	}
	dir := t.TempDir()
	path := writeWorkbenchFixture(t, dir, "good.jsonl", "deny", "ok", time.Now())
	data, _ := os.ReadFile(path)
	partial := decodeRecording(append(data, []byte(`{"seq":9`)...), "partial.jsonl")
	if partial.Info.Integrity != "invalid" || partial.Info.FirstBadSeq == 0 {
		t.Fatal("partial tail not surfaced")
	}
	data = bytes.Replace(data, []byte("read permitted"), []byte("tampered reason"), 1)
	bad := decodeRecording(data, "bad.jsonl")
	if bad.Info.ChainOK || bad.Info.Problem == "" {
		t.Fatal("tampered chain accepted")
	}
}

func TestSizeAndEventPaginationBounds(t *testing.T) {
	dir := t.TempDir()
	f, err := os.Create(filepath.Join(dir, "oversize.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	f.Truncate(maxRecordingBytes + 1)
	f.Close()
	h := DirHandler(dir, "")
	rw, _ := requestJSON(t, h, "GET", "/api/activity?file=oversize.jsonl", nil)
	if rw.Code != 400 || !strings.Contains(rw.Body.String(), "16 MiB") {
		t.Fatal(rw.Code, rw.Body.String())
	}
	var data bytes.Buffer
	for i := 1; i <= 1200; i++ {
		fmt.Fprintf(&data, "{\"v\":0,\"seq\":%d,\"type\":\"tools/call\",\"payload\":{\"id\":%d,\"tool\":\"read\"}}\n", i, i)
	}
	os.WriteFile(filepath.Join(dir, "large.jsonl"), data.Bytes(), 0600)
	rw, body := requestJSON(t, h, "GET", "/api/activity?file=large.jsonl&offset=100&limit=50000", nil)
	var calls []activity
	json.Unmarshal(body["activities"], &calls)
	if rw.Code != 200 || len(calls) != 100 || calls[0].Seq != 101 || calls[99].Seq != 200 {
		t.Fatal("activity pagination is not bounded")
	}
	payload := session.Event{Seq: 1, Payload: json.RawMessage(`{"text":"` + strings.Repeat("x", maxPreviewBytes*3) + `"}`)}
	view := viewEvent(payload)
	if !view.Truncated || len(view.Raw) > maxPreviewBytes+150 {
		t.Fatal("event preview unbounded")
	}
	tooMany := bytes.Repeat([]byte("{}\n"), maxRecordingEvents+1)
	limited := decodeRecording(tooMany, "many.jsonl")
	if limited.Info.Integrity != "limited" || len(limited.Events) != maxRecordingEvents {
		t.Fatal("event count limit not enforced")
	}
}

func TestPathConfinementAndAPISecurity(t *testing.T) {
	dir := t.TempDir()
	writeWorkbenchFixture(t, dir, "a.jsonl", "deny", "ok", time.Now())
	h := DirHandler(dir, "")
	for _, evil := range []string{"../escape.jsonl", "..\\escape.jsonl", "C:escape.jsonl", "a.jsonl:stream", "a.jsonl\x00", "/a.jsonl"} {
		for _, endpoint := range []string{"/api/activity?file=", "/api/events?file=", "/api/event?seq=3&file=", "/api/compare?b=a.jsonl&a="} {
			rw, _ := requestJSON(t, h, "GET", endpoint+url.QueryEscape(evil), nil)
			if rw.Code != 400 {
				t.Fatal("unsafe path accepted", evil, endpoint, rw.Code)
			}
		}
	}
	for _, endpoint := range []string{"/api/activity?file=a.jsonl", "/api/events?file=a.jsonl", "/api/event?file=a.jsonl&seq=3", "/api/compare?a=a.jsonl&b=a.jsonl", "/api/sessions"} {
		req := httptest.NewRequest("GET", "http://evil.example"+endpoint, nil)
		rw := httptest.NewRecorder()
		h.ServeHTTP(rw, req)
		if rw.Code != 403 {
			t.Fatal("host pinning lost", endpoint)
		}
	}
	req := httptest.NewRequest("POST", "http://127.0.0.1:8923/api/test", strings.NewReader(`{}`))
	req.Header.Set("Origin", "https://evil.example")
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, req)
	if rw.Code != 403 {
		t.Fatal("test CSRF guard lost")
	}
	auth := DirHandler(dir, "secret")
	rw, _ = requestJSON(t, auth, "GET", "/api/activity?file=a.jsonl", nil)
	if rw.Code != 401 {
		t.Fatal("new endpoint bypassed token")
	}
	for _, endpoint := range []string{"/api/activity", "/api/events", "/api/event", "/api/compare", "/api/sessions"} {
		rw, _ := requestJSON(t, h, "POST", endpoint, nil)
		if rw.Code != 405 {
			t.Fatal("GET method guard lost", endpoint)
		}
	}
}

func TestSymlinkRecordingCannotEscape(t *testing.T) {
	outside := filepath.Join(t.TempDir(), "private.jsonl")
	os.WriteFile(outside, []byte("fictional-private"), 0600)
	dir := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(dir, "linked.jsonl")); err != nil {
		t.Skip("symlink creation unavailable:", err)
	}
	h := DirHandler(dir, "")
	rw, _ := requestJSON(t, h, "GET", "/api/events?file=linked.jsonl", nil)
	if rw.Code != 400 || strings.Contains(rw.Body.String(), "fictional-private") {
		t.Fatal("symlink read escaped root")
	}
}

func TestComparisonUsesExistingEngineAndIgnoresClockIDs(t *testing.T) {
	dir := t.TempDir()
	at := time.Now()
	a := writeWorkbenchFixture(t, dir, "a.jsonl", "deny", "ok", at)
	b := writeWorkbenchFixture(t, dir, "b.jsonl", "deny", "ok", at.Add(time.Hour))
	h := DirHandler(dir, "")
	rw, body := requestJSON(t, h, "GET", "/api/compare?a=a.jsonl&b=b.jsonl", nil)
	var got replay.DiffResult
	json.Unmarshal(body["diff"], &got)
	if rw.Code != 200 || !got.Same {
		t.Fatal("timestamps/session IDs made diff noisy", rw.Body.String())
	}
	writeWorkbenchFixture(t, dir, "b.jsonl", "deny", "changed result", at.Add(time.Hour))
	rw, body = requestJSON(t, h, "GET", "/api/compare?a=a.jsonl&b=b.jsonl", nil)
	json.Unmarshal(body["diff"], &got)
	ta, _ := replay.Load(a)
	tb, _ := replay.Load(b)
	want := replay.Diff(ta, tb)
	if got.Same || len(got.Changed) != 1 || len(got.Changed) != len(want.Changed) {
		t.Fatal("engine result not reflected", rw.Body.String())
	}
	data, _ := os.ReadFile(a)
	os.WriteFile(filepath.Join(dir, "bad.jsonl"), bytes.Replace(data, []byte("read permitted"), []byte("tampered"), 1), 0600)
	rw, _ = requestJSON(t, h, "GET", "/api/compare?a=a.jsonl&b=bad.jsonl", nil)
	if rw.Code != 422 {
		t.Fatal("corrupt comparison accepted")
	}
}

func TestGeneratedScenarioSyntaxBehaviourAndNoPayloadCopy(t *testing.T) {
	dir := t.TempDir()
	path := writeWorkbenchFixture(t, dir, "a.jsonl", "deny", "ok", time.Now())
	before, _ := os.ReadFile(path)
	rec := decodeRecording(before, "a.jsonl")
	h := DirHandler(dir, "")
	req := map[string]any{"file": "a.jsonl", "revision": rec.Info.Revision, "seq": 6, "kind": "denied", "fixture": "a.jsonl", "run": true}
	rw, body := requestJSON(t, h, "POST", "/api/test", req)
	if rw.Code != 200 {
		t.Fatal(rw.Body.String())
	}
	var generated string
	json.Unmarshal(body["yaml"], &generated)
	var passed bool
	json.Unmarshal(body["passed"], &passed)
	if !passed || strings.Contains(generated, "FICTIONAL-SENSITIVE-ARG") || strings.Contains(generated, "Fictional policy explanation") {
		t.Fatal("generated test leaks payload or fails")
	}
	// Write only in this test to prove compatibility with the real CLI parser.
	testPath := filepath.Join(t.TempDir(), "test.yaml")
	os.WriteFile(testPath, []byte(generated), 0600)
	sc, err := scenario.Load(testPath)
	if err != nil {
		t.Fatal(err)
	}
	mutated := writeWorkbenchFixture(t, t.TempDir(), "mutated.jsonl", "allow", "ok", time.Now())
	tape, _ := replay.Load(mutated)
	if len(scenario.Run(sc, tape, nil)) != 1 {
		t.Fatal("generated assertion does not detect the denial regression")
	}
	req["revision"] = "stale"
	rw, _ = requestJSON(t, h, "POST", "/api/test", req)
	if rw.Code != 409 {
		t.Fatal("stale evidence accepted")
	}
	req["revision"] = rec.Info.Revision
	req["kind"] = "never_answered"
	req["seq"] = 3
	rw, _ = requestJSON(t, h, "POST", "/api/test", req)
	if rw.Code != 422 {
		t.Fatal("false never-answered assertion accepted")
	}
	req["kind"] = "denied"
	rw, _ = requestJSON(t, h, "POST", "/api/test", req)
	if rw.Code != 422 {
		t.Fatal("false denial assertion accepted")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("test runner modified source log")
	}
	files, _ := os.ReadDir(dir)
	if len(files) != 1 {
		t.Fatal("test API wrote a file")
	}
}

func TestWorkbenchInlineLoaderRefusesExternalBlobReads(t *testing.T) {
	raw := `{"v":0,"seq":1,"type":"tools/call","payload":{"id":1,"tool":"read","args":{"$blob":{"sha256":"../../private","size":10}}}}`
	_, err := replay.LoadInline(strings.NewReader(raw))
	if err == nil || !strings.Contains(err.Error(), "blob-backed") {
		t.Fatal("inline loader tried to resolve a blob", err)
	}
}

func TestMaliciousEventIsEncodedAsData(t *testing.T) {
	dir := t.TempDir()
	attack := `<img src=x onerror="globalThis.pwned=true"><script>alert(1)</script>`
	writeWorkbenchFixture(t, dir, "x.jsonl", "deny", attack, time.Now())
	h := DirHandler(dir, "")
	rw, _ := requestJSON(t, h, "GET", "/api/event?file=x.jsonl&seq=3", nil)
	if rw.Code != 200 || strings.Contains(rw.Body.String(), "<script>") || strings.Contains(rw.Body.String(), "<img") {
		t.Fatal("malicious content was not JSON-escaped")
	}
	if !strings.Contains(rw.Header().Get("Content-Security-Policy"), "script-src 'self'") {
		t.Fatal("CSP missing")
	}
}
