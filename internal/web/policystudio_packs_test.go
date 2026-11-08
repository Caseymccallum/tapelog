package web

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Caseymccallum/tapelog/internal/policy"
	"github.com/Caseymccallum/tapelog/internal/session"
	"github.com/Caseymccallum/tapelog/packs"
)

// studioCall is one fictional recorded tool call.
type studioCall struct {
	tool string
	args map[string]any
}

// studioCallsFixture records several calls, each with an allow decision and a
// result, so the Studio can simulate a candidate against them.
func studioCallsFixture(t *testing.T, dir, name string, calls []studioCall) *recording {
	t.Helper()
	path := filepath.Join(dir, name)
	w, err := session.NewWriter(path, name)
	if err != nil {
		t.Fatal(err)
	}
	add := func(kind session.EventType, payload any) *session.Event {
		e, err := w.Append(kind, payload)
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	add(session.EventSessionStart, map[string]any{"task": "fictional"})
	for i, c := range calls {
		id := i + 1
		call := add(session.EventToolCall, map[string]any{"id": id, "tool": c.tool, "args": c.args})
		add(session.EventPolicyDecision, map[string]any{
			"id": id, "parent_seq": call.Seq, "verdict": "allow", "rule_id": "default",
			"reason": "no rule matched; policy default is allow",
		})
		add(session.EventToolResult, map[string]any{"id": id, "parent_seq": call.Seq, "result": map[string]any{"ok": true}})
	}
	add(session.EventSessionEnd, map[string]any{"reason": "complete"})
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return decodeRecording(data, name)
}

// TestStudioReportsNoDeadConditionsInShippedPacks is the Phase 3 half of the
// M5.1 repair: the Studio must stop calling repaired conditions ineffective,
// while remaining the thing that finds such patterns.
func TestStudioReportsNoDeadConditionsInShippedPacks(t *testing.T) {
	h := DirHandler(t.TempDir(), "")
	for _, p := range packs.All() {
		rw, data := requestJSON(t, h, "GET", "/api/policy/inspect?source=pack:"+p.Name, nil)
		if rw.Code != 200 {
			t.Fatalf("%s: status %d %s", p.Name, rw.Code, rw.Body.String())
		}
		var analysis policy.Analysis
		if err := json.Unmarshal(data["analysis"], &analysis); err != nil {
			t.Fatalf("%s: %v", p.Name, err)
		}
		if analysis.DeadConditions != 0 {
			for _, r := range analysis.Rules {
				for _, c := range r.Conditions {
					if c.Problem != "" {
						t.Errorf("%s rule %q: %s", p.Name, r.ID, c.Problem)
					}
				}
			}
			t.Fatalf("%s: %d dead condition(s) reported", p.Name, analysis.DeadConditions)
		}
		for _, w := range analysis.Warnings {
			if strings.Contains(w, "cannot match") {
				t.Errorf("%s: the Studio still warns %q", p.Name, w)
			}
		}
	}
}
