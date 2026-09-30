// Package taint implements value-level data-flow tracking (CaMeL-style,
// experimental): tool results are labeled with their source, and later
// tool-call arguments are checked for contaminated values. Where
// session-level flow rules presume "the source ran, so everything is
// tainted", this fires only when source data demonstrably reaches a sink.
//
// Honest limits (docs/THREAT_MODEL.md): contamination is detected by
// substring matching of recorded string values. Transformations (base64,
// re-encoding, splitting, summarizing) evade matching — the model may
// paraphrase secrets beyond recognition. This is a precision layer over
// session taint, not a proof of non-interference. Keep session-level
// rules for the flows that must never happen.
package taint

import (
	"encoding/json"
	"sync"
)

// Store holds tainted string values per source tool. Bounded: value
// length and per-source count caps keep memory and scan cost flat.
type Store struct {
	mu        sync.Mutex
	maxValues int // per source
	maxLen    int // per value
	minLen    int // shorter values are too collision-prone to taint with
	values    map[string][]string
}

// New creates a Store. Negative/zero fields get sane defaults:
// 512 values per source, 8 KiB per value, 4-char minimum.
func New(maxValues, maxLen int) *Store {
	if maxValues <= 0 {
		maxValues = 512
	}
	if maxLen <= 0 {
		maxLen = 8192
	}
	return &Store{maxValues: maxValues, maxLen: maxLen, minLen: 4, values: map[string][]string{}}
}

// Mark labels the string values inside a tool result as produced by
// source. Values shorter than 4 chars or longer than the cap are skipped;
// the store keeps at most maxValues strings per source (oldest dropped).
func (s *Store) Mark(source string, result json.RawMessage) {
	var found []string
	collectStrings(result, &found, s.maxLen)
	if len(found) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := s.values[source]
	for _, v := range found {
		if len(v) < s.minLen {
			continue
		}
		kept = append(kept, v)
	}
	if len(kept) > s.maxValues {
		kept = kept[len(kept)-s.maxValues:]
	}
	s.values[source] = kept
}

// ContaminatedBy returns the source tools whose recorded values appear in
// args (substring match), deduplicated. Argument strings are matched in
// full — only *stored* values are length-capped (storage bounds are what
// matter; a capped needle can't match across its own truncation).
func (s *Store) ContaminatedBy(args json.RawMessage) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.values) == 0 {
		return nil
	}
	var needles []string
	collectStrings(args, &needles, 0) // 0 = no cap on scanned strings
	if len(needles) == 0 {
		return nil
	}
	var out []string
	for source, vals := range s.values {
		hit := false
		for _, v := range vals {
			for _, n := range needles {
				if len(n) >= s.minLen && len(v) >= s.minLen && contains(n, v) {
					hit = true
					break
				}
			}
			if hit {
				break
			}
		}
		if hit {
			out = append(out, source)
		}
	}
	return out
}

// contains is strings.Contains without the import cycle risk in tests.
func contains(haystack, needle string) bool {
	return len(needle) <= len(haystack) && index(haystack, needle) >= 0
}

func index(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}

// collectStrings walks a JSON value and appends its strings. maxLen > 0
// truncates each string (used for storage; 0 leaves them intact).
func collectStrings(raw []byte, out *[]string, maxLen int) {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return
	}
	walkStrings(v, out, maxLen)
}

func walkStrings(v any, out *[]string, maxLen int) {
	switch t := v.(type) {
	case string:
		if maxLen > 0 && len(t) > maxLen {
			t = t[:maxLen]
		}
		*out = append(*out, t)
	case []any:
		for _, e := range t {
			walkStrings(e, out, maxLen)
		}
	case map[string]any:
		for _, e := range t {
			walkStrings(e, out, maxLen)
		}
	}
}
