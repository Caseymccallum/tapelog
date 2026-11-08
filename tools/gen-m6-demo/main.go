// Command gen-m6-demo writes the synthetic recording library used by the M6
// review document and the browser walkthrough.
//
// Every value here is fictional. The recordings are built with the real session
// writer, so they have valid hash chains and behave exactly like captured
// evidence: `tapelog verify` accepts them and `tapelog web --dir` reads them the
// same way it reads real sessions.
//
//	go run ./tools/gen-m6-demo m6-demo-sessions
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Caseymccallum/tapelog/internal/session"
)

// call is one fictional tool call plus the decision and result around it.
type call struct {
	tool    string
	args    map[string]any
	verdict string // "" = no decision recorded
	rule    string
	reason  string
	result  any // nil = no recorded result
	isError bool
	gap     time.Duration
}

type recording struct {
	file     string
	task     string
	harness  string
	start    time.Time
	complete bool
	calls    []call
	discover bool
}

func main() {
	dir := "m6-demo-sessions"
	if len(os.Args) > 1 {
		dir = os.Args[1]
	}
	if err := generate(dir); err != nil {
		fail(err)
	}
}

func generate(dir string) error {
	// Refuse existing directories so fictional examples never overwrite recordings.
	if err := os.Mkdir(dir, 0o700); err != nil {
		return err
	}
	for _, r := range library() {
		if err := write(dir, r); err != nil {
			return err
		}
		fmt.Printf("wrote %s (%d calls)\n", filepath.Join(dir, r.file), len(r.calls))
	}
	return nil
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "gen-m6-demo:", err)
	os.Exit(1)
}

