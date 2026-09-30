// Package limits provides session budgets, rate limits, and payload caps
// for tool calls — the "rate limits, payload caps, and budgets" every
// gateway in this space offers (docs/POLICY.md `limits:`). Enforcement is
// fail-closed and explainable: every violation carries a rule id and
// reason like a policy decision.
package limits

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Limits are session-scoped resource limits. Zero means unlimited.
type Limits struct {
	MaxCalls         int            `yaml:"max_calls"`           // total tool-call attempts per session
	MaxCallsPerTool  map[string]int `yaml:"max_calls_per_tool"`  // tool glob -> allowed attempts
	MaxPerMinute     int            `yaml:"max_per_minute"`      // sliding 60s window over all tools
	MaxResponseBytes int            `yaml:"max_response_bytes"`  // tool results larger than this are replaced with an error
}

// Enabled reports whether any limit is configured.
func (l Limits) Enabled() bool {
	if l.MaxCalls > 0 || l.MaxPerMinute > 0 || l.MaxResponseBytes > 0 {
		return true
	}
	for _, n := range l.MaxCallsPerTool {
		if n > 0 {
			return true
		}
	}
	return false
}

// Violation describes why a limit rejected a call or response.
type Violation struct {
	RuleID string
	Reason string
}

// Tracker enforces Limits for one session. Safe for concurrent use.
type Tracker struct {
	lim     Limits
	now     func() time.Time // test seam
	mu      sync.Mutex
	total   int
	perTool map[string]int
	win     []time.Time // attempt timestamps in the current window

	matchers []toolLimit
}

type toolLimit struct {
	pattern string
	re      *regexp.Regexp
	max     int
}

// NewTracker validates and compiles a Limits configuration.
func NewTracker(l Limits) (*Tracker, error) {
	if l.MaxCalls < 0 || l.MaxPerMinute < 0 || l.MaxResponseBytes < 0 {
		return nil, fmt.Errorf("limits: values must be >= 0")
	}
	t := &Tracker{lim: l, perTool: map[string]int{}, now: time.Now}
	for pat, max := range l.MaxCallsPerTool {
		if max < 0 {
			return nil, fmt.Errorf("limits: max_calls_per_tool %q must be >= 0", pat)
		}
		re, err := globToRegex(pat)
		if err != nil {
			return nil, fmt.Errorf("limits: bad pattern %q: %w", pat, err)
		}
		t.matchers = append(t.matchers, toolLimit{pattern: pat, re: re, max: max})
	}
	return t, nil
}

// Check admits one tool-call ATTEMPT and counts it. Attempts denied by
// policy still count — limits are anti-flood protection, not billing.
// A nil Violation means the call may proceed.
func (t *Tracker) Check(tool string) *Violation {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()

	if t.lim.MaxCalls > 0 && t.total >= t.lim.MaxCalls {
		return &Violation{"limits.max_calls",
			fmt.Sprintf("session tool-call budget exhausted (%d calls)", t.lim.MaxCalls)}
	}
	if t.lim.MaxPerMinute > 0 {
		cutoff := now.Add(-time.Minute)
		kept := t.win[:0]
		for _, ts := range t.win {
			if ts.After(cutoff) {
				kept = append(kept, ts)
			}
		}
		t.win = kept
		if len(t.win) >= t.lim.MaxPerMinute {
			return &Violation{"limits.max_per_minute",
				fmt.Sprintf("rate limit exceeded (%d calls/min)", t.lim.MaxPerMinute)}
		}
		t.win = append(t.win, now)
	}
	for _, m := range t.matchers {
		if m.re.MatchString(tool) && t.perTool[m.pattern] >= m.max {
			return &Violation{"limits.max_calls_per_tool",
				fmt.Sprintf("tool %q exceeded its attempt budget (%d for pattern %q)", tool, m.max, m.pattern)}
		}
	}

	t.total++
	for _, m := range t.matchers {
		if m.re.MatchString(tool) {
			t.perTool[m.pattern]++
		}
	}
	return nil
}

// CheckResponse enforces the result payload cap.
func (t *Tracker) CheckResponse(n int) *Violation {
	if t.lim.MaxResponseBytes > 0 && n > t.lim.MaxResponseBytes {
		return &Violation{"limits.max_response_bytes",
			fmt.Sprintf("tool result is %d bytes, over the %d-byte cap", n, t.lim.MaxResponseBytes)}
	}
	return nil
}

// globToRegex converts a tool glob (* and ?) to an anchored regexp.
func globToRegex(pat string) (*regexp.Regexp, error) {
	var b strings.Builder
	b.WriteString("^")
	for _, r := range pat {
		switch r {
		case '*':
			b.WriteString(".*")
		case '?':
			b.WriteString(".")
		case '.', '+', '(', ')', '|', '^', '$', '{', '}', '[', ']', '\\':
			b.WriteString(regexp.QuoteMeta(string(r)))
		default:
			b.WriteRune(r)
		}
	}
	b.WriteString("$")
	return regexp.Compile(b.String())
}
