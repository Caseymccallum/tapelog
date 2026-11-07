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
