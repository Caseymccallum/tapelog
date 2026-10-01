package tui

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Caseymccallum/tapelog/internal/session"
)

// buildTestLog writes a session with a contaminated deny (mirroring the
// trajectory demo: read_secrets produces a value, send_http carries it
// and is denied by the flow rule) plus a plain rule deny.
func buildTestLog(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "session.jsonl")
	w, err := session.NewWriter(path, "sess-story-test")
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	// 1: tools/call read_secrets (record mode: un-namespaced name)
	if _, err := w.Append(session.EventToolCall, session.ToolCallPayload{
		ID: json.RawMessage(`1`), Tool: "read_secrets", Args: json.RawMessage(`{}`),
	}); err != nil {
		t.Fatal(err)
	}
	// 2: its result (canary value)
	if _, err := w.Append(session.EventToolResult, session.ToolResultPayload{
		ID: json.RawMessage(`1`), Result: json.RawMessage(`{"text":"sk-CANARY-7f3a9b"}`),
	}); err != nil {
		t.Fatal(err)
	}
	// 3: tools/call send_http carrying the value
	if _, err := w.Append(session.EventToolCall, session.ToolCallPayload{
		ID: json.RawMessage(`2`), Tool: "send_http",
		Args: json.RawMessage(`{"body":"exfiltrating sk-CANARY-7f3a9b now"}`),
	}); err != nil {
		t.Fatal(err)
	}
	// 4: flow deny with provenance suffix
	if _, err := w.Append(session.EventPolicyDecision, session.PolicyDecisionPayload{
		ID: json.RawMessage(`2`), Verdict: "deny", RuleID: "no-secret-exfil",
		Reason: "secret values must not reach a sink [contaminated by: read_secrets]",
	}); err != nil {
		t.Fatal(err)
	}
	// 5: tools/call delete_file
	if _, err := w.Append(session.EventToolCall, session.ToolCallPayload{
		ID: json.RawMessage(`3`), Tool: "delete_file", Args: json.RawMessage(`{"path":"/x"}`),
	}); err != nil {
		t.Fatal(err)
	}
	// 6: rule deny (no provenance)
	if _, err := w.Append(session.EventPolicyDecision, session.PolicyDecisionPayload{
		ID: json.RawMessage(`3`), Verdict: "deny", RuleID: "no-delete",
		Reason: "deletes are not delegated to agents",
	}); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestStoryProvenance(t *testing.T) {
	items, sum, story, err := Load(buildTestLog(t))
	if err != nil {
		t.Fatal(err)
	}
	if sum.Denied != 2 {
		t.Fatalf("Denied = %d, want 2", sum.Denied)
	}
	if story.SessionID != "sess-story-test" {
		t.Errorf("session id = %q", story.SessionID)
	}
	if len(story.Denied) != 2 {
		t.Fatalf("denied blocks = %d, want 2", len(story.Denied))
	}

	// The contaminated deny must carry provenance pointing at the
	// producing call (#1) and its result (#2).
	d := story.Denied[0]
	if d.Tool != "send_http" || d.CallSeq != 3 || d.RuleID != "no-secret-exfil" {
		t.Errorf("block 0 = %+v", d)
	}
	if len(d.Provenance) != 1 {
		t.Fatalf("provenance = %+v", d.Provenance)
	}
	pv := d.Provenance[0]
	if pv.Source != "read_secrets" || pv.CallSeq != 1 || pv.ResultSeq != 2 {
		t.Errorf("provenance = %+v, want source read_secrets call #1 result #2", pv)
	}
	if d.Bypass {
		t.Errorf("no result recorded for the deny — Bypass must be false")
	}

	// The rule deny has no provenance and still gets a causal story.
	d2 := story.Denied[1]
	if d2.Tool != "delete_file" || d2.CallSeq != 5 || len(d2.Provenance) != 0 {
		t.Errorf("block 1 = %+v", d2)
	}

	// Plain rendering: DENIED section with event #, session, provenance.
	out := Plain(items, sum, story)
	for _, want := range []string{
		"── denied calls (2) ──",
		"#3  sess-story-test  send_http",
		"denied by no-secret-exfil",
		"provenance: value from read_secrets — produced at call #1 (result #2)",
		"causal story: call #3 → deny (no-secret-exfil) ← read_secrets → never executed",
		"#5  sess-story-test  delete_file",
		"causal story: call #5 → deny (no-delete) → never executed",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("plain output missing %q:\n%s", want, out)
		}
	}
}

