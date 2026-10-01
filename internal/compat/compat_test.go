package compat

import (
	"context"
	"fmt"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

var (
	buildOnce sync.Once
	fakeCmd   string // path to the built fakecmd binary
	buildErr  error
)

// buildFakeCmd compiles the hermetic stdio server once per test run.
func buildFakeCmd(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		dir, err := os.MkdirTemp("", "tapelog-compat-*")
		if err != nil {
			buildErr = err
			return
		}
		out := filepath.Join(dir, "fakecmd")
		if runtime.GOOS == "windows" {
			out += ".exe"
		}
		cmd := exec.Command("go", "build", "-o", out, "./fakecmd")
		if b, err := cmd.CombinedOutput(); err != nil {
			buildErr = fmt.Errorf("build fakecmd: %v: %s", err, b)
			return
		}
		fakeCmd = out
	})
	if buildErr != nil {
		t.Fatal(buildErr)
	}
	return fakeCmd
}

// TestAdaptiveAgainstFixtureServer drives the REAL-WORLD tier's adaptive
// conversation against the fakecmd fixture server (whose catalog has
// delete_file / read_secrets / send_http): the plan must fire the rule
// deny AND the value-taint deny through the safe-by-construction probes,
// with what-if parity intact. This is the live tier's logic proven
// hermetically.
func TestAdaptiveAgainstFixtureServer(t *testing.T) {
	bin := buildFakeCmd(t)
	workDir := t.TempDir()
	rep, err := RunCell(context.Background(), Cell{
		Name: "Fixture server", Era: Era2026, Transport: TransportStdio,
		Command: []string{bin, "-era", string(Era2026)}, Adaptive: true,
	}, workDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range rep.Errs {
		t.Error(e)
	}
	if rep.Plan == nil {
		t.Fatal("adaptive cell must produce a plan")
	}
	// fakecmd's catalog covers both deny shapes: both must fire.
	denies := strings.Join(rep.DenyRules, ",")
	if !strings.Contains(denies, "no-delete") {
		t.Errorf("rule deny missing (got %q)", denies)
	}
	if rep.Plan.TaintSink == "" {
		t.Fatal("taint sink must be planned against fakecmd (read_secrets + send_http exist)")
	}
	if !strings.Contains(denies, "no-secret-exfil") {
		t.Errorf("value-taint deny missing (got %q)", denies)
	}
	if !rep.Plan.Redaction || !rep.RedactedOK {
		t.Error("redaction probe must run and hold against fakecmd")
	}
	if !rep.VerifyOK {
		t.Errorf("session log failed verification: %s", rep.VerifyProblem)
	}
	if rep.WhatIfChanged != 0 {
		t.Errorf("what-if parity broken on adaptive conversation, %d changed:\n%s",
			rep.WhatIfChanged, rep.WhatIfOut)
	}
	// The taint source produced a result; the sink never executed.
	if rep.Unanswered < 2 {
		t.Errorf("both deny probes must be unanswered (got %d)", rep.Unanswered)
	}
}

// TestHermeticMatrix is the CI-blocking compatibility matrix: every
// (era x transport) cell runs the full tapelog loop against an era-variant
// scripted server and must reproduce verdicts, redact, record, verify,
// and replay cleanly.
func TestHermeticMatrix(t *testing.T) {
	for _, era := range Eras {
		for _, tr := range []Transport{TransportStdio, TransportHTTP} {
			era, tr := era, tr
			t.Run(fmt.Sprintf("%s/%s", era, tr), func(t *testing.T) {
				cell := Cell{Name: "Test server", Era: era, Transport: tr}
				workDir := t.TempDir()

				var srv *Server
				var stop func()
				if tr == TransportStdio {
					bin := buildFakeCmd(t)
					cell.Command = []string{bin, "-era", string(era)}
				} else {
					srv = NewServer(era)
					hts := httptest.NewServer(srv.HTTPHandler())
					stop = hts.Close
					cell.URL = hts.URL
				}
				if stop != nil {
					defer stop()
				}

				rep, err := RunCell(context.Background(), cell, workDir)
				if err != nil {
					t.Fatal(err)
				}
				for _, e := range rep.Errs {
					t.Error(e)
				}

				// The loop's substance, every cell:
				if !rep.VerifyOK {
					t.Errorf("session log failed verification: %s", rep.VerifyProblem)
				}
				if !rep.RedactedOK {
					t.Error("redaction contract broken: secret-shaped args not masked or raw secret leaked into the log")
				}
				if rep.WhatIfChanged != 0 {
					t.Errorf("what-if must reproduce live verdicts, %d changed:\n%s", rep.WhatIfChanged, rep.WhatIfOut)
				}
				if rep.Interactions < 5 {
					t.Errorf("tape too small: %d interactions", rep.Interactions)
				}
				// Rule deny AND value-taint deny both fired.
				denies := strings.Join(rep.DenyRules, ",")
				if !strings.Contains(denies, "no-delete") {
					t.Errorf("rule deny missing (got %q)", denies)
				}
				if !strings.Contains(denies, "no-secret-exfil") {
					t.Errorf("value-taint deny missing (got %q)", denies)
				}
				// Server error path recorded.
				if !strings.Contains(rep.HarnessOut, "boom") {
					t.Error("server error (boom) did not reach the harness")
				}

				// Era-specific wire assertions (HTTP cells expose the
				// in-process observation; stdio cells assert on harness I/O).
				if tr == TransportHTTP {
					obs := srv.Observations()
					if era == Era2026 {
						if obs.DiscoverResult != "discover-result" {
							t.Errorf("modern server should answer server/discover, got %q", obs.DiscoverResult)
						}
						if obs.InitializedNotification {
							t.Error("modern era must not receive notifications/initialized")
						}
						if !obs.InputResponses {
							t.Error("MRTR inputResponses never reached the upstream (params dropped?)")
						}
						for _, m := range []string{"tools/list", "tools/call"} {
							if !obs.MetaOn[m] {
								t.Errorf("request %s missing _meta self-description", m)
							}
						}
						if obs.Headers["MCP-Protocol-Version"] == "" || obs.Headers["Mcp-Method"] == "" {
							t.Errorf("request-metadata headers missing: %v", obs.Headers)
						}
					} else {
						if obs.DiscoverResult != "method-not-found" {
							t.Errorf("legacy server should not answer server/discover, got %q", obs.DiscoverResult)
						}
						if !obs.InitializedNotification {
							t.Error("legacy era must receive notifications/initialized")
						}
					}
				}
				if era == Era2026 {
					if !strings.Contains(rep.HarnessOut, `"resultType":"input_required"`) {
						t.Errorf("MRTR input_required missing from harness output:\n%s", rep.HarnessOut)
					}
				} else {
					if strings.Contains(rep.HarnessOut, "input_required") {
						t.Errorf("legacy era must not speak MRTR:\n%s", rep.HarnessOut)
					}
				}
			})
		}
	}
}
