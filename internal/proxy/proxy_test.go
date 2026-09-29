package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cassette-ai/cassette/internal/jsonrpc"
)

// syncBuffer is a goroutine-safe output sink for tests.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

const callLine = `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"delete_all","arguments":{}}}`

func waitRun(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not finish")
	}
}

func TestDenyShortCircuit(t *testing.T) {
	var clientOut, serverOut syncBuffer

	done := make(chan error, 1)
	go func() {
		done <- Run(context.Background(),
			strings.NewReader(callLine+"\n"), &clientOut,
			strings.NewReader(""), &serverOut,
			Hooks{OnToolCall: func(id json.RawMessage, call *jsonrpc.ToolCallParams) (Decision, *DenyData) {
				return DecisionDeny, &DenyData{
					Code: "tool_denied", RuleID: "deny-delete",
					Reason: "deletion is forbidden", Verdict: "deny",
				}
			}})
	}()
	waitRun(t, done)

	if serverOut.String() != "" {
		t.Fatalf("denied call reached the server: %q", serverOut.String())
	}
	out := clientOut.String()
	if !strings.Contains(out, `"code":-32010`) || !strings.Contains(out, "tool_denied") {
		t.Fatalf("client did not receive structured deny response: %q", out)
	}
}

func TestAllowForwardsAndReportsResult(t *testing.T) {
	var clientOut, serverOut syncBuffer
	resultLine := `{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"ok"}]}}`

	var mu sync.Mutex
	gotResultID, gotResult := "", ""

	done := make(chan error, 1)
	go func() {
		done <- Run(context.Background(),
			strings.NewReader(callLine+"\n"), &clientOut,
			strings.NewReader(resultLine+"\n"), &serverOut,
			Hooks{
				OnToolCall: func(id json.RawMessage, call *jsonrpc.ToolCallParams) (Decision, *DenyData) {
					if call.Name != "delete_all" {
						t.Errorf("tool name = %q", call.Name)
					}
					return DecisionAllow, nil
				},
				OnToolResult: func(id json.RawMessage, isError bool, result json.RawMessage) {
					mu.Lock()
					defer mu.Unlock()
					gotResultID, gotResult = string(id), string(result)
				},
			})
	}()
	waitRun(t, done)

	if !strings.Contains(serverOut.String(), `"tools/call"`) {
		t.Fatalf("call was not forwarded to server: %q", serverOut.String())
	}
	if !strings.Contains(clientOut.String(), `"text":"ok"`) {
		t.Fatalf("result was not forwarded to client: %q", clientOut.String())
	}
	mu.Lock()
	defer mu.Unlock()
	if gotResultID != "1" {
		t.Fatalf("OnToolResult id = %q, want 1", gotResultID)
	}
	if !strings.Contains(gotResult, "ok") {
		t.Fatalf("OnToolResult payload = %q", gotResult)
	}
}

func TestNonToolTrafficPassesThrough(t *testing.T) {
	var clientOut, serverOut syncBuffer
	line := `{"jsonrpc":"2.0","id":7,"method":"resources/read","params":{"uri":"file:///x"}}`
	done := make(chan error, 1)
	go func() {
		done <- Run(context.Background(),
			strings.NewReader(line+"\n"), &clientOut,
			strings.NewReader(""), &serverOut,
			Hooks{OnToolCall: func(id json.RawMessage, call *jsonrpc.ToolCallParams) (Decision, *DenyData) {
				t.Errorf("OnToolCall must not fire for resources/read")
				return DecisionAllow, nil
			}})
	}()
	waitRun(t, done)

	if !strings.Contains(serverOut.String(), "resources/read") {
		t.Fatalf("non-tool request was not forwarded: %q", serverOut.String())
	}
}
