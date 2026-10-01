// Package compat is the MCP compatibility laboratory: era-variant scripted
// servers plus a matrix runner that drives tapelog's full loop —
// discover → connect → tools/list → tools/call → errors → `_meta` → MRTR →
// redaction → policy → recording → replay → what-if — against every
// (protocol era × transport) cell.
//
// Two tiers share one runner (docs/ROADMAP.md v0.4):
//
//   - hermetic: in-repo scripted servers (this package's Server + the
//     fakecmd binary) — deterministic, CI-blocking
//   - real-world: pinned third-party servers via RunCell with a Command/URL
//     — scheduled, never blocks CI (live_test.go)
//
// The Server records what it saw on the wire (methods, per-request `_meta`,
// handshake notifications, MRTR `inputResponses`, HTTP request-metadata
// headers) so assertions can prove tapelog's client-side behavior from the
// server's point of view — not from tapelog's own claims.
package compat

import (
	"encoding/json"
	"sync"

	"github.com/Caseymccallum/tapelog/internal/jsonrpc"
)

// Era is an MCP protocol revision a server speaks.
type Era string

const (
	// Era2026 is the current stateless revision: server/discover,
	// per-request `_meta`, MRTR, resultType on every result.
	Era2026 Era = "2026-07-28"
	// Era2025 is the newest handshake-based revision (initialize +
	// notifications/initialized).
	Era2025 Era = "2025-11-25"
	// Era2024 is the older handshake-based revision.
	Era2024 Era = "2024-11-05"
)

// Eras lists every era the hermetic tier covers.
var Eras = []Era{Era2026, Era2025, Era2024}

// Observation is the server-side record of one conversation — the
// ground truth for wire-level assertions.
type Observation struct {
	// Methods lists every request/notification method seen, in order.
	Methods []string `json:"methods"`
	// MetaOn[method] is true when the request carried
	// `_meta["io.modelcontextprotocol/protocolVersion"]`.
	MetaOn map[string]bool `json:"meta_on"`
	// InitializedNotification records a legacy `notifications/initialized`.
	InitializedNotification bool `json:"initialized_notification"`
	// DiscoverResult is how server/discover was answered:
	// "discover-result" (modern) or "method-not-found" (legacy).
	DiscoverResult string `json:"discover_result"`
	// InputResponses records whether an MRTR retry with `inputResponses`
	// reached the server (params preservation end-to-end).
	InputResponses bool `json:"input_responses"`
	// Headers captures the streamable-HTTP request-metadata headers of the
	// last request (HTTP cells only).
	Headers map[string]string `json:"headers,omitempty"`
}

// Server is an era-variant scripted MCP server. Tools: read_file,
// write_file, read_secrets (canary producer), send_http (external sink),
// delete_file, boom (JSON-RPC error), needs_input (MRTR on Era2026).
type Server struct {
	mu   sync.Mutex
	era  Era
	obs  Observation
	call int // tools/call counter (MRTR first/second round)
}

// NewServer returns a fake server speaking the given era.
func NewServer(era Era) *Server {
	return &Server{era: era, obs: Observation{MetaOn: map[string]bool{}, Headers: map[string]string{}}}
}

// Era reports the served revision.
func (s *Server) Era() Era { return s.era }

// Observations returns a copy of the wire record.
func (s *Server) Observations() Observation {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := Observation{
		Methods:                 append([]string(nil), s.obs.Methods...),
		MetaOn:                  map[string]bool{},
		InitializedNotification: s.obs.InitializedNotification,
		DiscoverResult:          s.obs.DiscoverResult,
		InputResponses:          s.obs.InputResponses,
		Headers:                 map[string]string{},
	}
	for k, v := range s.obs.MetaOn {
		cp.MetaOn[k] = v
	}
	for k, v := range s.obs.Headers {
		cp.Headers[k] = v
	}
	return cp
}

// ObserveHeaders records streamable-HTTP request-metadata headers.
func (s *Server) ObserveHeaders(h map[string]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, v := range h {
		s.obs.Headers[k] = v
	}
}

// Handle answers one JSON-RPC line; the return is the response line
// ("" for notifications or unparseable input).
func (s *Server) Handle(line []byte) []byte {
	msg, err := jsonrpc.Parse(line)
	if err != nil {
		return nil
	}
	s.observe(msg)
	if msg.IsNotification() {
		if msg.Method == "notifications/initialized" {
			s.mu.Lock()
			s.obs.InitializedNotification = true
			s.mu.Unlock()
		}
		return nil
	}
	if !msg.IsRequest() {
		return nil
	}
	return s.dispatch(msg)
}