// library is the demo set. Each file isolates one behaviour question a
// developer would ask, so a walkthrough shows one idea at a time.
func library() []recording {
	day := func(d, hour int) time.Time {
		return time.Date(2026, 2, d, hour, 0, 0, 0, time.UTC)
	}
	pgError := func(message string) map[string]any {
		return map[string]any{"error": map[string]any{"code": -32000, "message": message}}
	}
	return []recording{
		{
			// The known-good run, used as the reference recording.
			file: "01-baseline-release.jsonl", task: "release 4.2", harness: "fictional-agent",
			start: day(20, 9), complete: true, discover: true,
			calls: []call{
				{tool: "files__read", args: map[string]any{"path": "/work/api/server.go"}, verdict: "allow", rule: "read-only", reason: "workspace read permitted", result: map[string]any{"bytes": 812}},
				{tool: "git__status", args: map[string]any{"repo": "/work/api"}, verdict: "allow", rule: "read-only", reason: "workspace read permitted", result: map[string]any{"clean": true}, gap: 4 * time.Second},
				{tool: "tests__run", args: map[string]any{"suite": "unit"}, verdict: "allow", rule: "default", reason: "no rule matched; policy default is allow", result: map[string]any{"passed": 214, "failed": 0}, gap: 30 * time.Second},
				{tool: "git__commit", args: map[string]any{"message": "release 4.2"}, verdict: "allow", rule: "default", reason: "no rule matched; policy default is allow", result: map[string]any{"commit": "abc1234"}, gap: 12 * time.Second},
				{tool: "deploy__apply", args: map[string]any{"channel": "stable"}, verdict: "confirm", rule: "deploy-needs-human", reason: "production deploy requires a human", result: map[string]any{"revision": "r-42"}, gap: 45 * time.Second},
			},
		},
		{
			// A retry loop: identical arguments, byte-identical error every time.
			file: "02-retry-loop.jsonl", task: "fix failing report", harness: "fictional-agent",
			start: day(27, 14), complete: true, discover: true,
			calls: []call{
				{tool: "files__read", args: map[string]any{"path": "/work/reports/query.sql"}, verdict: "allow", rule: "read-only", reason: "workspace read permitted", result: map[string]any{"bytes": 640}},
				{tool: "db__query", args: map[string]any{"sql": "select * from users"}, verdict: "allow", rule: "default", reason: "no rule matched; policy default is allow", result: pgError(`relation "users" does not exist`), isError: true, gap: 3 * time.Second},
				{tool: "db__query", args: map[string]any{"sql": "select * from users"}, verdict: "allow", rule: "default", reason: "no rule matched; policy default is allow", result: pgError(`relation "users" does not exist`), isError: true, gap: 2 * time.Second},
				{tool: "db__query", args: map[string]any{"sql": "select * from users"}, verdict: "allow", rule: "default", reason: "no rule matched; policy default is allow", result: pgError(`relation "users" does not exist`), isError: true, gap: 2 * time.Second},
				{tool: "db__query", args: map[string]any{"sql": "select * from users"}, verdict: "allow", rule: "default", reason: "no rule matched; policy default is allow", result: pgError(`relation "users" does not exist`), isError: true, gap: 2 * time.Second},
				{tool: "files__write", args: map[string]any{"path": "/work/reports/query.sql"}, verdict: "allow", rule: "default", reason: "no rule matched; policy default is allow", result: map[string]any{"written": true}, gap: 5 * time.Second},
			},
		},
		{
			// Legitimate polling: same call, changing output, slow cadence.
			file: "03-polling.jsonl", task: "wait for build", harness: "fictional-agent",
			start: day(28, 11), complete: true, discover: true,
			calls: []call{
				{tool: "jobs__status", args: map[string]any{"id": "job-77"}, verdict: "allow", rule: "read-only", reason: "workspace read permitted", result: map[string]any{"state": "queued"}},
				{tool: "jobs__status", args: map[string]any{"id": "job-77"}, verdict: "allow", rule: "read-only", reason: "workspace read permitted", result: map[string]any{"state": "running", "step": 1}, gap: 20 * time.Second},
				{tool: "jobs__status", args: map[string]any{"id": "job-77"}, verdict: "allow", rule: "read-only", reason: "workspace read permitted", result: map[string]any{"state": "running", "step": 2}, gap: 20 * time.Second},
				{tool: "jobs__status", args: map[string]any{"id": "job-77"}, verdict: "allow", rule: "read-only", reason: "workspace read permitted", result: map[string]any{"state": "running", "step": 3}, gap: 20 * time.Second},
				{tool: "jobs__status", args: map[string]any{"id": "job-77"}, verdict: "allow", rule: "read-only", reason: "workspace read permitted", result: map[string]any{"state": "running", "step": 4}, gap: 20 * time.Second},
				{tool: "jobs__status", args: map[string]any{"id": "job-77"}, verdict: "allow", rule: "read-only", reason: "workspace read permitted", result: map[string]any{"state": "succeeded"}, gap: 20 * time.Second},
				{tool: "tests__run", args: map[string]any{"suite": "integration"}, verdict: "allow", rule: "default", reason: "no rule matched; policy default is allow", result: map[string]any{"passed": 41, "failed": 0}, gap: 10 * time.Second},
			},
		},
		{
			// Repeated denials: the boundary refused every identical attempt.
			file: "04-denials.jsonl", task: "force a hotfix", harness: "fictional-agent",
			start: day(2, 8), complete: true, discover: true,
			calls: []call{
				{tool: "git__status", args: map[string]any{"repo": "/work/api"}, verdict: "allow", rule: "read-only", reason: "workspace read permitted", result: map[string]any{"clean": false}},
				{tool: "git__push", args: map[string]any{"remote": "origin", "force": true}, verdict: "deny", rule: "no-force-push", reason: "history destruction requires an explicit human decision", gap: 5 * time.Second},
				{tool: "git__push", args: map[string]any{"remote": "origin", "force": true}, verdict: "deny", rule: "no-force-push", reason: "history destruction requires an explicit human decision", gap: 3 * time.Second},
				{tool: "git__push", args: map[string]any{"remote": "origin", "force": true}, verdict: "deny", rule: "no-force-push", reason: "history destruction requires an explicit human decision", gap: 3 * time.Second},
				{tool: "git__push", args: map[string]any{"remote": "origin", "force": true}, verdict: "deny", rule: "no-force-push", reason: "history destruction requires an explicit human decision", gap: 3 * time.Second},
				{tool: "git__push", args: map[string]any{"remote": "origin"}, verdict: "allow", rule: "default", reason: "no rule matched; policy default is allow", result: map[string]any{"pushed": "main"}, gap: 8 * time.Second},
			},
		},
		{
			// Alternating work: read, write, read, write, read, write.
			file: "05-alternating.jsonl", task: "rename a field", harness: "fictional-agent",
			start: day(3, 10), complete: true, discover: true,
			calls: []call{
				{tool: "files__read", args: map[string]any{"path": "/work/api/types.go"}, verdict: "allow", rule: "read-only", reason: "workspace read permitted", result: map[string]any{"bytes": 410}},
				{tool: "files__write", args: map[string]any{"path": "/work/api/types.go"}, verdict: "allow", rule: "default", reason: "no rule matched; policy default is allow", result: map[string]any{"written": true}, gap: 6 * time.Second},
				{tool: "files__read", args: map[string]any{"path": "/work/api/types.go"}, verdict: "allow", rule: "read-only", reason: "workspace read permitted", result: map[string]any{"bytes": 412}, gap: 6 * time.Second},
				{tool: "files__write", args: map[string]any{"path": "/work/api/types.go"}, verdict: "allow", rule: "default", reason: "no rule matched; policy default is allow", result: map[string]any{"written": true}, gap: 6 * time.Second},
				{tool: "files__read", args: map[string]any{"path": "/work/api/types.go"}, verdict: "allow", rule: "read-only", reason: "workspace read permitted", result: map[string]any{"bytes": 415}, gap: 6 * time.Second},
				{tool: "files__write", args: map[string]any{"path": "/work/api/types.go"}, verdict: "allow", rule: "default", reason: "no rule matched; policy default is allow", result: map[string]any{"written": true}, gap: 6 * time.Second},
			},
		},
		{
			// The same failure as file 02, plus a tool that fails with a volatile
			// message: one group spans sessions, the other must not fragment.
			file: "06-mixed-failures.jsonl", task: "nightly report", harness: "fictional-agent",
			start: day(4, 22), complete: true, discover: true,
			calls: []call{
				{tool: "db__query", args: map[string]any{"sql": "select count(*) from users"}, verdict: "allow", rule: "default", reason: "no rule matched; policy default is allow", result: pgError(`relation "users" does not exist`), isError: true},
				{tool: "net__fetch", args: map[string]any{"url": "https://reports.internal/api"}, verdict: "allow", rule: "default", reason: "no rule matched; policy default is allow", result: map[string]any{"error": map[string]any{"code": -32001, "message": "timeout after 30s"}}, isError: true, gap: 30 * time.Second},
				{tool: "net__fetch", args: map[string]any{"url": "https://reports.internal/api"}, verdict: "allow", rule: "default", reason: "no rule matched; policy default is allow", result: map[string]any{"error": map[string]any{"code": -32001, "message": "timeout after 60s"}}, isError: true, gap: 60 * time.Second},
				{tool: "net__fetch", args: map[string]any{"url": "https://reports.internal/api"}, verdict: "allow", rule: "default", reason: "no rule matched; policy default is allow", result: map[string]any{"error": map[string]any{"code": -32001, "message": "timeout after 90s"}}, isError: true, gap: 90 * time.Second},
				{tool: "report__render", args: map[string]any{"month": "2026-01"}, verdict: "allow", rule: "default", reason: "no rule matched; policy default is allow", result: map[string]any{"pages": 12}, gap: 20 * time.Second},
			},
		},
		{
			// Interrupted: no session/end, and the last call was never answered.
			file: "07-interrupted.jsonl", task: "triage incident", harness: "fictional-agent",
			start: day(5, 3), complete: false, discover: true,
			calls: []call{
				{tool: "logs__tail", args: map[string]any{"service": "api", "lines": 200}, verdict: "allow", rule: "read-only", reason: "workspace read permitted", result: map[string]any{"lines": 200}},
				{tool: "logs__tail", args: map[string]any{"service": "api", "lines": 200}, verdict: "allow", rule: "read-only", reason: "workspace read permitted", result: map[string]any{"lines": 200}, gap: 5 * time.Second},
				{tool: "logs__tail", args: map[string]any{"service": "api", "lines": 200}, verdict: "allow", rule: "read-only", reason: "workspace read permitted", result: map[string]any{"lines": 201}, gap: 5 * time.Second},
				{tool: "shell__exec", args: map[string]any{"cmd": "systemctl restart api"}, verdict: "confirm", rule: "restart-needs-human", reason: "service restarts require a human", gap: 10 * time.Second},
			},
		},
		{
			// Discovery only: a connection that never invoked a tool.
			file: "08-discovery-only.jsonl", task: "first run", harness: "fictional-agent",
			start: day(6, 9), complete: true, discover: true,
		},
	}
}

