package web

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Caseymccallum/tapelog/internal/policy"
	"github.com/Caseymccallum/tapelog/internal/replay"
)

// TestStudioDescribesTheRepairedConditions asserts the Studio's descriptions
// match the corrected conditions: a single-clause pattern is explained in plain
// language, a compound `||` chain is left as source (never flattened), and
// nothing is reported as ineffective.
func TestStudioDescribesTheRepairedConditions(t *testing.T) {
	h := DirHandler(t.TempDir(), "")

	analysis := func(pack string) policy.Analysis {
		t.Helper()
		rw, data := requestJSON(t, h, "GET", "/api/policy/inspect?source=pack:"+pack, nil)
		if rw.Code != 200 {
			t.Fatalf("%s: status %d: %s", pack, rw.Code, rw.Body.String())
		}
		var a policy.Analysis
		if err := json.Unmarshal(data["analysis"], &a); err != nil {
			t.Fatalf("%s: %v", pack, err)
		}
		return a
	}
	rule := func(a policy.Analysis, id string) *policy.RuleView {
		for i := range a.Rules {
			if a.Rules[i].ID == id {
				return &a.Rules[i]
			}
		}
		t.Fatalf("rule %q missing", id)
		return nil
	}

	// shell: one folded compound expression, so it must stay as source.
	shell := analysis("shell")
	catastrophe := rule(shell, "no-catastrophe")
	if len(catastrophe.WhereRaw) != 1 {
		t.Fatalf("expected one folded expression, got %d", len(catastrophe.WhereRaw))
	}
	if strings.Contains(catastrophe.WhereRaw[0], "%") {
		t.Fatalf("the raw condition still contains a percent sign: %s", catastrophe.WhereRaw[0])
	}
	if !strings.Contains(catastrophe.WhereRaw[0], `like "*rm -rf /"`) {
		t.Fatalf("the corrected root pattern is missing: %s", catastrophe.WhereRaw[0])
	}
	// One `where` entry is one compound expression, so the Studio keeps it as
	// source instead of pretending it is a single comparison.
	if len(catastrophe.Conditions) != 1 {
		t.Fatalf("expected one condition view for the folded expression, got %d", len(catastrophe.Conditions))
	}
	cond := catastrophe.Conditions[0]
	if cond.Problem != "" {
		t.Errorf("condition reported as ineffective: %s", cond.Problem)
	}
	if cond.Simplified {
		t.Errorf("a compound expression must not be simplified: %q", cond.Summary)
	}
	if strings.Count(cond.Expr, "||") < 20 {
		t.Errorf("the pattern set looks truncated: %s", cond.Expr)
	}
	if !strings.Contains(catastrophe.Summary, "Block calls to") || !strings.Contains(catastrophe.Summary, "argument condition") {
		t.Fatalf("rule summary: %q", catastrophe.Summary)
	}

	// git: a single-clause pattern is explained in plain language.
	pushes := rule(analysis("git"), "pushes-confirm")
	if len(pushes.Conditions) != 1 || !pushes.Conditions[0].Simplified {
		t.Fatalf("a single like clause should be simplified: %+v", pushes.Conditions)
	}
	if !strings.Contains(pushes.Conditions[0].Summary, "contains") || !strings.Contains(pushes.Conditions[0].Summary, "push") {
		t.Fatalf("condition summary: %q", pushes.Conditions[0].Summary)
	}
}

// TestStudioWhatIfShowsTheRepairedDenies is the end-to-end proof for Phase 4:
// recorded calls that the packs previously allowed are now denied, and the
// Studio reports it through the authoritative evaluator.
func TestStudioWhatIfShowsTheRepairedDenies(t *testing.T) {
	dir := t.TempDir()
	rec := studioCallsFixture(t, dir, "regressions.jsonl", []studioCall{
		{tool: "bash", args: map[string]any{"command": "sudo rm -rf / --no-preserve-root"}},
		{tool: "run_git_command", args: map[string]any{"command": "git push --force origin main"}},
		{tool: "query", args: map[string]any{"query": "DROP TABLE users"}},
		{tool: "fetch", args: map[string]any{"url": "http://169.254.169.254/latest/meta-data/"}},
		{tool: "bash", args: map[string]any{"command": "ls -la"}},
		{tool: "run_git_command", args: map[string]any{"command": "git status"}},
		{tool: "query", args: map[string]any{"query": "SELECT * FROM t"}},
		{tool: "fetch", args: map[string]any{"url": "https://example.com/"}},
	})
	if !rec.Info.ChainOK || !rec.Info.Complete {
		t.Fatalf("fixture must be usable: %+v", rec.Info)
	}
	h := DirHandler(dir, "")
	calls := func(source string) replay.Simulation {
		t.Helper()
		rw, data := requestJSON(t, h, "POST", "/api/policy/simulate", map[string]any{
			"source": source, "file": "regressions.jsonl", "revision": rec.Info.Revision,
		})
		if rw.Code != 200 {
			t.Fatalf("%s: status %d %s", source, rw.Code, rw.Body.String())
		}
		var sim replay.Simulation
		if err := json.Unmarshal(data["simulation"], &sim); err != nil {
			t.Fatalf("%s: %v (%s)", source, err, data["simulation_error"])
		}
		return sim
	}

	// Each pack must deny its own prohibited call and leave the others alone.
	// The fixture records two calls per tool: one dangerous, one legitimate.
	type expectation struct {
		source string
		tool   string
		rule   string
	}
	for _, want := range []expectation{
		{"pack:shell", "bash", "no-catastrophe"},
		{"pack:git", "run_git_command", "no-history-destruction"},
		{"pack:postgres", "query", "no-schema-destruction"},
		{"pack:fetch", "fetch", "no-internal-urls"},
	} {
		sim := calls(want.source)
		denied := 0
		for _, c := range sim.Calls {
			if c.Tool != want.tool {
				continue
			}
			switch c.CandidateVerdict {
			case "deny":
				denied++
				if c.CandidateRule != want.rule {
					t.Errorf("%s: %s denied by %q, want %q", want.source, c.Tool, c.CandidateRule, want.rule)
				}
				if c.RecordedVerdict != "allow" || c.Transition != "allow->deny" {
					t.Errorf("%s: %s transition %q", want.source, c.Tool, c.Transition)
				}
			case "confirm":
				// shell's default is confirm; acceptable for the dangerous call
				// only if the rule did not apply, which the deny count catches.
			}
		}
		if denied != 1 {
			t.Errorf("%s: expected exactly one denied %s call, got %d", want.source, want.tool, denied)
		}
		if sim.NewlyDenied == 0 {
			t.Errorf("%s: no call was newly denied", want.source)
		}
	}

	// The headline M5 scenario, stated explicitly: a force-push that was
	// allowed before M5.1 is now denied by the history rule.
	sim := calls("pack:git")
	var found bool
	for _, c := range sim.Calls {
		if c.Tool == "run_git_command" && strings.Contains(c.CandidateReason, "history") {
			found = true
			if c.RecordedVerdict != "allow" || c.CandidateVerdict != "deny" {
				t.Fatalf("force push: recorded %s -> candidate %s", c.RecordedVerdict, c.CandidateVerdict)
			}
		}
	}
	if !found {
		t.Fatal("the force-push call was not denied by the history rule")
	}
	// The legitimate git call must not be denied by that rule.
	for _, c := range sim.Calls {
		if c.Tool == "run_git_command" && c.CandidateVerdict == "deny" && !strings.Contains(c.CandidateReason, "history") {
			t.Fatalf("a legitimate git call was denied: %+v", c)
		}
	}
}
