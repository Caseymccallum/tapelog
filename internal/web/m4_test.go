package web

import (
	"encoding/json"
	"github.com/Caseymccallum/tapelog/internal/replay"
	"github.com/Caseymccallum/tapelog/internal/scenario"
	"github.com/Caseymccallum/tapelog/internal/session"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func multiFixture(t *testing.T, dir, name, later string) *recording {
	t.Helper()
	path := filepath.Join(dir, name)
	w, err := session.NewWriter(path, name)
	if err != nil {
		t.Fatal(err)
	}
	add := func(kind session.EventType, p any) *session.Event {
		e, err := w.Append(kind, p)
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	add(session.EventSessionStart, map[string]any{"task": "fictional"})
	c := add(session.EventToolCall, map[string]any{"id": 1, "tool": "files__write", "args": map[string]any{"path": "[REDACTED]"}})
	add(session.EventPolicyDecision, map[string]any{"id": 1, "parent_seq": c.Seq, "verdict": "confirm", "rule_id": "review", "reason": "needs review"})
	add(session.EventPolicyDecision, map[string]any{"id": 1, "parent_seq": c.Seq, "verdict": later, "rule_id": "review", "reason": "later recorded decision"})
	add(session.EventToolResult, map[string]any{"id": 1, "parent_seq": c.Seq, "is_error": true, "result": map[string]any{"message": "fictional failure <script>"}})
	add(session.EventSessionEnd, map[string]any{"reason": "complete"})
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return decodeRecording(data, name)
}
func TestM4DecisionHistoryAndRecordedEvidence(t *testing.T) {
	dir := t.TempDir()
	a := multiFixture(t, dir, "a.jsonl", "allow")
	b := multiFixture(t, dir, "b.jsonl", "deny")
	at, err := workbenchTape(a)
	if err != nil {
		t.Fatal(err)
	}
	bt, err := workbenchTape(b)
	if err != nil {
		t.Fatal(err)
	}
	history := comparePolicyHistory(a, b, at, bt)
	if history["total"].(int) != 1 {
		t.Fatalf("history: %+v", history)
	}
	h := DirHandler(dir, "")
	rw, data := requestJSON(t, h, "GET", "/api/event?file="+url.QueryEscape("a.jsonl")+"&seq=2", nil)
	if rw.Code != 200 {
		t.Fatal(rw.Body.String())
	}
	var ev struct {
		Input, Result string
		Decisions     []struct{ Verdict string }
	}
	if err = json.Unmarshal(data["evidence"], &ev); err != nil {
		t.Fatal(err)
	}
	if len(ev.Decisions) != 2 || ev.Decisions[0].Verdict != "confirm" || ev.Decisions[1].Verdict != "allow" || !strings.Contains(ev.Input, "[REDACTED]") || !strings.Contains(ev.Result, "fictional failure") {
		t.Fatalf("evidence %+v", ev)
	}
}
func TestM4AttemptCeilingIsEngineAssertion(t *testing.T) {
	rec := &recording{Activities: []*activity{{Seq: 1, Tool: "danger", ResultSeq: 0}}}
	sc, err := buildScenario(rec, 1, "attempt_ceiling", "fixture")
	if err != nil {
		t.Fatal(err)
	}
	zero := 0
	sc.Assert[0].Attempted.Max = &zero
	tape := &replay.Tape{Unanswered: []replay.Interaction{{Order: 1, Tool: "danger"}}}
	if len(scenario.Run(sc, tape, nil)) == 0 {
		t.Fatal("unanswered attempt was not counted")
	}
	one := 1
	sc.Assert[0].Attempted.Max = &one
	if len(scenario.Run(sc, tape, nil)) != 0 {
		t.Fatal("ceiling of one should pass")
	}
}
