package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"github.com/Caseymccallum/tapelog/internal/session"
	"path/filepath"
	"strings"
	"testing"
)

func workflowFixture(t *testing.T, dir, name string, complete bool) {
	t.Helper()
	w, err := session.NewWriter(filepath.Join(dir, name), name)
	if err != nil {
		t.Fatal(err)
	}
	add := func(kind session.EventType, p any) *session.Event {
		e, e2 := w.Append(kind, p)
		if e2 != nil {
			t.Fatal(e2)
		}
		return e
	}
	add(session.EventSessionStart, map[string]any{"task": "SECRET_TASK_VALUE"})
	c := add(session.EventToolCall, map[string]any{"id": 1, "tool": "db__query", "args": map[string]any{"secret": "SECRET_ARGUMENT_VALUE"}})
	add(session.EventToolResult, map[string]any{"id": 1, "parent_seq": c.Seq, "is_error": true, "result": map[string]any{"message": "SECRET_MESSAGE_VALUE"}})
	if complete {
		add(session.EventSessionEnd, map[string]any{"reason": "done"})
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
}
func executeBehaviour(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := newBehaviourCmd()
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}
func TestM7CLIJSONAndPartial(t *testing.T) {
	dir := t.TempDir()
	workflowFixture(t, dir, "safe.jsonl", true)
	workflowFixture(t, dir, "unfinished.jsonl", false)
	out, err := executeBehaviour(t, "failures", "--dir", dir, "--json", "safe.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "SECRET_") {
		t.Fatal("recorded content leaked", out)
	}
	var data map[string]any
	if err = json.Unmarshal([]byte(out), &data); err != nil {
		t.Fatal(err)
	}
	if data["status"] != "complete" || data["schema_version"].(float64) != 1 {
		t.Fatal(data)
	}
	out, err = executeBehaviour(t, "summary", "--dir", dir, "--json", "unfinished.jsonl")
	if !errors.Is(err, errIncompleteBehaviour) {
		t.Fatal("expected partial", err)
	}
	if json.Unmarshal([]byte(out), &data) != nil || data["status"] != "partial" {
		t.Fatal(out)
	}
	if _, err = executeBehaviour(t, "summary", "--dir", dir, "../outside.jsonl"); err == nil {
		t.Fatal("accepted traversal")
	}
}
func TestM7CLIComparisonAndLinks(t *testing.T) {
	dir := t.TempDir()
	workflowFixture(t, dir, "one space.jsonl", true)
	workflowFixture(t, dir, "two.jsonl", true)
	out, err := executeBehaviour(t, "compare", "--dir", dir, "--reference", "one space.jsonl", "--candidate", "two.jsonl", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(out), &body); err != nil {
		t.Fatal(err)
	}
	links := body["result"].(map[string]any)["workbench_links"].(map[string]any)
	if !strings.Contains(links["comparison"].(string), "#behaviour&a=one+space.jsonl") || strings.Contains(out, "SECRET_") {
		t.Fatal(out)
	}
}
