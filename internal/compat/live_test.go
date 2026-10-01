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
//	TAPELOG_COMPAT_LIVE="npx -y @modelcontextprotocol/server-filesystem@<pinned> /tmp" \
//	  go test ./internal/compat/ -run Live -v
func liveCommand(t *testing.T) []string {
	spec := os.Getenv("TAPELOG_COMPAT_LIVE")
	if spec == "" {
		t.Skip("TAPELOG_COMPAT_LIVE not set — hermetic tier gates CI; live tier is opt-in")
	}
	return strings.Fields(spec)
}

// TestLiveStdio runs the full loop against a real MCP server over stdio.
// Assertions are the loop invariants that must hold for ANY compliant
// server: the session verifies, redaction holds, and what-if reproduces
// the live verdicts for the policy's per-call rules (value-taint and
// tool-specific denies depend on the server's actual tools and are
// asserted only when the catalog contains them).
func TestLiveStdio(t *testing.T) {
	cmd := liveCommand(t)
	workDir := t.TempDir()
	rep, err := RunCell(context.Background(), Cell{
		Name: "Real-world server", Era: "live", Transport: TransportStdio, Command: cmd,
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
	if rep.WhatIfChanged != 0 {
		t.Errorf("what-if must reproduce live verdicts against real servers, %d changed:\n%s",
			rep.WhatIfChanged, rep.WhatIfOut)
	}
	t.Logf("live cell: %d interactions, %d unanswered, denies: %v",
		rep.Interactions, rep.Unanswered, rep.DenyRules)
}
