package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Caseymccallum/tapelog/internal/jsonrpc"
)

// A client may reuse a request id once (or while) calls are outstanding. Both
// responses must still be reported to the recorder: a boolean "in flight" flag
// cleared by the first response silently dropped the second result from the
// evidence, even though the client received it.
func TestReusedRequestIDReportsEveryResult(t *testing.T) {
	var clientOut, serverOut syncBuffer
	call := `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"read_file","arguments":{"path":"/a"}}}`
	results := []string{
		`{"jsonrpc":"2.0","id":7,"result":{"content":[{"type":"text","text":"first"}]}}`,
		`{"jsonrpc":"2.0","id":7,"result":{"content":[{"type":"text","text":"second"}]}}`,
	}

	serverR, serverW := io.Pipe()
	go func() {
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			if strings.Count(serverOut.String(), `"tools/call"`) >= 2 {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		for _, r := range results {
			fmt.Fprintln(serverW, r)
		}
		serverW.Close()
	}()

	var mu sync.Mutex
	var seen []string

	done := make(chan error, 1)
	go func() {
		done <- Run(context.Background(),
			strings.NewReader(call+"\n"+call+"\n"), &clientOut,
			serverR, &serverOut,
			Hooks{
				OnToolCall: func(id json.RawMessage, c *jsonrpc.ToolCallParams) (Decision, *DenyData) {
					return DecisionAllow, nil
				},
				OnToolResult: func(id json.RawMessage, isError bool, result json.RawMessage) (Decision, *DenyData) {
					mu.Lock()
					seen = append(seen, string(result))
					mu.Unlock()
					return DecisionAllow, nil
				},
			})
	}()
	waitRun(t, done)

	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 2 {
		t.Fatalf("both responses must be reported to the recorder, got %d: %v", len(seen), seen)
	}
	if !strings.Contains(seen[0], "first") || !strings.Contains(seen[1], "second") {
		t.Fatalf("results reported out of order or wrong: %v", seen)
	}
}

// A deny under a shared id must settle only ITS OWN outstanding entry. Deleting
// the whole id hid the other call's response from the recorder: the client saw
// two responses, the recording kept one.
func TestDenyUnderSharedIDKeepsOtherCallRecorded(t *testing.T) {
	var clientOut, serverOut syncBuffer
	call := `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"%s","arguments":{}}}`
	resp := `{"jsonrpc":"2.0","id":7,"result":{"content":[{"type":"text","text":"ok"}]}}`

	// The ALLOWED call is forwarded first and stays in flight; the DENIED call
	// is registered under the same id while it is still outstanding. That is
	// the state a whole-id delete destroys.
	var in bytes.Buffer
	fmt.Fprintln(&in, fmt.Sprintf(call, "read_file"))
	fmt.Fprintln(&in, fmt.Sprintf(call, "delete_all"))

	serverR, serverW := io.Pipe()
	go func() {
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) && !strings.Contains(serverOut.String(), "tools/call") {
			time.Sleep(5 * time.Millisecond)
		}
		fmt.Fprintln(serverW, resp)
		serverW.Close()
	}()

	var mu sync.Mutex
	results := 0
	done := make(chan error, 1)
	go func() {
		done <- Run(context.Background(), &in, &clientOut,
			serverR, &serverOut,
			Hooks{
				OnToolCall: func(id json.RawMessage, c *jsonrpc.ToolCallParams) (Decision, *DenyData) {
					if c.Name == "delete_all" {
						return DecisionDeny, &DenyData{Code: "tool_denied", RuleID: "no-delete", Reason: "no", Verdict: "deny"}
					}
					return DecisionAllow, nil
				},
				OnToolResult: func(id json.RawMessage, isError bool, result json.RawMessage) (Decision, *DenyData) {
					mu.Lock()
					results++
					mu.Unlock()
					return DecisionAllow, nil
				},
			})
	}()
	waitRun(t, done)

	mu.Lock()
	defer mu.Unlock()
	if results != 1 {
		t.Fatalf("the allowed call's response must still be reported, got %d result(s)", results)
	}
}
