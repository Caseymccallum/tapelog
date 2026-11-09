package main

// Regressions for the M14 adversarial review's CLI findings: the conversation
// filter must not select the reserved remainder, and it must not be silently
// ignored by --json.

import (
	"strings"
	"testing"
)

func TestConversationFilterRejectsTheReservedRemainder(t *testing.T) {
	dir := t.TempDir()
	workflowFixture(t, dir, "safe.jsonl", true)
	_, err := executeBehaviour(t, "conversations", "--dir", dir, "--conversation", "(unattributed)")
	if err == nil {
		t.Fatal("the unattributed remainder is not a conversation and must not be selectable")
	}
	if !strings.Contains(err.Error(), "not a conversation") {
		t.Fatalf("the refusal must say why, got %v", err)
	}
}

func TestConversationFilterRejectsMalformedIds(t *testing.T) {
	dir := t.TempDir()
	workflowFixture(t, dir, "safe.jsonl", true)
	for _, id := range []string{"has space", strings.Repeat("a", 200)} {
		if _, err := executeBehaviour(t, "conversations", "--dir", dir, "--conversation", id); err == nil {
			t.Fatalf("conversation id %q must be refused", id)
		}
	}
}

// --json used to ignore --conversation entirely and emit every group. Refusing
// is better than a filter that silently does nothing.
func TestConversationFilterIsNotSilentlyIgnoredInJSONMode(t *testing.T) {
	dir := t.TempDir()
	workflowFixture(t, dir, "safe.jsonl", true)
	_, err := executeBehaviour(t, "conversations", "--dir", dir, "--conversation", "conv-x", "--json")
	if err == nil {
		t.Fatal("--json with --conversation must point at the conversation command, not print the whole library")
	}
	if !strings.Contains(err.Error(), "behaviour conversation") {
		t.Fatalf("the error must name the command that does support it, got %v", err)
	}
}
