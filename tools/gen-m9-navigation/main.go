// gen-m9-navigation writes one fictional long recording for UI pagination
// walkthroughs. It allocates a fresh filename and never starts a tool server.
package main

import (
	"fmt"
	"os"

	"github.com/Caseymccallum/tapelog/internal/session"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: go run ./tools/gen-m9-navigation <fictional-recordings-directory>")
		os.Exit(2)
	}
	w, err := session.NewWriterInDir(os.Args[1], "fictional-m9-long")
	if err != nil {
		panic(err)
	}
	add := func(kind session.EventType, p any) *session.Event {
		e, err := w.Append(kind, p)
		if err != nil {
			panic(err)
		}
		return e
	}
	add(session.EventSessionStart, map[string]any{"harness": "fictional-long-session", "task": "M9 long-recording navigation"})
	for i := 1; i <= 2000; i++ {
		e := add(session.EventToolCall, map[string]any{"id": i, "tool": "fixture__read", "args": map[string]any{"block": i}})
		add(session.EventPolicyDecision, map[string]any{"id": i, "parent_seq": e.Seq, "verdict": "allow"})
		add(session.EventToolResult, map[string]any{"id": i, "parent_seq": e.Seq, "result": map[string]any{"content": []any{map[string]any{"type": "text", "text": fmt.Sprintf("Fictional block %d", i)}}}})
	}
	add(session.EventSessionEnd, map[string]any{"reason": "fictional fixture complete"})
	if err := w.Close(); err != nil {
		panic(err)
	}
	fmt.Printf("Wrote %s: 2,000 fictional calls / 6,002 events\n", w.Path())
}
