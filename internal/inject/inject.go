// Package inject scans tool results for prompt-injection markers: text
// that tries to rewrite the agent's instructions from inside tool output.
// Detection is heuristic — patterns target imperative + AI-context phrases
// to keep false positives low — so the default policy mode is `log`, and
// stronger actions (confirm/deny) are the operator's explicit choice
// (docs/POLICY.md "Injection scanning").
package inject

import (
	"fmt"
	"regexp"
	"strings"
)

// builtins are the conservative default patterns. Each pairs an imperative
// with agent-context vocabulary — generic phrases like "ignore the rules"
// in a game's text must not match.
var builtins = []string{
	`(?i)\bignore\s+(?:all\s+)?(?:previous|prior|above|earlier|your)\s+(?:instructions?|prompts?|rules?|directives?)\b`,
	`(?i)\bdisregard\s+(?:all\s+)?(?:previous|prior|above|earlier|your)\s+(?:instructions?|prompts?|rules?)\b`,
	`(?i)\bforget\s+(?:all\s+)?(?:previous|prior|above|your)\s+(?:instructions?|prompts?|rules?)\b`,
	`(?i)\bnew\s+instructions?\s*:`,
	`(?i)\byou\s+are\s+now\s+(?:a|an|the)\b`,
	`(?i)\bact\s+as\s+(?:a|an)\s+(?:unrestricted|unfiltered|jailbroken)\b`,
	`(?i)\b(?:reveal|print|show|repeat|output)\s+(?:your|the)\s+(?:system|initial|hidden)\s+(?:prompt|instructions?)\b`,
	`(?i)\bdo\s+not\s+(?:tell|inform|alert|notify)\s+the\s+user\b`,
	`(?i)\bwithout\s+(?:telling|informing|alerting)\s+the\s+user\b`,
	`(?i)\bsend\s+(?:the\s+)?(?:contents?|data|files?)\s+to\s+https?://\b`,
	`\{\s*"method"\s*:\s*"(?:tools/call|resources/read|prompts/get)"`, // smuggled tool-call JSON in text
}

// Finding is one matched injection marker.
type Finding struct {
	Pattern string // short pattern id (the regexp source, trimmed)
	Excerpt string // surrounding text, compacted
}

// Scanner holds compiled patterns (built-ins + operator extras).
type Scanner struct {
	patterns []*regexp.Regexp
}

// New compiles the built-in patterns plus optional extras (Go regexp
// syntax). Invalid extras are an error — fail at load, not at scan time.
func New(extras []string) (*Scanner, error) {
	s := &Scanner{}
	all := append([]string{}, builtins...)
	all = append(all, extras...)
	for _, p := range all {
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, fmt.Errorf("inject: bad pattern %q: %w", p, err)
		}
		s.patterns = append(s.patterns, re)
	}
	return s, nil
}

// Scan reports injection findings in text. Order is pattern order.
func (s *Scanner) Scan(text string) []Finding {
	var out []Finding
	for _, re := range s.patterns {
		if loc := re.FindStringIndex(text); loc != nil {
			start := loc[0] - 40
			if start < 0 {
				start = 0
			}
			end := loc[1] + 40
			if end > len(text) {
				end = len(text)
			}
			out = append(out, Finding{
				Pattern: compact(re.String()),
				Excerpt: compact(text[start:end]),
			})
		}
	}
	return out
}

// compact flattens whitespace and trims for one-line audit reasons.
func compact(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 160 {
		s = s[:160] + "…"
	}
	return s
}
