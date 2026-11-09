// Package jsonrpc implements the JSON-RPC 2.0 envelope used by the MCP
// wire format (newline-delimited JSON on stdio). Only what tapelog needs:
// parse, classify, and synthesize error responses.
package jsonrpc

import (
	"encoding/json"
	"fmt"
)

// CodeToolDenied is the JSON-RPC error code tapelog returns when policy
// denies a tool call. Data carries the structured decision.
const CodeToolDenied = -32010

// CodeInvalidParams is the JSON-RPC error code tapelog returns when a
// tools/call request cannot be parsed at the boundary (malformed
// params, missing tool name). The call is rejected and recorded — it
// never reaches an MCP server.
const CodeInvalidParams = -32602

// CodeParseError / CodeInvalidRequest are the standard JSON-RPC errors
// the boundary answers with when a client→server line cannot be parsed
// at all (or is valid JSON but not a valid JSON-RPC message). Such lines
// are REJECTED, never relayed: an upstream with a different or more
// lenient parser must not get the chance to form a second
// interpretation of an actionable request.
const (
	CodeParseError    = -32700
	CodeInvalidRequest = -32600
)

// ErrorObj is a JSON-RPC error object.
type ErrorObj struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// Message is a JSON-RPC 2.0 envelope (request, notification or response).
type Message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *ErrorObj       `json:"error,omitempty"`
}

// Parse decodes one wire line into a Message.
func Parse(line []byte) (*Message, error) {
	var m Message
	if err := json.Unmarshal(line, &m); err != nil {
		return nil, fmt.Errorf("parse JSON-RPC message: %w", err)
	}
	if m.JSONRPC != "2.0" {
		return nil, fmt.Errorf("not a JSON-RPC 2.0 message (jsonrpc=%q)", m.JSONRPC)
	}
	return &m, nil
}

// ProbeID extracts a request id from a line that may not be a valid
// JSON-RPC message, so boundary rejections can answer with the right id
// instead of leaving a client waiting. Returns nil (rendered as JSON-RPC
// null, the spec's choice when the id cannot be detected) otherwise.
func ProbeID(line []byte) json.RawMessage {
	var p struct {
		ID json.RawMessage `json:"id"`
	}
	if err := json.Unmarshal(line, &p); err != nil {
		return nil
	}
	if len(p.ID) == 0 {
		return nil
	}
	return p.ID
}

// IsRequest reports whether the message is a request (method + id).
func (m *Message) IsRequest() bool { return m.Method != "" && len(m.ID) > 0 }

// IsNotification reports whether the message is a notification (method, no id).
func (m *Message) IsNotification() bool { return m.Method != "" && len(m.ID) == 0 }

// IsResponse reports whether the message is a response (id, no method).
func (m *Message) IsResponse() bool { return m.Method == "" && len(m.ID) > 0 }

// ToolCallParams are the MCP tools/call parameters tapelog cares about.
type ToolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
	// Meta is the request's `_meta` object when present (MCP metadata
	// passthrough — e.g. `_meta.traceparent`, spec/session-log-v0.md §6.1).
	Meta json.RawMessage `json:"_meta"`
}

// ToolCall extracts MCP tools/call parameters from a request message.
// A tools/call without a parseable params object or without a tool name
// is malformed: the boundary must reject it (never forward it), so
// failure here is a hard error, not a "nothing to mediate" signal.
func (m *Message) ToolCall() (*ToolCallParams, error) {
	if m.Method != "tools/call" {
		return nil, fmt.Errorf("not a tools/call request (method=%q)", m.Method)
	}
	var p ToolCallParams
	if err := json.Unmarshal(m.Params, &p); err != nil {
		return nil, fmt.Errorf("decode tools/call params: %w", err)
	}
	if p.Name == "" {
		return nil, fmt.Errorf("tools/call params: missing tool name")
	}
	return &p, nil
}

