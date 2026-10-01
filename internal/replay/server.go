package replay

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/Caseymccallum/tapelog/internal/buildinfo"
	"github.com/Caseymccallum/tapelog/internal/jsonrpc"
	"github.com/Caseymccallum/tapelog/internal/transport"
)

// DefaultProtocolVersion is the MCP protocol version the replay server
// advertises when the client does not request one (spec 2026-07-28).
const DefaultProtocolVersion = "2026-07-28"

// CodeNoMatchingRecording is the JSON-RPC error returned when a live call
// has no recording to play — the fail-loud contract (VCR semantics).
const CodeNoMatchingRecording = -32011

// Server is a hermetic MCP server that answers tool traffic from a tapelog.
type Server struct {
	Player   *Player
	Version  string // tapelog version reported in serverInfo
	Protocol string // MCP protocol version to advertise
}

// Serve runs the replay server over newline-delimited JSON-RPC until the
// input closes or the context is cancelled.
func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	protocol := s.Protocol
	if protocol == "" {
		protocol = DefaultProtocolVersion
	}
	version := s.Version
	if version == "" {
		version = buildinfo.Version
	}

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
		return write(&jsonrpc.Message{
			JSONRPC: "2.0", ID: id,
			Error: errorWith(code, msg, data),
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
		if err != nil {
			continue // never break the protocol on stray bytes
		}
		if msg.IsNotification() {
			continue
		}
		if !msg.IsRequest() {
			continue
		}

		switch msg.Method {
		case "server/discover":
			if err := result(msg.ID, map[string]any{
				"resultType":        "complete",
				"supportedVersions": append([]string{protocol}, transport.LegacyVersions...),
				"capabilities":      map[string]any{"tools": map[string]any{}},
				"_meta": map[string]any{
					"io.modelcontextprotocol/serverInfo": map[string]any{"name": "tapelog-replay", "version": version},
				},
			}); err != nil {
				return err
			}
		case "initialize":
			if err := result(msg.ID, map[string]any{
				"protocolVersion": protocol,
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]any{"name": "tapelog-replay", "version": version},
			}); err != nil {
				return err
			}
		case "ping":
			if err := result(msg.ID, map[string]any{}); err != nil {
				return err
			}
		case "tools/list":
			tools := s.Player.c.Tools
			if tools == nil {
				tools = []json.RawMessage{}
			}
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
			it, ok := s.Player.Play(call.Name, call.Arguments)
			if !ok {
				if err := fail(msg.ID, CodeNoMatchingRecording,
					fmt.Sprintf("no recorded interaction for tool %q", call.Name),
					map[string]any{
						"code":      "no_matching_recording",
						"tool":      call.Name,
						"match":     string(s.Player.mode),
						"remediation": "record this call first, or loosen --match (exact|subset|tool)",
					}); err != nil {
					return err
				}
				continue
			}
			if it.IsError {
				// Recorded outcome was an error: replay the error object.
				var eobj jsonrpc.ErrorObj
				if err := json.Unmarshal(it.Result, &eobj); err == nil {
					if err := write(&jsonrpc.Message{JSONRPC: "2.0", ID: msg.ID, Error: &eobj}); err != nil {
						return err
					}
					continue
				}
			}
			if err := result(msg.ID, it.Result); err != nil {
				return err
			}
		default:
			if err := fail(msg.ID, -32601, "method not found in replay", nil); err != nil {
				return err
			}
		}
	}
	return sc.Err()
}

func errorWith(code int, msg string, data any) *jsonrpc.ErrorObj {
	var raw json.RawMessage
	if data != nil {
		raw, _ = json.Marshal(data)
	}
	return &jsonrpc.ErrorObj{Code: code, Message: msg, Data: raw}
}
