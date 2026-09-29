package replay

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cassette-ai/cassette/internal/session"
)

// buildCassette writes a session log: tools/list with one tool, a read_file
// call whose args contain a secret (redacted at record time) with a result,
// and a denied delete_file call with no result.
func buildCassette(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "session.jsonl")
	w, err := session.NewWriter(path, "replay-test")
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	red := session.NewRedactor()

	w.Append(session.EventSessionStart, session.SessionStartPayload{Harness: "test", PolicyID: "observe"})
	w.Append(session.EventToolsList, session.ToolsListPayload{Tools: []json.RawMessage{
		json.RawMessage(`{"name":"read_file","description":"Read a file"}`),
	}})
	w.Append(session.EventToolCall, session.ToolCallPayload{
		ID: json.RawMessage(`1`), Tool: "read_file",
		Args: red.RedactJSON(json.RawMessage(`{"path":"/tmp/a.txt","api_key":"sk-secret-value-1234567890123456789012"}`)),
	})
	w.Append(session.EventPolicyDecision, session.PolicyDecisionPayload{ID: json.RawMessage(`1`), Verdict: "allow", RuleID: "allow-reads", Reason: "ok"})
	w.Append(session.EventToolResult, session.ToolResultPayload{
		ID: json.RawMessage(`1`), Result: json.RawMessage(`{"content":[{"type":"text","text":"file body"}]}`),
	})
	w.Append(session.EventToolCall, session.ToolCallPayload{
		ID: json.RawMessage(`2`), Tool: "delete_file",
		Args: red.RedactJSON(json.RawMessage(`{"path":"/tmp/a.txt"}`)),
	})
	w.Append(session.EventPolicyDecision, session.PolicyDecisionPayload{ID: json.RawMessage(`2`), Verdict: "deny", RuleID: "deny-delete", Reason: "no"})
	w.Append(session.EventSessionEnd, session.SessionEndPayload{Reason: "done"})
	return path
}

func TestLoadPairsInteractions(t *testing.T) {
	c, err := Load(buildCassette(t))
	if err != nil {
		t.Fatal(err)
	}
	if c.SessionID != "replay-test" {
		t.Errorf("session id = %q", c.SessionID)
	}
	if len(c.Tools) != 1 || !strings.Contains(string(c.Tools[0]), "read_file") {
		t.Errorf("tools = %s", c.Tools)
	}
	if len(c.Interactions) != 1 {
		t.Fatalf("want 1 answered interaction, got %d", len(c.Interactions))
	}
	if len(c.Unanswered) != 1 || c.Unanswered[0].Tool != "delete_file" {
		t.Fatalf("want 1 unanswered delete_file, got %+v", c.Unanswered)
	}
	it := c.Interactions[0]
	if it.Tool != "read_file" || it.Verdict != "allow" || !strings.Contains(string(it.Result), "file body") {
		t.Errorf("interaction = %+v", it)
	}
	if strings.Contains(string(it.Args), "sk-secret") {
		t.Errorf("args not redacted in cassette: %s", it.Args)
	}
}

func TestPlayMatchesAcrossRedaction(t *testing.T) {
	// The recording has api_key "[REDACTED]"; a live call with a *fresh*
	// secret must still match — that is the point of deterministic redaction.
	c, _ := Load(buildCassette(t))
	p := NewPlayer(c, MatchExact)

	it, ok := p.Play("read_file", json.RawMessage(`{"path":"/tmp/a.txt","api_key":"sk-completely-different-secret-aaaaaaaaaaaaaa"}`))
	if !ok {
		t.Fatal("live call with different secret did not match recording")
	}
	if !strings.Contains(string(it.Result), "file body") {
		t.Fatalf("result = %s", it.Result)
	}
}

func TestPlayFailLoudOnMissingRecording(t *testing.T) {
	c, _ := Load(buildCassette(t))
	p := NewPlayer(c, MatchExact)

	if _, ok := p.Play("delete_file", json.RawMessage(`{"path":"/tmp/a.txt"}`)); ok {
		t.Fatal("denied call (no recording) must not play")
	}
	if _, ok := p.Play("read_file", json.RawMessage(`{"path":"/OTHER.txt"}`)); ok {
		t.Fatal("call with different args must not play in exact mode")
	}
	_, _, misses := p.Stats()
	if len(misses) != 2 {
		t.Fatalf("want 2 misses, got %d", len(misses))
	}
}