// MetaOf extracts a request's `_meta` object from raw JSON-RPC params
// (for request shapes tapelog doesn't model, e.g. surface calls).
func MetaOf(params json.RawMessage) json.RawMessage {
	var p struct {
		Meta json.RawMessage `json:"_meta"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil
	}
	return p.Meta
}

// TraceparentOf extracts `_meta.traceparent` from a `_meta` object.
func TraceparentOf(meta json.RawMessage) string {
	if len(meta) == 0 {
		return ""
	}
	var m struct {
		Traceparent string `json:"traceparent"`
	}
	if err := json.Unmarshal(meta, &m); err != nil {
		return ""
	}
	return m.Traceparent
}

// Attribution is client-declared correlation metadata carried in a
// request's `_meta` object (spec/session-log-v0.md §6.1, M11). Every
// field is a CLAIM by the client, not an authenticated fact: it groups
// activity, and it must never be read as a session boundary,
// completeness, identity of a human, or permission to read other
// events. Empty means "the client declared nothing".
type Attribution struct {
	ConversationID string
	ToolCallID     string
	SubagentID     string
}

// MaxAttributionLen bounds a client-declared identifier. A longer value
// is rejected rather than truncated: a truncated id is a different id,
// and silently recording the wrong one is worse than recording none.
const MaxAttributionLen = 128

// Canonical (recorder-namespaced) keys, plus a first-party harness alias
// so a client can declare its own identity without impersonating the
// recorder. Canonical wins when a request carries both.
const (
	metaConversationID    = "io.tapelog/conversationId"
	metaToolCallID        = "io.tapelog/toolCallId"
	metaSubagentID        = "io.tapelog/subagentId"
	aliasConversationID   = "io.hermes.agent/conversationId"
	aliasToolCallID       = "io.hermes.agent/toolCallId"
	aliasSubagentID       = "io.hermes.agent/subagentId"
)

// ValidAttributionID reports whether s is acceptable as a recorded
// identifier: a bounded opaque token. The charset excludes control
// characters (log injection) and non-ASCII (homoglyph confusion); it is
// deliberately not a semantic check — the content is the client's
// responsibility.
func ValidAttributionID(s string) bool {
	if s == "" || len(s) > MaxAttributionLen {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '.' || c == '_' || c == ':' || c == '-':
		default:
			return false
		}
	}
	return true
}

// AttributionOf extracts conversation/tool-call/subagent attribution from
// a `_meta` object. The returned count is the number of keys that were
// present but rejected as invalid, so callers can surface a diagnostic
// without ever recording a malformed identifier.
func AttributionOf(meta json.RawMessage) (Attribution, int) {
	var a Attribution
	if len(meta) == 0 {
		return a, 0
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(meta, &m); err != nil {
		return a, 0
	}
	dropped := 0
	pick := func(canonical, alias string) string {
		for _, key := range []string{canonical, alias} {
			raw, ok := m[key]
			if !ok {
				continue
			}
			// A declared canonical key decides: if it is malformed we do not
			// silently fall through to the alias, or a client could present an
			// invalid canonical value alongside a valid alias and still be
			// recorded under the alias, contradicting the documented precedence.
			var v string
			if err := json.Unmarshal(raw, &v); err != nil {
				dropped++
				return ""
			}
			if !ValidAttributionID(v) {
				dropped++
				return ""
			}
			return v
		}
		return ""
	}
	a.ConversationID = pick(metaConversationID, aliasConversationID)
	a.ToolCallID = pick(metaToolCallID, aliasToolCallID)
	a.SubagentID = pick(metaSubagentID, aliasSubagentID)
	return a, dropped
}

// DenyResponse synthesizes a structured JSON-RPC error response for a
// denied tool call. The original request id is echoed back.
func DenyResponse(id json.RawMessage, data any) *Message {
	return ErrorResponse(id, CodeToolDenied, "tool call denied by policy", data)
}

// ErrorResponse synthesizes a JSON-RPC error response with an explicit
// error code (e.g. CodeInvalidParams for malformed requests the
// boundary refuses). The original request id is echoed back.
func ErrorResponse(id json.RawMessage, code int, message string, data any) *Message {
	dataRaw, _ := json.Marshal(data)
	return &Message{
		JSONRPC: "2.0",
		ID:      id,
		Error: &ErrorObj{
			Code:    code,
			Message: message,
			Data:    dataRaw,
		},
	}
}

// WithResultType ensures a result carries the `resultType` discriminator
// the 2026-07-28 spec requires on every result ("complete" unless the
// result already declares one, e.g. MRTR "input_required"). Results that
// are not JSON objects are returned untouched.
func WithResultType(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return raw
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return raw
	}
	if _, ok := m["resultType"]; ok {
		return raw
	}
	m["resultType"] = json.RawMessage(`"complete"`)
	out, err := json.Marshal(m)
	if err != nil {
		return raw
	}
	return out
}

// Marshal serializes a Message to a single wire line (without newline).
func Marshal(m *Message) ([]byte, error) {
	return json.Marshal(m)
}
