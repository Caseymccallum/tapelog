package taint

import (
	"encoding/json"
	"strings"
	"testing"
)

// The unknown-source fact must survive value-bucket pressure. A sentinel stored
// as ordinary values is flushed oldest-first once the per-source cap is hit,
// which would silently turn "never clean" back into "clean".
func TestUnknownSourceSurvivesEviction(t *testing.T) {
	s := New(0, 0) // defaults: 512 values per source
	s.MarkUnknown()

	for i := 0; i < 600; i++ {
		s.Mark("some_tool", json.RawMessage(`{"text":"value-`+strings.Repeat("x", 10)+`-`+string(rune('a'+i%26))+`"}`))
	}
	got := s.ContaminatedBy([]byte(`{"body":"anything"}`))
	found := false
	for _, g := range got {
		if g == UnknownSource {
			found = true
		}
	}
	if !found {
		t.Fatalf("unknown must remain after value eviction, got %v", got)
	}
}
