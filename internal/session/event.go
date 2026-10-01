// Package session implements tapelog's session event log: an append-only,
// hash-chained JSONL format that serves as both the audit trail and the
// replay source. The canonical form is specified in spec/session-log-v0.md and MUST
// stay byte-identical to this implementation (schema version "v: 0").
package session

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// SchemaVersion is the current session log schema version (the "v" field).
const SchemaVersion = 0

// EventType enumerates v0 event types. See spec/session-log-v0.md.
type EventType string

const (
	EventSessionStart   EventType = "session/start"
	EventToolsList      EventType = "tools/list"
	EventToolCall       EventType = "tools/call"
	EventPolicyDecision EventType = "policy/decision"
	EventToolResult     EventType = "tools/result"
	EventSessionEnd     EventType = "session/end"
)

// Event is one line of the session log.
type Event struct {
	Version   int             `json:"v"`
	Seq       uint64          `json:"seq"`
	TS        string          `json:"ts"`
	Type      EventType       `json:"type"`
	SessionID string          `json:"session_id"`
	Payload   json.RawMessage `json:"payload"`
	PrevHash  string          `json:"prev_hash"`
	Hash      string          `json:"hash"`
}

// CanonicalBytes returns the exact byte string hashed for the event,
// per spec/session-log-v0.md "Hash chain — canonical form".
func (e Event) CanonicalBytes() ([]byte, error) {
	payload := e.Payload
	if len(payload) == 0 {
		payload = json.RawMessage("{}")
	}
	canonPayload, err := canonicalJSON(payload)
	if err != nil {
		return nil, fmt.Errorf("canonicalize payload: %w", err)
	}
	return []byte(fmt.Sprintf("v=%d\nseq=%d\nts=%s\ntype=%s\nsession_id=%s\nprev_hash=%s\npayload=%s",
		e.Version, e.Seq, e.TS, e.Type, e.SessionID, e.PrevHash, canonPayload)), nil
}

// ComputeHash returns the hex SHA-256 of the event's canonical form.
func (e Event) ComputeHash() (string, error) {
	b, err := e.CanonicalBytes()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// --- v0 payload types -------------------------------------------------

// SessionStartPayload is the payload of session/start.
type SessionStartPayload struct {
	Harness    string `json:"harness"`
	PolicyID   string `json:"policy_id"`
	PolicyHash string `json:"policy_hash"`
}

// ToolsListPayload is the payload of tools/list: the tool descriptors the
// server advertised. Descriptors are redacted and canonicalized before
// recording (spec/session-log-v0.md), so replay can serve them verbatim.
type ToolsListPayload struct {
	Tools []json.RawMessage `json:"tools"`
}

// ToolCallPayload is the payload of tools/call.
type ToolCallPayload struct {
	ID                 json.RawMessage `json:"id"`
	Tool               string          `json:"tool"`
	Args               json.RawMessage `json:"args"`
	ToolDescriptorHash string          `json:"tool_descriptor_hash,omitempty"`
	DescriptorDrift    bool            `json:"descriptor_drift,omitempty"`
	// ParentSeq is the seq of the event this call was caused by
	// (optional causation link — populated by async continuations in
	// v0.6; additive, readers MUST ignore when absent).
	ParentSeq *uint64 `json:"parent_seq,omitempty"`
	// Traceparent is the W3C trace context copied from the request's
	// `_meta.traceparent` (optional correlation passthrough).
	Traceparent string `json:"traceparent,omitempty"`
}

// PolicyDecisionPayload is the payload of policy/decision.
type PolicyDecisionPayload struct {
	ID      json.RawMessage `json:"id"`
	Verdict string          `json:"verdict"`
	RuleID  string          `json:"rule_id"`
	Reason  string          `json:"reason"`
	// ParentSeq links the decision to the tools/call event it decided.
	ParentSeq  *uint64 `json:"parent_seq,omitempty"`
	Traceparent string `json:"traceparent,omitempty"`
}

// ToolResultPayload is the payload of tools/result.
type ToolResultPayload struct {
	ID       json.RawMessage `json:"id"`
	IsError  bool            `json:"is_error"`
	Result   json.RawMessage `json:"result"`
	// ParentSeq links the result to the tools/call event it answers.
	ParentSeq  *uint64 `json:"parent_seq,omitempty"`
	Traceparent string `json:"traceparent,omitempty"`
}

// SessionEndPayload is the payload of session/end.
type SessionEndPayload struct {
	Reason string `json:"reason"`
}