func TestPlayConsumeOnce(t *testing.T) {
	c, _ := Load(buildCassette(t))
	p := NewPlayer(c, MatchExact)
	args := json.RawMessage(`{"path":"/tmp/a.txt","api_key":"sk-x"}`)

	if _, ok := p.Play("read_file", args); !ok {
		t.Fatal("first play failed")
	}
	if _, ok := p.Play("read_file", args); ok {
		t.Fatal("recording must be consumed once")
	}
}

func TestMatchModes(t *testing.T) {
	c, _ := Load(buildCassette(t))

	subset := NewPlayer(c, MatchSubset)
	if _, ok := subset.Play("read_file", json.RawMessage(`{"path":"/tmp/a.txt","api_key":"sk-x","extra":"field"}`)); !ok {
		t.Error("subset mode should accept live superset")
	}

	tool := NewPlayer(c, MatchTool)
	if _, ok := tool.Play("read_file", json.RawMessage(`{"anything":"goes"}`)); !ok {
		t.Error("tool mode should match on name only")
	}

	exact := NewPlayer(c, MatchExact)
	if _, ok := exact.Play("read_file", json.RawMessage(`{"path":"/tmp/a.txt","api_key":"sk-x","extra":"field"}`)); ok {
		t.Error("exact mode must reject live superset")
	}
}

func TestServeReplaysFromCassette(t *testing.T) {
	c, _ := Load(buildCassette(t))
	p := NewPlayer(c, MatchExact)
	s := &Server{Player: p, Version: "test"}

	in := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2026-07-28"}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"read_file","arguments":{"path":"/tmp/a.txt","api_key":"sk-live-secret-01234567890123456789012345"}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"write_file","arguments":{}}}`,
	}, "\n")

	var out bytes.Buffer
	if err := s.Serve(context.Background(), strings.NewReader(in), &out); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{
		`"cassette-replay"`,     // initialize answered
		`"read_file"`,           // tools/list answered from recording
		`"file body"`,           // tools/call replayed
		"no_matching_recording", // fail-loud
		"-32011",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("replay output missing %q:\n%s", want, got)
		}
	}
}

func TestMergeToolsLatestWins(t *testing.T) {
	catalog := mergeTools(nil, []json.RawMessage{json.RawMessage(`{"name":"read_file","description":"v1"}`)})
	catalog = mergeTools(catalog, []json.RawMessage{json.RawMessage(`{"name":"read_file","description":"v2"}`)})
	if len(catalog) != 1 {
		t.Fatalf("want 1 tool after merge, got %d", len(catalog))
	}
	if !strings.Contains(string(catalog[0]), "v2") {
		t.Fatalf("newest descriptor should win, got %s", catalog[0])
	}
}

func TestDiff(t *testing.T) {
	pathA := buildCassette(t)

	// B: same call, different recorded result.
	pathB := filepath.Join(t.TempDir(), "b.jsonl")
	w, _ := session.NewWriter(pathB, "replay-test")
	defer w.Close()
	red := session.NewRedactor()
	w.Append(session.EventToolCall, session.ToolCallPayload{
		ID: json.RawMessage(`1`), Tool: "read_file",
		Args: red.RedactJSON(json.RawMessage(`{"path":"/tmp/a.txt","api_key":"sk-secret-value-1234567890123456789012"}`)),
	})
	w.Append(session.EventToolResult, session.ToolResultPayload{
		ID: json.RawMessage(`1`), Result: json.RawMessage(`{"content":[{"type":"text","text":"DIFFERENT"}]}`),
	})

	a, _ := Load(pathA)
	b, _ := Load(pathB)
	res := Diff(a, b)
	if res.Same {
		t.Fatal("diff reports sessions identical")
	}
	if len(res.Changed) != 1 {
		t.Fatalf("want 1 changed interaction, got %+v", res)
	}

	if res2 := Diff(a, a); !res2.Same {
		t.Fatalf("diff of a session with itself must be Same: %+v", res2)
	}
}

