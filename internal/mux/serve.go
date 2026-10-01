package mux

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/Caseymccallum/tapelog/internal/jsonrpc"
	"github.com/Caseymccallum/tapelog/internal/mediator"
	"github.com/Caseymccallum/tapelog/internal/transport"
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
		// Spec 2026-07-28: every result MUST carry `resultType`. Synthesized
		// and relayed results get one when absent ("complete"); upstreams
		// that already declare one (e.g. MRTR "input_required") pass through
		// untouched. The session log keeps the upstream's verbatim bytes —
		// this normalization is harness-facing only.
		return write(&jsonrpc.Message{JSONRPC: "2.0", ID: id, Result: jsonrpc.WithResultType(raw)})
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
		case "server/discover":
			// Modern-era discovery (spec 2026-07-28): no handshake needed.
			if err := result(msg.ID, map[string]any{
				"resultType":        "complete",
				"supportedVersions": append([]string{mcpclientProtocol()}, transport.LegacyVersions...),
				"capabilities": map[string]any{
					"tools": map[string]any{}, "resources": map[string]any{}, "prompts": map[string]any{},
				},
				"_meta": map[string]any{
					"io.modelcontextprotocol/serverInfo": map[string]any{"name": "tapelog-mux", "version": mx.version},
				},
			}); err != nil {
				return err
			}
		case "initialize":
			// Legacy-era handshake: serve the revision the client asked
			// for when we can (dual-era server semantics).
			if err := result(msg.ID, map[string]any{
				"protocolVersion": transport.NegotiateLegacy(paramStr(msg.Params, "protocolVersion")),
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]any{"name": "tapelog-mux", "version": mx.version},
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

			// Forward the client's params VERBATIM except the namespaced name:
		// rebuilding {name, arguments} would silently drop `_meta`,
		// MRTR `inputResponses`, and any future params field.
		fwd := withParam(msg.Params, "name", tool)
		if fwd == nil {
			fwd, _ = json.Marshal(map[string]any{"name": tool, "arguments": call.Arguments})
		}
		raw, isError, err := u.client.CallToolParams(ctx, json.RawMessage(fwd))
			if err != nil {
				eobj := &jsonrpc.ErrorObj{Code: -32603, Message: fmt.Sprintf("upstream %q: %v", server, err)}
				rawErr, _ := json.Marshal(eobj)
				mx.med.Result(msg.ID, true, rawErr)
				if err := write(&jsonrpc.Message{JSONRPC: "2.0", ID: msg.ID, Error: eobj}); err != nil {
					return err
				}
				continue
			}
			if v := mx.med.Result(msg.ID, isError, raw); v != nil {
				// Not deliverable as-is (payload cap / injection block):
				// replace what the harness sees (original stays recorded).
				if err := fail(msg.ID, jsonrpc.CodeToolDenied, "tool result not deliverable", map[string]any{
					"code": v.Code, "rule_id": v.RuleID,
					"reason": v.Reason, "verdict": "deny",
				}); err != nil {
					return err
				}
				continue
			}
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
			// Every other method (resources/read, prompts/get, ...) is a
			// surface call: mediate it like a tool call, then forward.
			outcome := mx.med.Decide(msg.ID, msg.Method, msg.Params)
			if !outcome.Allowed {
				if err := fail(msg.ID, jsonrpc.CodeToolDenied, "tool call denied by policy", map[string]any{
					"code": "tool_denied", "rule_id": outcome.RuleID,
					"reason": outcome.Reason, "verdict": outcome.Verdict,
				}); err != nil {
					return err
				}
				continue
			}
			raw, err := mx.callRouted(ctx, msg.Method, msg.Params)
			if err != nil {
				eobj := &jsonrpc.ErrorObj{Code: -32603, Message: err.Error()}
				rawErr, _ := json.Marshal(eobj)
				_ = mx.med.Result(msg.ID, true, rawErr)
				if err := write(&jsonrpc.Message{JSONRPC: "2.0", ID: msg.ID, Error: eobj}); err != nil {
					return err
				}
				continue
			}
			if v := mx.med.Result(msg.ID, false, raw); v != nil {
				if err := fail(msg.ID, jsonrpc.CodeToolDenied, "tool result not deliverable", map[string]any{
					"code": v.Code, "rule_id": v.RuleID,
					"reason": v.Reason, "verdict": "deny",
				}); err != nil {
					return err
				}
				continue
			}
			if err := result(msg.ID, json.RawMessage(raw)); err != nil {
				return err
			}
		}
	}
	return sc.Err()
}

// mcpclientProtocol returns the protocol version we advertise.
func mcpclientProtocol() string { return "2026-07-28" }