func write(dir string, r recording) error {
	path := filepath.Join(dir, r.file)
	w, err := session.NewWriter(path, r.file)
	if err != nil {
		return err
	}
	now := r.start
	w.SetClock(func() time.Time { return now })
	add := func(kind session.EventType, payload any) (*session.Event, error) {
		now = now.Add(150 * time.Millisecond)
		return w.Append(kind, payload)
	}
	if _, err := add(session.EventSessionStart, map[string]any{
		"harness": r.harness, "task": r.task, "policy_id": "demo-policy", "policy_hash": "demo",
	}); err != nil {
		return err
	}
	if r.discover {
		if _, err := add(session.EventToolsList, session.ToolsListPayload{}); err != nil {
			return err
		}
	}
	for i, c := range r.calls {
		now = now.Add(c.gap)
		id := i + 1
		callEvent, err := add(session.EventToolCall, map[string]any{"id": id, "tool": c.tool, "args": c.args})
		if err != nil {
			return err
		}
		if c.verdict != "" {
			if _, err := add(session.EventPolicyDecision, map[string]any{
				"id": id, "parent_seq": callEvent.Seq, "verdict": c.verdict,
				"rule_id": c.rule, "reason": c.reason,
			}); err != nil {
				return err
			}
		}
		if c.result != nil {
			if _, err := add(session.EventToolResult, map[string]any{
				"id": id, "parent_seq": callEvent.Seq, "is_error": c.isError, "result": c.result,
			}); err != nil {
				return err
			}
		}
	}
	if r.complete {
		if _, err := add(session.EventSessionEnd, map[string]any{"reason": "task complete"}); err != nil {
			return err
		}
	}
	return w.Close()
}
