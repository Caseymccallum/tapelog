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
}

// ToolCall extracts MCP tools/call parameters from a request message.
func (m *Message) ToolCall() (*ToolCallParams, error) {
	if m.Method != "tools/call" {
		return nil, fmt.Errorf("not a tools/call request (method=%q)", m.Method)
	}
	var p ToolCallParams
	if err := json.Unmarshal(m.Params, &p); err != nil {
		return nil, fmt.Errorf("decode tools/call params: %w", err)
	}
	return &p, nil
}

// DenyResponse synthesizes a structured JSON-RPC error response for a
// denied tool call. The original request id is echoed back.
func DenyResponse(id json.RawMessage, data any) *Message {
	dataRaw, _ := json.Marshal(data)
	return &Message{
		JSONRPC: "2.0",
		ID:      id,
		Error: &ErrorObj{
			Code:    CodeToolDenied,
			Message: "tool call denied by policy",
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
