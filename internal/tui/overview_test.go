package tui

import (
	"github.com/Caseymccallum/tapelog/internal/session"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOverviewHonestEmptyStates(t *testing.T) {
	for _, tc := range []struct {
		sum  Summary
		want string
	}{
		{Summary{}, "No session events found"},
		{Summary{Events: 2, Discovery: 1}, "Only tool discovery observed"},
		{Summary{Events: 1, Started: true}, "No mediated tool calls captured"},
		{Summary{Events: 4, Calls: 1, Failed: 1}, "1 failed results"},
	} {
		got := Overview(tc.sum)
		for _, want := range []string{tc.want, "built-in agent tools are unobserved", "not checked by inspect"} {
			if !strings.Contains(got, want) {
				t.Fatalf("missing %q in %s", want, got)
			}
		}
	}
	if strings.Contains(Overview(Summary{Events: 2, Ended: true}), "may have been interrupted") {
		t.Fatal("completed session marked interrupted")
	}
}

func TestLoadRejectsMalformedLogAndCountsFailures(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	w, err := session.NewWriter(path, "dx")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Append(session.EventToolResult, session.ToolResultPayload{IsError: true}); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	_, sum, _, err := Load(path)
	if err != nil || sum.Failed != 1 {
		t.Fatalf("failure count %+v %v", sum, err)
	}
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	_, _ = f.WriteString("{broken\n")
	_ = f.Close()
	if _, _, _, err := Load(path); err == nil || !strings.Contains(err.Error(), "line 2") {
		t.Fatalf("malformed tail silently discarded: %v", err)
	}
}

// TestStructureVerdictNamesTheActualDefect guards the surface disagreement the
// M15 audit found: the CLI printed one canned sentence for every structurally
// invalid file, so a zero-event recording was described as having "mixed session
// ids" while `verify` and the workbench API named no_events. The rendered
// verdict must name the defect the shared checker actually reported.
func TestStructureVerdictNamesTheActualDefect(t *testing.T) {
	dir := t.TempDir()

	empty := filepath.Join(dir, "empty.jsonl")
	if err := os.WriteFile(empty, nil, 0600); err != nil {
		t.Fatal(err)
	}
	_, sum, _, err := Load(empty)
	if err != nil {
		t.Fatal(err)
	}
	if got := Overview(sum); !strings.Contains(got, "no_events") || strings.Contains(got, "mixed session ids") {
		t.Fatalf("empty file must be described as no_events, not mixed identities:\n%s", got)
	}

	// One identity, no session/start: hashes are wrong on purpose — structure and
	// integrity are independent, and this test is about the structural sentence.
	startless := filepath.Join(dir, "startless.jsonl")
	line := `{"v":0,"seq":2,"ts":"2026-01-01T00:00:00Z","type":"tools/list","session_id":"s1","payload":{"tools":[]},"prev_hash":"","hash":""}` + "\n"
	if err := os.WriteFile(startless, []byte(line), 0600); err != nil {
		t.Fatal(err)
	}
	_, sum, _, err = Load(startless)
	if err != nil {
		t.Fatal(err)
	}
	if got := Overview(sum); !strings.Contains(got, "no_start") {
		t.Fatalf("startless file must name no_start:\n%s", got)
	}

	// One start, no end: incomplete, and named as such.
	open := filepath.Join(dir, "open.jsonl")
	start := `{"v":0,"seq":1,"ts":"2026-01-01T00:00:00Z","type":"session/start","session_id":"s1","payload":{"harness":"t"},"prev_hash":"","hash":""}` + "\n"
	if err := os.WriteFile(open, []byte(start+line), 0600); err != nil {
		t.Fatal(err)
	}
	_, sum, _, err = Load(open)
	if err != nil {
		t.Fatal(err)
	}
	got := Overview(sum)
	if !strings.Contains(got, "INCOMPLETE") || !strings.Contains(got, "no_end") {
		t.Fatalf("open recording must be INCOMPLETE and name no_end:\n%s", got)
	}
	if !strings.Contains(got, "Completeness: open") {
		t.Fatalf("open recording must report completeness open:\n%s", got)
	}
}
