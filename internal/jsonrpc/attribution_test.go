package jsonrpc

import (
	"strings"
	"testing"
)

func TestAttributionOfCanonical(t *testing.T) {
	meta := []byte(`{"io.tapelog/conversationId":"conv-1","io.tapelog/toolCallId":"call-7","io.tapelog/subagentId":"worker-b"}`)
	a, dropped := AttributionOf(meta)
	if dropped != 0 {
		t.Fatalf("no value should be dropped, got %d", dropped)
	}
	if a.ConversationID != "conv-1" || a.ToolCallID != "call-7" || a.SubagentID != "worker-b" {
		t.Fatalf("unexpected attribution: %+v", a)
	}
}

// The harness alias is accepted so a client can declare its own identity
// without impersonating the recorder; the canonical key wins if both are
// present, or the same call could be attributed two ways.
func TestAttributionOfAliasAndPrecedence(t *testing.T) {
	alias := []byte(`{"io.hermes.agent/conversationId":"hermes-1"}`)
	a, dropped := AttributionOf(alias)
	if dropped != 0 || a.ConversationID != "hermes-1" {
		t.Fatalf("alias must be accepted: %+v dropped=%d", a, dropped)
	}

	both := []byte(`{"io.tapelog/conversationId":"canonical","io.hermes.agent/conversationId":"alias"}`)
	a, _ = AttributionOf(both)
	if a.ConversationID != "canonical" {
		t.Fatalf("canonical key must win, got %q", a.ConversationID)
	}
}

// A malformed identifier is a different identifier: it must be rejected
// and counted, never truncated or coerced.
func TestAttributionOfRejectsInvalid(t *testing.T) {
	long := `{"io.tapelog/conversationId":"` + strings.Repeat("a", MaxAttributionLen+1) + `"}`
	cases := map[string]string{
		"oversize":     long,
		"control char": "{\"io.tapelog/conversationId\":\"conv\\u0000inject\"}",
		"space":        `{"io.tapelog/conversationId":"two words"}`,
		"unicode":      `{"io.tapelog/conversationId":"cönv"}`,
		"empty":        `{"io.tapelog/conversationId":""}`,
		"wrong type":   `{"io.tapelog/conversationId":42}`,
	}
	for name, meta := range cases {
		a, dropped := AttributionOf([]byte(meta))
		if a.ConversationID != "" {
			t.Fatalf("%s: invalid id must not be recorded, got %q", name, a.ConversationID)
		}
		if dropped != 1 {
			t.Fatalf("%s: want 1 dropped key, got %d", name, dropped)
		}
	}
}

func TestValidAttributionID(t *testing.T) {
	for _, s := range []string{"a", "A-1_b.c:d", strings.Repeat("a", MaxAttributionLen)} {
		if !ValidAttributionID(s) {
			t.Fatalf("%q (len %d) must be valid", s, len(s))
		}
	}
	for _, s := range []string{"", "has space", strings.Repeat("a", MaxAttributionLen+1), "conv\n", "cönv"} {
		if ValidAttributionID(s) {
			t.Fatalf("%q must be invalid", s)
		}
	}
}

// A malformed canonical value must not fall through to the alias: that
// would let a client present an invalid canonical id and still be
// recorded under the alias, contradicting the documented precedence.
func TestAttributionCanonicalInvalidDoesNotFallThrough(t *testing.T) {
	meta := []byte(`{"io.tapelog/conversationId":"has space","io.hermes.agent/conversationId":"alias-1"}`)
	a, dropped := AttributionOf(meta)
	if a.ConversationID != "" {
		t.Fatalf("invalid canonical must not fall through to the alias, got %q", a.ConversationID)
	}
	if dropped != 1 {
		t.Fatalf("want 1 dropped key, got %d", dropped)
	}
	// The alias still applies when the canonical key is simply absent.
	a, _ = AttributionOf([]byte(`{"io.hermes.agent/conversationId":"alias-1"}`))
	if a.ConversationID != "alias-1" {
		t.Fatalf("alias must be used when canonical is absent, got %q", a.ConversationID)
	}
}

// Absent metadata is not an error and yields zero values: the caller
// reports "unavailable" rather than inventing an identifier.
func TestAttributionOfAbsent(t *testing.T) {
	for _, meta := range [][]byte{nil, {}, []byte(`{}`), []byte(`null`)} {
		a, dropped := AttributionOf(meta)
		if a != (Attribution{}) || dropped != 0 {
			t.Fatalf("meta %q: want zero attribution and no drops, got %+v/%d", meta, a, dropped)
		}
	}
}
