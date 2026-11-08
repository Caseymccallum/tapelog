// workbench-demo creates fictional, hash-chained recordings for exploring the
// UI. It never starts a tool server and refuses an existing output directory.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/Caseymccallum/tapelog/internal/session"
	"os"
	"path/filepath"
	"time"
)

func main() {
	out := flag.String("out", "workbench-demo-sessions", "NEW directory for fictional recordings")
	flag.Parse()
	if err := os.Mkdir(*out, 0700); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	names := []string{"01-workspace-audit", "02-workspace-audit-fixed", "03-dependency-review", "04-discovery-only", "05-interrupted", "06-integrity-example", "07-untrusted-text"}
	for i, name := range names {
		path := filepath.Join(*out, name+".jsonl")
		w, err := session.NewWriter(path, fmt.Sprintf("fictional-m2-%d", i+1))
		if err != nil {
			panic(err)
		}
		clock := time.Date(2026, 11, 8, 9, 12+i*8, 0, 0, time.UTC)
		w.SetClock(func() time.Time { return clock })
		appendEvent := func(kind session.EventType, p any) *session.Event {
			clock = clock.Add(120 * time.Millisecond)
			e, err := w.Append(kind, p)
			if err != nil {
				panic(err)
			}
			return e
		}
		appendEvent(session.EventSessionStart, map[string]any{"harness": "fictional-demo", "task": []string{"Workspace audit", "Workspace audit · after fix", "Dependency review", "Server discovery", "Interrupted connection", "Integrity investigation", "Untrusted tool output"}[i]})
		appendEvent(session.EventToolsList, session.ToolsListPayload{Tools: []json.RawMessage{json.RawMessage(`{"name":"workspace__read_file","inputSchema":{"type":"object"}}`)}})
		if i != 3 {
			tools := []string{"workspace__list_files", "workspace__read_file", "packages__lookup", "workspace__delete_file"}
			if i == 2 {
				tools = []string{"packages__lookup", "packages__license", "workspace__write_file"}
			}
			for n, tool := range tools {
				call := appendEvent(session.EventToolCall, map[string]any{"id": n + 1, "tool": tool, "args": map[string]any{"path": "fictional-project/notes.md"}})
				verdict, rule, reason := "allow", "read-only-scope", "Read-only investigation is allowed."
				if tool == "workspace__delete_file" {
					verdict, rule, reason = "deny", "protect-project-files", "Deletion is outside the permitted read-only investigation. No execution evidence is inferred from this recording."
				}
				if tool == "workspace__write_file" {
					verdict, rule, reason = "confirm", "review-writes", "A human confirmation was required by the recorded policy."
				}
				appendEvent(session.EventPolicyDecision, map[string]any{"id": n + 1, "parent_seq": call.Seq, "verdict": verdict, "rule_id": rule, "reason": reason})
				if i == 2 && tool == "workspace__write_file" {
					appendEvent(session.EventPolicyDecision, map[string]any{"id": n + 1, "parent_seq": call.Seq, "verdict": "allow", "rule_id": "review-writes", "reason": "Fictional later decision recorded."})
				}
				if i == 4 && n == 1 {
					break
				}
				if verdict == "deny" {
					continue
				}
				clock = clock.Add(time.Duration(n+1) * 210 * time.Millisecond)
				text := "Fictional result: notes found"
				isError := i == 0 && n == 2
				if isError {
					text = "Fictional upstream error: package lookup timed out"
				}
				if i == 1 && n == 2 {
					text = "Fictional package lookup recovered"
				}
				if i == 6 {
					text = `<img src=x onerror="window.TAPELOG_XSS=true"> Synthetic secret: [REDACTED]`
				}
				appendEvent(session.EventToolResult, map[string]any{"id": n + 1, "parent_seq": call.Seq, "is_error": isError, "result": map[string]any{"content": []any{map[string]string{"type": "text", "text": text}}}})
			}
		}
		if i != 4 {
			appendEvent(session.EventSessionEnd, map[string]any{"reason": "fictional client disconnected"})
		}
		if err := w.Close(); err != nil {
			panic(err)
		}
		if i == 5 {
			data, err := os.ReadFile(path)
			if err != nil {
				panic(err)
			}
			data = bytes.Replace(data, []byte("fictional-demo"), []byte("fictional-xemo"), 1)
			if err := os.WriteFile(path, data, 0600); err != nil {
				panic(err)
			}
		}
	}
	abs, _ := filepath.Abs(*out)
	fmt.Println("Fictional recordings only:", abs)
	fmt.Println("Open with: tapelog web --dir <directory>")
}
