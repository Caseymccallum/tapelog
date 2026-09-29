package replay

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/tapelog-dev/tapelog/internal/session"
)

// MatchMode selects how live arguments are matched to recordings.
type MatchMode string

const (
	// MatchExact requires canonical equality after redaction (default).
	MatchExact MatchMode = "exact"
	// MatchSubset accepts live args that contain all recorded args
	// (recorded ⊆ live); tolerates harness-side extra fields.
	MatchSubset MatchMode = "subset"
	// MatchTool matches on tool name only; results replay in recorded
	// order. For flow testing when arguments legitimately vary.
	MatchTool MatchMode = "tool"
)

// ParseMatchMode validates a --match flag value.
func ParseMatchMode(s string) (MatchMode, error) {
	switch MatchMode(s) {
	case MatchExact, MatchSubset, MatchTool:
		return MatchMode(s), nil
	}
	return "", fmt.Errorf("unknown match mode %q (want exact|subset|tool)", s)
}

// Miss records a fail-loud unmatched call.
type Miss struct {
	Tool string
	Args json.RawMessage
}

// Player serves recorded interactions to live calls, consuming each once.
// Safe for concurrent use.
type Player struct {
	mu   sync.Mutex
	c    *Tape
	mode MatchMode
	red  *session.Redactor
	used []bool

	played int
	misses []Miss
}

// NewPlayer creates a Player over a tapelog.
func NewPlayer(c *Tape, mode MatchMode) *Player {
	return &Player{
		c:    c,
		mode: mode,
		red:  session.NewRedactor(),
		used: make([]bool, len(c.Interactions)),
	}
}

// Play returns the next unconsumed interaction matching the live call.
// ok=false means no recording matched — the caller must fail loudly.
func (p *Player) Play(tool string, args json.RawMessage) (Interaction, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	redacted := p.red.RedactJSON(args)
	for i := range p.c.Interactions {
		if p.used[i] {
			continue
		}
		it := &p.c.Interactions[i]
		if it.Tool != tool {
			continue
		}
		if !p.argsMatch(it, redacted) {
			continue
		}
		p.used[i] = true
		p.played++
		return *it, true
	}
	p.misses = append(p.misses, Miss{Tool: tool, Args: redacted})
	return Interaction{}, false
}

// argsMatch applies the matcher mode (both sides redacted/canonical).
func (p *Player) argsMatch(it *Interaction, redactedLive json.RawMessage) bool {
	switch p.mode {
	case MatchTool:
		return true
	case MatchSubset:
		return subsetMatch(it.Args, redactedLive)
	default: // MatchExact
		a, err1 := session.CanonicalJSON(it.Args)
		b, err2 := session.CanonicalJSON(redactedLive)
		if err1 != nil || err2 != nil {
			return false
		}
		return a == b
	}
}

// Stats reports played/unused counts and the fail-loud misses.
func (p *Player) Stats() (played, unused int, misses []Miss) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, u := range p.used {
		if !u {
			unused++
		}
	}
	return p.played, unused, append([]Miss(nil), p.misses...)
}

// subsetMatch reports whether every value in `recorded` appears in `live`
// (objects recurse; everything else compares canonically).
func subsetMatch(recorded, live json.RawMessage) bool {
	var r, l any
	if err := decodeAny(recorded, &r); err != nil {
		return false
	}
	if err := decodeAny(live, &l); err != nil {
		return false
	}
	return subsetValue(r, l)
}

func decodeAny(raw json.RawMessage, out *any) error {
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	return dec.Decode(out)
}

func subsetValue(recorded, live any) bool {
	switch rv := recorded.(type) {
	case map[string]any:
		lv, ok := live.(map[string]any)
		if !ok {
			return false
		}
		for k, rVal := range rv {
			lVal, ok := lv[k]
			if !ok || !subsetValue(rVal, lVal) {
				return false
			}
		}
		return true
	case []any:
		lv, ok := live.([]any)
		if !ok || len(rv) != len(lv) {
			return false
		}
		for i := range rv {
			if !subsetValue(rv[i], lv[i]) {
				return false
			}
		}
		return true
	default:
		rb, err1 := json.Marshal(rv)
		lb, err2 := json.Marshal(live)
		if err1 != nil || err2 != nil {
			return false
		}
		return string(rb) == string(lb)
	}
}
