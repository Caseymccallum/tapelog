package web

import (
	"encoding/json"
	"github.com/Caseymccallum/tapelog/internal/replay"
	"github.com/Caseymccallum/tapelog/internal/session"
	"path/filepath"
	"testing"
	"time"
)

func TestM3AttemptsCompareUnansweredAndRepeated(t *testing.T) {
	a := &replay.Tape{Unanswered: []replay.Interaction{{Order: 1, Tool: "delete", ArgsHash: "abc", Verdict: "deny"}}}
	b := &replay.Tape{Unanswered: []replay.Interaction{{Order: 1, Tool: "delete", ArgsHash: "abc", Verdict: "allow"}, {Order: 2, Tool: "delete", ArgsHash: "abc", Verdict: "deny"}}}
	v := compareAttempts(a, b)
	if v["change_count"].(int) != 2 || !v["ambiguous_duplicates"].(bool) || !v["sequence_differs"].(bool) {
		t.Fatalf("unexpected comparison: %+v", v)
	}
}
func TestM3OverviewAndLazyEvents(t *testing.T) {
	dir := t.TempDir()
	writeWorkbenchFixture(t, dir, "sample.jsonl", "deny", "error", time.Now())
	h := DirHandler(dir, "")
	rw, data := requestJSON(t, h, "GET", "/api/activity?file=sample.jsonl", nil)
	if rw.Code != 200 {
		t.Fatal(rw.Body.String())
	}
	var findings []map[string]any
	if json.Unmarshal(data["findings"], &findings) != nil || len(findings) != 2 || findings[0]["seq"].(float64) != 3 || findings[1]["seq"].(float64) != 6 {
		t.Fatalf("findings: %s", data["findings"])
	}
	_, data = requestJSON(t, h, "GET", "/api/events?file=sample.jsonl&summary=1", nil)
	var events []eventView
	_ = json.Unmarshal(data["events"], &events)
	for _, e := range events {
		if e.Raw != "" {
			t.Fatal("summary included payload")
		}
	}
}
func TestM3ScenarioSemantics(t *testing.T) {
	rec := &recording{Activities: []*activity{{Seq: 1, Tool: "read", ResultSeq: 2}, {Seq: 3, Tool: "write", ResultSeq: 4}}}
	zero, err := buildScenario(rec, 1, "no_attempts", "fixture.jsonl")
	if err != nil || *zero.Assert[0].Attempted.Times != 0 {
		t.Fatal(zero, err)
	}
	seq, err := buildScenario(rec, 1, "sequence", "fixture.jsonl", 1, 3)
	if err != nil || len(seq.Assert[0].Sequence) != 2 {
		t.Fatal(seq, err)
	}
	if reversed, err := buildScenario(rec, 1, "sequence", "fixture.jsonl", 3, 1); err != nil || reversed.Assert[0].Sequence[0] != "write" {
		t.Fatal("explicit order not preserved", err)
	}
	if _, err = buildScenario(rec, 1, "sequence", "fixture.jsonl", 1, 1); err == nil {
		t.Fatal("accepted repeated event")
	}
	if _, err = buildScenario(rec, 1, "failure_absent", "fixture.jsonl"); err == nil {
		t.Fatal("accepted unsupported error assertion")
	}
}

// Synthetic workload measures response size and exercises late-event navigation.
func TestM3LargeSyntheticRecording(t *testing.T) {
	dir := t.TempDir()
	w, err := session.NewWriter(filepath.Join(dir, "large.jsonl"), "fictional")
	if err != nil {
		t.Fatal(err)
	}
	appendEvent := func(kind session.EventType, p any) {
		if _, err := w.Append(kind, p); err != nil {
			t.Fatal(err)
		}
	}
	appendEvent(session.EventSessionStart, map[string]any{"task": "Synthetic large session"})
	for i := 1; i <= 2000; i++ {
		appendEvent(session.EventToolCall, map[string]any{"id": i, "tool": "files__read", "args": map[string]any{"path": "fictional.txt", "padding": string(make([]byte, 1024))}})
		appendEvent(session.EventToolResult, map[string]any{"id": i, "result": "fictional"})
	}
	appendEvent(session.EventSessionEnd, map[string]any{"reason": "done"})
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	h := DirHandler(dir, "")
	start := time.Now()
	summary, _ := requestJSON(t, h, "GET", "/api/events?file=large.jsonl&offset=1000&limit=50&summary=1", nil)
	summaryTime := time.Since(start)
	start = time.Now()
	full, _ := requestJSON(t, h, "GET", "/api/events?file=large.jsonl&offset=1000&limit=50", nil)
	fullTime := time.Since(start)
	if summary.Code != 200 || full.Code != 200 || summary.Body.Len() >= full.Body.Len() {
		t.Fatalf("summary=%d full=%d", summary.Body.Len(), full.Body.Len())
	}
	start = time.Now()
	detail, _ := requestJSON(t, h, "GET", "/api/event?file=large.jsonl&seq=4000", nil)
	if detail.Code != 200 {
		t.Fatal(detail.Body.String())
	}
	t.Logf("4002 events: 50-row summary %d bytes in %s; full %d bytes in %s; late detail in %s", summary.Body.Len(), summaryTime, full.Body.Len(), fullTime, time.Since(start))
}
