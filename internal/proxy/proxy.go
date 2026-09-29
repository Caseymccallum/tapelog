// Package proxy implements tapelog's transparent MCP relay: it sits
// between an agent harness (client side) and real MCP servers (server side),
// evaluating policy on tools/call traffic before it is forwarded and
// emitting hooks so every message can be recorded.
package proxy

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"

	"github.com/tapelog-dev/tapelog/internal/jsonrpc"
)

// Decision mirrors policy.Verdict semantics without importing policy:
// the proxy only needs to know whether to forward, and what to answer
// when it must not.
type Decision int

const (
	DecisionAllow Decision = iota
	DecisionDeny
)

// DenyData is the structured payload returned to the harness on deny.
type DenyData struct {
	Code    string `json:"code"`
	RuleID  string `json:"rule_id"`
	Reason  string `json:"reason"`
	Verdict string `json:"verdict"`
}

// Hooks are invoked by the relay. They must be safe for concurrent use.
type Hooks struct {
	// OnToolCall is called before a tools/call is forwarded. Returning
	// DecisionDeny short-circuits: the call never reaches the server and
	// denyData is returned to the harness as a JSON-RPC error.
	OnToolCall func(id json.RawMessage, call *jsonrpc.ToolCallParams) (Decision, *DenyData)

	// OnToolResult is called for each server response that answers a
	// previously seen tools/call request.
	OnToolResult func(id json.RawMessage, isError bool, result json.RawMessage)

	// OnRawMessage is called for every message in both directions,
	// direction is "c2s" (client→server) or "s2c" (server→client).
	OnRawMessage func(direction string, raw []byte)
}

// Run relays MCP traffic until client EOF, context cancellation, or error.
//
//	 clientIn/Out  — the harness side (e.g. os.Stdin/os.Stdout)
//	 serverIn/Out  — the MCP server side (e.g. subprocess pipes)
func Run(ctx context.Context, clientIn io.Reader, clientOut io.Writer, serverIn io.Reader, serverOut io.Writer, hooks Hooks) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var outMu sync.Mutex // serializes writes to clientOut (responses + denies)

	// toolIDs tracks in-flight tools/call request ids to pair results.
	var idMu sync.Mutex
	toolIDs := map[string]bool{}

	idKey := func(id json.RawMessage) string { return string(id) }

	go func() {
		<-ctx.Done()
		if c, ok := serverOut.(io.Closer); ok {
			_ = c.Close()
		}
	}()

	// pumpClientToServer inspects tools/call traffic and forwards the rest.
	pumpClientToServer := func() error {
		sc := bufio.NewScanner(clientIn)
		sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
		for sc.Scan() {
			line := append([]byte(nil), sc.Bytes()...)
			if len(line) == 0 {
				continue
			}
			if hooks.OnRawMessage != nil {
				hooks.OnRawMessage("c2s", line)
			}
			msg, err := jsonrpc.Parse(line)
			if err != nil {
				// Not parseable JSON-RPC: forward untouched (protocol safety).
				if _, err := serverOut.Write(append(line, '\n')); err != nil {
					return fmt.Errorf("write to server: %w", err)
				}
				continue
			}
			if msg.IsRequest() && msg.Method == "tools/call" {
				call, err := msg.ToolCall()
				if err == nil {
					idMu.Lock()
					toolIDs[idKey(msg.ID)] = true
					idMu.Unlock()
					if hooks.OnToolCall != nil {
						decision, denyData := hooks.OnToolCall(msg.ID, call)
						if decision == DecisionDeny {
							resp := jsonrpc.DenyResponse(msg.ID, denyData)
							out, _ := jsonrpc.Marshal(resp)
							outMu.Lock()
							_, werr := clientOut.Write(append(out, '\n'))
							outMu.Unlock()
							if werr != nil {
								return fmt.Errorf("write deny response: %w", werr)
							}
							continue // never forwarded
						}
					}
				}
			}
			if _, err := serverOut.Write(append(line, '\n')); err != nil {
				return fmt.Errorf("write to server: %w", err)
			}
		}
		return sc.Err()
	}

	// pumpServerToClient forwards responses and reports tool results.
	pumpServerToClient := func() error {
		sc := bufio.NewScanner(serverIn)
		sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
		for sc.Scan() {
			line := append([]byte(nil), sc.Bytes()...)
			if len(line) == 0 {
				continue
			}
			if hooks.OnRawMessage != nil {
				hooks.OnRawMessage("s2c", line)
			}
			msg, err := jsonrpc.Parse(line)
			if err == nil && msg.IsResponse() {
				idMu.Lock()
				isTool := toolIDs[idKey(msg.ID)]
				if isTool {
					delete(toolIDs, idKey(msg.ID))
				}
				idMu.Unlock()
				if isTool && hooks.OnToolResult != nil {
					// On error responses the error object is reported in
					// place of the result so recordings keep the evidence.
					res := msg.Result
					if msg.Error != nil {
						res, _ = json.Marshal(msg.Error)
					}
					hooks.OnToolResult(msg.ID, msg.Error != nil, res)
				}
			}
			outMu.Lock()
			_, werr := clientOut.Write(append(line, '\n'))
			outMu.Unlock()
			if werr != nil {
				return fmt.Errorf("write to client: %w", werr)
			}
		}
		return sc.Err()
	}

	errC := make(chan error, 2)
	go func() { errC <- pumpClientToServer() }()
	go func() { errC <- pumpServerToClient() }()

	// First pump failure ends the relay (client EOF is normal).
	err := <-errC
	cancel()
	<-errC
	if err == context.Canceled || err == io.EOF {
		return nil
	}
	return err
}