func (s *Server) observe(msg *jsonrpc.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.obs.Methods = append(s.obs.Methods, msg.Method)
	var p map[string]json.RawMessage
	_ = json.Unmarshal(msg.Params, &p)
	if meta, ok := p["_meta"]; ok {
		var m map[string]json.RawMessage
		if json.Unmarshal(meta, &m) == nil {
			if _, has := m["io.modelcontextprotocol/protocolVersion"]; has {
				s.obs.MetaOn[msg.Method] = true
			}
		}
	}
	if msg.Method == "tools/call" {
		var params struct {
			InputResponses json.RawMessage `json:"inputResponses"`
		}
		_ = json.Unmarshal(msg.Params, &params)
		if len(params.InputResponses) > 0 && string(params.InputResponses) != "null" {
			s.obs.InputResponses = true
		}
	}
}
func (s *Server) dispatch(msg *jsonrpc.Message) []byte {
	switch msg.Method {
	case "server/discover":
		s.mu.Lock()
		if s.era == Era2026 {
			s.obs.DiscoverResult = "discover-result"
			s.mu.Unlock()
			return s.result(msg.ID, discoverResult(s.era))
		}
		s.obs.DiscoverResult = "method-not-found"
		s.mu.Unlock()
		// Legacy-era servers predate discovery: plain method-not-found is
		// exactly what drives a dual-era client's fallback.
		return s.error(msg.ID, -32601, "method not found", nil)
	case "initialize":
		if s.era == Era2026 {
			// Modern-only server: initialize is gone; name the supported
			// versions per the spec's compatibility matrix.
			return s.error(msg.ID, -32601,
				"initialize is not part of this server's protocol; use server/discover",
				map[string]any{"supported": []string{string(Era2026)}})
		}
		return s.result(msg.ID, map[string]any{
			"protocolVersion": string(s.era),
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "compat-fake-" + string(s.era), "version": "1.0.0"},
		})
	case "tools/list":
		return s.result(msg.ID, map[string]any{"tools": toolCatalog()})
	case "tools/call":
		return s.toolCall(msg)
	default:
		return s.error(msg.ID, -32601, "method not found: "+msg.Method, nil)
	}
}

func (s *Server) toolCall(msg *jsonrpc.Message) []byte {
	var p struct {
		Name           string          `json:"name"`
		Arguments      json.RawMessage `json:"arguments"`
		InputResponses json.RawMessage `json:"inputResponses"`
	}
	_ = json.Unmarshal(msg.Params, &p)
	hasInputResponses := len(p.InputResponses) > 0 && string(p.InputResponses) != "null"

	switch p.Name {
	case "read_file", "write_file", "send_http", "delete_file":
		return s.result(msg.ID, map[string]any{
			"content": []any{map[string]any{"type": "text", "text": "ok:" + p.Name}},
		})
	case "read_secrets":
		return s.result(msg.ID, map[string]any{
			"content": []any{map[string]any{"type": "text", "text": "sk-CANARY-7f3a9b"}},
		})
	case "boom":
		return s.error(msg.ID, -32000, "boom: tool exploded on purpose", nil)
	case "needs_input":
		if s.era != Era2026 {
			// Legacy eras have no MRTR: plain result.
			return s.result(msg.ID, map[string]any{
				"content": []any{map[string]any{"type": "text", "text": "no input needed"}},
			})
		}
		if !hasInputResponses {
			// MRTR round 1: ask for more input.
			return s.result(msg.ID, map[string]any{
				"resultType":    "input_required",
				"inputRequests": []any{map[string]any{"type": "text", "prompt": "which environment?"}},
			})
		}
		// MRTR round 2: complete with the provided answer.
		return s.result(msg.ID, map[string]any{
			"resultType": "complete",
			"content":    []any{map[string]any{"type": "text", "text": "proceed:" + string(p.InputResponses)}},
		})
	default:
		return s.error(msg.ID, -32602, "unknown tool: "+p.Name, nil)
	}
}

// discoverResult builds the 2026-07-28 DiscoverResult.
func discoverResult(era Era) map[string]any {
	return map[string]any{
		"resultType":        "complete",
		"supportedVersions": []string{string(era)},
		"capabilities":      map[string]any{"tools": map[string]any{}},
		"_meta": map[string]any{
			"io.modelcontextprotocol/serverInfo": map[string]any{"name": "compat-fake-" + string(era), "version": "1.0.0"},
		},
	}
}

func (s *Server) result(id json.RawMessage, v any) []byte {
	raw, _ := json.Marshal(v)
	out, _ := jsonrpc.Marshal(&jsonrpc.Message{JSONRPC: "2.0", ID: id, Result: jsonrpc.WithResultType(raw)})
	return append(out, '\n')
}

func (s *Server) error(id json.RawMessage, code int, message string, data any) []byte {
	var raw json.RawMessage
	if data != nil {
		raw, _ = json.Marshal(data)
	}
	out, _ := jsonrpc.Marshal(&jsonrpc.Message{JSONRPC: "2.0", ID: id,
		Error: &jsonrpc.ErrorObj{Code: code, Message: message, Data: raw}})
	return append(out, '\n')
}

// toolCatalog is the fixture tool set shared by every era.
func toolCatalog() []map[string]any {
	desc := func(name, description string) map[string]any {
		return map[string]any{
			"name": name, "description": description,
			"inputSchema": map[string]any{"type": "object"},
		}
	}
	return []map[string]any{
		desc("read_file", "Read a file"),
		desc("write_file", "Write a file"),
		desc("read_secrets", "Read secrets (canary producer)"),
		desc("send_http", "Send data to a URL (external sink)"),
		desc("delete_file", "Delete a file"),
		desc("boom", "Explode with a JSON-RPC error"),
		desc("needs_input", "Ask the agent for more input (MRTR)"),
	}
}
