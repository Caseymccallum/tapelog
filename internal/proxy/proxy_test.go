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

	// A real server answers only AFTER receiving the call; mimic that
	// causality (a pre-loaded response could race ahead of request
	// registration, which is impossible in production).
	serverR, serverW := io.Pipe()
	go func() {
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) && !strings.Contains(serverOut.String(), `"tools/call"`) {
			time.Sleep(5 * time.Millisecond)
		}
		fmt.Fprintln(serverW, resultLine)
		serverW.Close()
	}()

	var mu sync.Mutex
	gotResultID, gotResult := "", ""

	done := make(chan error, 1)
	go func() {
		done <- Run(context.Background(),
			strings.NewReader(callLine+"\n"), &clientOut,
			serverR, &serverOut,
			Hooks{
				OnToolCall: func(id json.RawMessage, call *jsonrpc.ToolCallParams) (Decision, *DenyData) {
					if call.Name != "delete_all" {
						t.Errorf("tool name = %q", call.Name)
					}
					return DecisionAllow, nil
				},
				OnToolResult: func(id json.RawMessage, isError bool, result json.RawMessage) (Decision, *DenyData) {
					mu.Lock()
					defer mu.Unlock()
					gotResultID, gotResult = string(id), string(result)
					return DecisionAllow, nil
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

func TestResultVetoReplacesResponse(t *testing.T) {
	var clientOut, serverOut syncBuffer
	resultLine := `{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"huge"}]}}`

	serverR, serverW := io.Pipe()
	go func() {
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) && !strings.Contains(serverOut.String(), `"tools/call"`) {
			time.Sleep(5 * time.Millisecond)
		}
		fmt.Fprintln(serverW, resultLine)
		serverW.Close()
	}()

	done := make(chan error, 1)
	go func() {
		done <- Run(context.Background(),
			strings.NewReader(callLine+"\n"), &clientOut,
			serverR, &serverOut,
			Hooks{
				OnToolCall: func(id json.RawMessage, call *jsonrpc.ToolCallParams) (Decision, *DenyData) {
					return DecisionAllow, nil
				},
				OnToolResult: func(id json.RawMessage, isError bool, result json.RawMessage) (Decision, *DenyData) {
					return DecisionDeny, &DenyData{
						Code: "response_too_large", RuleID: "limits.max_response_bytes",
						Reason: "too big", Verdict: "deny",
					}
				},
			})
	}()
	waitRun(t, done)

	out := clientOut.String()
	if strings.Contains(out, `"text":"huge"`) {
		t.Fatalf("vetoed result reached the harness: %q", out)
	}
	if !strings.Contains(out, "response_too_large") || !strings.Contains(out, `"code":-32010`) {
		t.Fatalf("harness did not receive the replacement error: %q", out)
	}
}

func TestCoreMethodPassesUnmediated(t *testing.T) {
	var clientOut, serverOut syncBuffer
	line := `{"jsonrpc":"2.0","id":7,"method":"ping","params":{}}`
	done := make(chan error, 1)
	go func() {
		done <- Run(context.Background(),
			strings.NewReader(line+"\n"), &clientOut,
			strings.NewReader(""), &serverOut,
			Hooks{OnToolCall: func(id json.RawMessage, call *jsonrpc.ToolCallParams) (Decision, *DenyData) {
				t.Errorf("OnToolCall must not fire for ping")
				return DecisionAllow, nil
			}})
	}()
	waitRun(t, done)

	if !strings.Contains(serverOut.String(), "ping") {
		t.Fatalf("core method was not forwarded: %q", serverOut.String())
	}
}

func TestSurfaceCallsAreMediated(t *testing.T) {
	// Deny path: a refused resources/read must never reach the server,
	// and must answer with the structured deny error.
	var clientOut, serverOut syncBuffer
	readLine := `{"jsonrpc":"2.0","id":8,"method":"resources/read","params":{"uri":"file:///secrets/key"}}`
	done := make(chan error, 1)
	var gotName, gotArgs string
	go func() {
		done <- Run(context.Background(),
			strings.NewReader(readLine+"\n"), &clientOut,
			strings.NewReader(""), &serverOut,
			Hooks{OnToolCall: func(id json.RawMessage, call *jsonrpc.ToolCallParams) (Decision, *DenyData) {
				gotName, gotArgs = call.Name, string(call.Arguments)
				return DecisionDeny, &DenyData{
					Code: "tool_denied", RuleID: "no-secrets",
					Reason: "secrets are out of bounds", Verdict: "deny",
				}
			}})
	}()
	waitRun(t, done)
	if gotName != "resources/read" || !strings.Contains(gotArgs, "file:///secrets/key") {
		t.Fatalf("surface call not mediated correctly: name=%q args=%q", gotName, gotArgs)
	}
	if serverOut.String() != "" {
		t.Fatalf("denied surface call reached the server: %q", serverOut.String())
	}
	if !strings.Contains(clientOut.String(), "no-secrets") || !strings.Contains(clientOut.String(), `"code":-32010`) {
		t.Fatalf("no structured deny for surface call: %q", clientOut.String())
	}

	// Allow path: prompts/get is forwarded and its result is paired
	// (recorded) like any tool result.
	var cOut, sOut syncBuffer
	promptLine := `{"jsonrpc":"2.0","id":9,"method":"prompts/get","params":{"name":"summarize"}}`
	resultLine := `{"jsonrpc":"2.0","id":9,"result":{"messages":[]}}`
	serverR, serverW := io.Pipe()
	go func() {
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) && !strings.Contains(sOut.String(), "prompts/get") {
			time.Sleep(5 * time.Millisecond)
		}
		fmt.Fprintln(serverW, resultLine)
		serverW.Close()
	}()
	var paired string
	done2 := make(chan error, 1)
	go func() {
		done2 <- Run(context.Background(),
			strings.NewReader(promptLine+"\n"), &cOut,
			serverR, &sOut,
			Hooks{
				OnToolCall: func(id json.RawMessage, call *jsonrpc.ToolCallParams) (Decision, *DenyData) {
					if call.Name != "prompts/get" {
						t.Errorf("name = %q", call.Name)
					}
					return DecisionAllow, nil
				},
				OnToolResult: func(id json.RawMessage, isError bool, result json.RawMessage) (Decision, *DenyData) {
					paired = string(result)
					return DecisionAllow, nil
				},
			})
	}()
	waitRun(t, done2)
	if !strings.Contains(sOut.String(), "prompts/get") {
		t.Fatalf("surface call was not forwarded: %q", sOut.String())
	}
	if !strings.Contains(paired, "messages") {
		t.Fatalf("surface result was not paired/recorded: %q", paired)
	}
}