func TestStoryMuxNamespacedSource(t *testing.T) {
	// Mux mode: tools are <server>__<tool> but the flow reason carries the
	// same namespaced name — provenance must still find the producing call.
	path := filepath.Join(t.TempDir(), "session.jsonl")
	w, err := session.NewWriter(path, "sess-ns")
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	w.Append(session.EventToolCall, session.ToolCallPayload{
		ID: json.RawMessage(`1`), Tool: "a__read_secrets", Args: json.RawMessage(`{}`)})
	w.Append(session.EventToolResult, session.ToolResultPayload{
		ID: json.RawMessage(`1`), Result: json.RawMessage(`{"text":"sk-CANARY-7f3a9b"}`)})
	w.Append(session.EventToolCall, session.ToolCallPayload{
		ID: json.RawMessage(`2`), Tool: "a__send_http", Args: json.RawMessage(`{"body":"sk-CANARY-7f3a9b"}`)})
	w.Append(session.EventPolicyDecision, session.PolicyDecisionPayload{
		ID: json.RawMessage(`2`), Verdict: "deny", RuleID: "no-secret-exfil",
		Reason: "secret values must not reach a sink [contaminated by: a__read_secrets]"})

	_, _, story, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(story.Denied) != 1 || len(story.Denied[0].Provenance) != 1 {
		t.Fatalf("story = %+v", story)
	}
	pv := story.Denied[0].Provenance[0]
	if pv.CallSeq != 1 || pv.ResultSeq != 2 {
		t.Errorf("namespaced provenance = %+v, want call #1 result #2", pv)
	}
}

func TestStoryBypassFlagged(t *testing.T) {
	// A denied call that DID produce a result must be flagged.
	path := filepath.Join(t.TempDir(), "session.jsonl")
	w, err := session.NewWriter(path, "sess-bypass")
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	w.Append(session.EventToolCall, session.ToolCallPayload{
		ID: json.RawMessage(`1`), Tool: "delete_file", Args: json.RawMessage(`{"path":"/x"}`)})
	w.Append(session.EventPolicyDecision, session.PolicyDecisionPayload{
		ID: json.RawMessage(`1`), Verdict: "deny", RuleID: "no-delete", Reason: "no"})
	w.Append(session.EventToolResult, session.ToolResultPayload{
		ID: json.RawMessage(`1`), Result: json.RawMessage(`{"text":"deleted"}`)})

	_, _, story, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	d := story.Denied[0]
	if !d.Bypass || d.BypassSeq != 3 {
		t.Fatalf("bypass not flagged: %+v", d)
	}
	if s := causalLine(d); !strings.Contains(s, "no_deny_bypassed") {
		t.Errorf("causal line must flag the bypass: %q", s)
	}
}

func TestSameToolNamespace(t *testing.T) {
	for _, tc := range []struct {
		tool, source string
		want         bool
	}{
		{"read_secrets", "read_secrets", true},
		{"a__read_secrets", "read_secrets", true},
		{"read_secrets", "a__read_secrets", true},
		{"a__read_secrets", "b__read_secrets", true},
		{"a__send_http", "read_secrets", false},
	} {
		if got := sameTool(tc.tool, tc.source); got != tc.want {
			t.Errorf("sameTool(%q, %q) = %v, want %v", tc.tool, tc.source, got, tc.want)
		}
	}
}
