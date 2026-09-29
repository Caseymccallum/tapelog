// Package session implements cassette's session event log: an append-only,
// hash-chained JSONL format that serves as both the audit trail and the
// replay source. The canonical form is specified in docs/SCHEMA.md and MUST
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

// EventType enumerates v0 event types. See docs/SCHEMA.md.
type EventType string

const (
	EventSessionStart   EventType = "session/start"
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
// per docs/SCHEMA.md "Hash chain — canonical form".
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

// ToolCallPayload is the payload of tools/call.
type ToolCallPayload struct {
	ID                 json.RawMessage `json:"id"`
	Tool               string          `json:"tool"`
	Args               json.RawMessage `json:"args"`
	ToolDescriptorHash string          `json:"tool_descriptor_hash,omitempty"`
	DescriptorDrift    bool            `json:"descriptor_drift,omitempty"`
}

// PolicyDecisionPayload is the payload of policy/decision.
type PolicyDecisionPayload struct {
	ID      json.RawMessage `json:"id"`
	Verdict string          `json:"verdict"`
	RuleID  string          `json:"rule_id"`
	Reason  string          `json:"reason"`
}

// ToolResultPayload is the payload of tools/result.
type ToolResultPayload struct {
	ID       json.RawMessage `json:"id"`
	IsError  bool            `json:"is_error"`
	Result   json.RawMessage `json:"result"`
}

// SessionEndPayload is the payload of session/end.
type SessionEndPayload struct {
	Reason string `json:"reason"`
}
