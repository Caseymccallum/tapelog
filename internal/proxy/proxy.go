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
	"strings"
	"sync"

	"github.com/Caseymccallum/tapelog/internal/jsonrpc"
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
	// previously seen tools/call request, BEFORE it reaches the harness.
	// Returning DecisionDeny replaces the response with a structured
	// error (used for payload caps); the original is still recorded.
	OnToolResult func(id json.RawMessage, isError bool, result json.RawMessage) (Decision, *DenyData)

	// OnMalformedCall is called when a tools/call request cannot be
	// parsed (bad params, missing tool name). The relay rejects such
	// requests with a structured JSON-RPC error and NEVER forwards them
	// (nothing crosses the boundary unmediated); this hook exists so the
	// rejection can be recorded as evidence.
	OnMalformedCall func(id json.RawMessage, params json.RawMessage, cause error)

	// OnRawMessage is called for every message in both directions,
	// direction is "c2s" (client→server) or "s2c" (server→client).
	OnRawMessage func(direction string, raw []byte)
}

// mediatedCall extracts the mediated call from a client→server request.
// tools/call yields the tool call; every other REQUEST except the protocol
// plumbing set is treated as a surface call named after its method
// (resources/read, prompts/get, ...), so nothing that can feed the agent's
// context crosses the boundary unmediated.
//
// Returns (nil, nil) for plumbing/notifications — forward untouched.
// Returns (nil, err) for a MALFORMED tools/call — the caller must reject
// it (JSON-RPC -32602) and must NOT forward it: a call the boundary
// cannot parse is a call the boundary cannot mediate, and the product
// promise is that calls crossing the boundary are subject to it.
//
// The plumbing set is protocol machinery that carries no agent-facing
// payload and MUST NOT be policy-gated (a deny would break compliant
// clients before they reach the tool layer):
//
//	initialize    — legacy handshake (pre-2026-07-28 clients)
//	ping          — liveness (removed in 2026-07-28; legacy clients still send it)
//	tools/list    — catalog plumbing; recorded + descriptor-pinned separately
//	server/discover — 2026-07-28 capability/version discovery (servers MUST answer)
//	notifications/* — protocol notifications (initialized, cancelled, progress, ...)
//
// Everything else is mediated — INCLUDING when it arrives without a
// request id (as a notification): an actionable method must not dodge
// mediation by dropping its id (the caller refuses those).
func mediatedCall(msg *jsonrpc.Message) (*jsonrpc.ToolCallParams, error) {
	switch {
	case msg.Method == "initialize", msg.Method == "ping",
		msg.Method == "tools/list", msg.Method == "server/discover",
		strings.HasPrefix(msg.Method, "notifications/"):
		return nil, nil
	case msg.Method == "tools/call":
		call, err := msg.ToolCall()
		if err != nil {
			return nil, err // malformed: reject at the boundary, never forward
		}
		return call, nil
	default:
		return &jsonrpc.ToolCallParams{Name: msg.Method, Arguments: msg.Params}, nil
	}
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
				// FAIL CLOSED: a client→server line the boundary cannot
				// parse is a line it cannot mediate — so it is rejected and
				// recorded, NEVER relayed. An upstream with a different or
				// more lenient parser (batch arrays, NaN literals, version
				// drift) must not get a second interpretation of an
				// actionable request. The reject follows the spec: parse
				// errors answer -32700, valid-JSON-but-not-JSON-RPC answers
				// -32600, id echoed when detectable (else null).
				rid := jsonrpc.ProbeID(line)
				if hooks.OnMalformedCall != nil {
					hooks.OnMalformedCall(rid, line, err)
				}
				code, message := jsonrpc.CodeInvalidRequest, "invalid request"
				if !json.Valid(line) {
					code, message = jsonrpc.CodeParseError, "parse error"
				}
				resp := jsonrpc.ErrorResponse(rid, code, message, map[string]any{
					"code": "invalid_request", "rule_id": "malformed-request",
					"reason": err.Error(), "verdict": "deny",
				})
				out, _ := jsonrpc.Marshal(resp)
				outMu.Lock()
				_, werr := clientOut.Write(append(out, '\n'))
				outMu.Unlock()
				if werr != nil {
					return fmt.Errorf("write rejection response: %w", werr)
				}
				continue // never forwarded
			}
			if msg.IsRequest() {
				call, merr := mediatedCall(msg)
				if merr != nil {
					// Malformed tools/call: the boundary rejects it and it
					// is NEVER forwarded (nothing crosses unmediated).
					// The hook records the rejection as evidence.
					if hooks.OnMalformedCall != nil {
						hooks.OnMalformedCall(msg.ID, msg.Params, merr)
					}
					resp := jsonrpc.ErrorResponse(msg.ID, jsonrpc.CodeInvalidParams,
						"invalid tools/call params", map[string]any{
							"code": "invalid_params", "rule_id": "malformed-request",
							"reason": merr.Error(), "verdict": "deny",
						})
					out, _ := jsonrpc.Marshal(resp)
					outMu.Lock()
					_, werr := clientOut.Write(append(out, '\n'))
					outMu.Unlock()
					if werr != nil {
						return fmt.Errorf("write rejection response: %w", werr)
					}
					continue // never forwarded
				}
				if call != nil {
					idMu.Lock()
					toolIDs[idKey(msg.ID)] = true
					idMu.Unlock()
					if hooks.OnToolCall != nil {
						decision, denyData := hooks.OnToolCall(msg.ID, call)
						if decision == DecisionDeny {
							// The synthesized deny response below IS the
							// response: no upstream reply will come, so
							// forget the id now (denied requests must not
							// accumulate in toolIDs).
							idMu.Lock()
							delete(toolIDs, idKey(msg.ID))
							idMu.Unlock()
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
			if msg.Method != "" && !msg.IsRequest() && !msg.IsResponse() {
				// A notification gets no response, so it can never be
				// mediated in-band — and an ACTIONABLE method disguised as
				// a notification would still cause side effects upstream.
				// Protocol notifications (notifications/*) pass as before;
				// anything that would be mediated as a call is refused and
				// recorded (silently — the spec forbids answering
				// notifications).
				if call, merr := mediatedCall(msg); call != nil || merr != nil {
					cause := fmt.Errorf("actionable method %q sent as a notification (no request id): refused at the boundary — tool calls must be requests", msg.Method)
					if hooks.OnMalformedCall != nil {
						hooks.OnMalformedCall(nil, line, cause)
					}
					continue // never forwarded
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
					decision, denyData := hooks.OnToolResult(msg.ID, msg.Error != nil, res)
					if decision == DecisionDeny {
						// Replace the response the harness sees (e.g. over
						// the payload cap) with a structured error.
						resp := jsonrpc.DenyResponse(msg.ID, denyData)
						out, _ := jsonrpc.Marshal(resp)
						outMu.Lock()
						_, werr := clientOut.Write(append(out, '\n'))
						outMu.Unlock()
						if werr != nil {
							return fmt.Errorf("write replaced response: %w", werr)
						}
						continue
					}
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
