package mux

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/cassette-ai/cassette/internal/jsonrpc"
	"github.com/cassette-ai/cassette/internal/mediator"
)

// Serve runs the harness-facing stdio MCP server. Messages are processed
// strictly in arrival order — deterministic mediation is worth more here
// than parallel throughput (ADR 0004).
func (mx *Mux) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	write := func(m *jsonrpc.Message) error {
		b, err := jsonrpc.Marshal(m)
		if err != nil {
			return err
		}
		_, err = out.Write(append(b, '\n'))
		return err
	}
	result := func(id json.RawMessage, v any) error {
		raw, _ := json.Marshal(v)
		return write(&jsonrpc.Message{JSONRPC: "2.0", ID: id, Result: raw})
	}
	fail := func(id json.RawMessage, code int, msg string, data any) error {
		var raw json.RawMessage
		if data != nil {
			raw, _ = json.Marshal(data)
		}
		return write(&jsonrpc.Message{
			JSONRPC: "2.0", ID: id,
			Error: &jsonrpc.ErrorObj{Code: code, Message: msg, Data: raw},
		})
	}

	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		msg, err := jsonrpc.Parse([]byte(line))
		if err != nil || msg.IsNotification() || !msg.IsRequest() {
			continue // never break the protocol on stray bytes
		}

		switch msg.Method {
		case "initialize":
			if err := result(msg.ID, map[string]any{
				"protocolVersion": mcpclientProtocol(),
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]any{"name": "cassette-mux", "version": mx.version},
			}); err != nil {
				return err
			}
		case "ping":
			if err := result(msg.ID, map[string]any{}); err != nil {
				return err
			}
		case "tools/list":
			tools, err := mx.tools(ctx)
			if err != nil {
				if err := fail(msg.ID, -32603, err.Error(), nil); err != nil {
					return err
				}
				continue
			}
			if tools == nil {
				tools = []json.RawMessage{}
			}
			mx.med.RecordToolsList(tools)
			if err := result(msg.ID, map[string]any{"tools": tools}); err != nil {
				return err
			}
		case "tools/call":
			call, err := msg.ToolCall()
			if err != nil {
				if err := fail(msg.ID, -32602, "invalid tools/call params", nil); err != nil {
					return err
				}
				continue
			}
			server, tool, ok := mediator.SplitNamespacedName(call.Name)
			u := mx.byName[server]
			if !ok || u == nil {
				if err := fail(msg.ID, -32602, "unknown tool: no such server", map[string]any{
					"code": "unknown_tool", "tool": call.Name,
				}); err != nil {
					return err
				}
				continue
			}

			outcome := mx.med.Decide(msg.ID, call.Name, call.Arguments)
			if !outcome.Allowed {
				if err := fail(msg.ID, jsonrpc.CodeToolDenied, "tool call denied by policy", map[string]any{
					"code": "tool_denied", "rule_id": outcome.RuleID,
					"reason": outcome.Reason, "verdict": outcome.Verdict,
				}); err != nil {
					return err
				}
				continue
			}

			raw, isError, err := u.client.CallTool(ctx, tool, call.Arguments)
			if err != nil {
				eobj := &jsonrpc.ErrorObj{Code: -32603, Message: fmt.Sprintf("upstream %q: %v", server, err)}
				rawErr, _ := json.Marshal(eobj)
				mx.med.Result(msg.ID, true, rawErr)
				if err := write(&jsonrpc.Message{JSONRPC: "2.0", ID: msg.ID, Error: eobj}); err != nil {
					return err
				}
				continue
			}
			mx.med.Result(msg.ID, isError, raw)
			if isError {
				var eobj jsonrpc.ErrorObj
				if json.Unmarshal(raw, &eobj) == nil {
					if err := write(&jsonrpc.Message{JSONRPC: "2.0", ID: msg.ID, Error: &eobj}); err != nil {
						return err
					}
					continue
				}
			}
			if err := result(msg.ID, json.RawMessage(raw)); err != nil {
				return err
			}
		default:
			if err := fail(msg.ID, -32601, "method not found", nil); err != nil {
				return err
			}
		}
	}
	return sc.Err()
}

// mcpclientProtocol returns the protocol version we advertise.
func mcpclientProtocol() string { return "2026-07-28" }
