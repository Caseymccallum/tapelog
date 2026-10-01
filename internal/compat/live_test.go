package compat

import (
	"context"
	"os"
	"strings"
	"testing"
)

// liveCommand returns the pinned real-world server command to test
// against, or "" to skip. The live tier is opt-in and scheduled — hermetic
// cells gate CI; this tier produces evidence against real infrastructure
// without ever blocking it (see docs/ROADMAP.md v0.4 compat lab).
//
//	TAPELOG_COMPAT_LIVE="npx -y @modelcontextprotocol/server-filesystem@2026.8.31 /tmp" \
//	  go test ./internal/compat/ -run Live -v
//
// The conversation is catalog-driven (liveconv.go): safe-by-construction
// probes generated from the server's real tools — never a blind fixture
// conversation that would execute write_file for real.
func liveCommand(t *testing.T) []string {
	spec := os.Getenv("TAPELOG_COMPAT_LIVE")
	if spec == "" {
		t.Skip("TAPELOG_COMPAT_LIVE not set — hermetic tier gates CI; live tier is opt-in")
	}
	return strings.Fields(spec)
}

// TestLiveStdio runs the full loop against a real MCP server over stdio.
// Assertions are the loop invariants that must hold for ANY compliant
// server: the session verifies, redaction holds when probed, and what-if
// reproduces the live verdicts. Tool-specific denies are asserted only
// when the catalog contains the matching tool (via Report.Plan).
func TestLiveStdio(t *testing.T) {
	cmd := liveCommand(t)
	workDir := t.TempDir()
	rep, err := RunCell(context.Background(), Cell{
		Name: "Real-world server", Era: "live", Transport: TransportStdio,
		Command: cmd, Adaptive: true,
	}, workDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range rep.Errs {
		t.Error(e)
	}
	if !rep.VerifyOK {
		t.Errorf("session log failed verification: %s", rep.VerifyProblem)
	}
	if !rep.RedactedOK {
		t.Error("redaction contract broken: canary not masked or raw canary leaked into the log")
	}
	if rep.WhatIfChanged != 0 {
		t.Errorf("what-if must reproduce live verdicts against real servers, %d changed:\n%s",
			rep.WhatIfChanged, rep.WhatIfOut)
	}

	// Plan-conditional assertions: only what the catalog actually offered.
	if rep.Plan == nil {
		t.Fatal("adaptive cell must produce a plan")
	}
	t.Logf("catalog: %v", rep.Plan.Catalog)
	t.Logf("probes:  %v", rep.Plan.Probes)
	denies := strings.Join(rep.DenyRules, ",")
	for _, want := range rep.Plan.WantDenies {
		if !strings.Contains(denies, want) {
			t.Errorf("expected deny rule %q did not fire (got %q)", want, denies)
		}
	}
	if rep.Plan.TaintSink != "" && !strings.Contains(denies, "no-secret-exfil") {
		t.Errorf("taint sink %s was planned but flow deny did not fire (got %q)",
			rep.Plan.TaintSink, denies)
	}
	t.Logf("live cell: %d interactions, %d unanswered, denies: %v",
		rep.Interactions, rep.Unanswered, rep.DenyRules)
}
