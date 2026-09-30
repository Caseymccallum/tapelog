package approval

import (
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"
)

// Item is one parked confirmation awaiting a human decision.
type Item struct {
	ID     int             `json:"id"`
	Tool   string          `json:"tool"`
	Args   json.RawMessage `json:"args"`
	Parked time.Time       `json:"parked_at"`
}

// PendingItem is an Item plus its wait age for listing APIs.
type PendingItem struct {
	Item
	AgeSeconds int `json:"age_seconds"`
}

// Queue is the "quarantine queue" Confirmer: `confirm` verdicts park at
// the boundary and block until a human decides remotely (Decide) or the
// timeout expires (deny — fail closed). The tool call is held, not
// dropped: MCP request/response semantics are preserved while the human
// thinks (docs/POLICY.md "Remote approval queue").
type Queue struct {
	mu      sync.Mutex
	nextID  int
	pending map[int]*parked
	session map[string]bool // tool -> allowed for session
	timeout time.Duration
	now     func() time.Time
}

type parked struct {
	item Item
	ch   chan Choice
	// resolved outcome metadata for Explain
	note   string
	via    string // "queue" | "timeout"
}

// NewQueue creates a Queue whose parked calls expire after timeout
// (timeout <= 0 means no expiry).
func NewQueue(timeout time.Duration) *Queue {
	return &Queue{
		pending: map[int]*parked{},
		session: map[string]bool{},
		timeout: timeout,
		now:     time.Now,
	}
}

// Confirm implements Confirmer: park the call and block.
func (q *Queue) Confirm(tool string, args json.RawMessage) Choice {
	c, _ := q.ConfirmExplain(tool, args)
	return c
}

// ConfirmExplain implements ExplainedConfirmer.
func (q *Queue) ConfirmExplain(tool string, args json.RawMessage) (Choice, string) {
	q.mu.Lock()
	if q.session[tool] {
		q.mu.Unlock()
		return ChoiceAllowSession, " (allowed for session via the approval queue)"
	}
	q.nextID++
	p := &parked{item: Item{ID: q.nextID, Tool: tool, Args: args, Parked: q.now()}, ch: make(chan Choice, 1)}
	q.pending[p.item.ID] = p
	q.mu.Unlock()

	if q.timeout <= 0 {
		choice := <-p.ch
		return q.explain(p, choice)
	}
	select {
	case choice := <-p.ch:
		return q.explain(p, choice)
	case <-time.After(q.timeout):
		q.mu.Lock()
		if _, still := q.pending[p.item.ID]; still {
			delete(q.pending, p.item.ID)
			p.via = "timeout"
			q.mu.Unlock()
			return ChoiceDeny, fmt.Sprintf(" (expired in the approval queue after %s)", q.timeout)
		}
		q.mu.Unlock()
		// Resolved concurrently at the deadline; read the decision.
		choice := <-p.ch
		return q.explain(p, choice)
	}
}

func (q *Queue) explain(p *parked, choice Choice) (Choice, string) {
	q.mu.Lock()
	via, note := p.via, p.note
	q.mu.Unlock()
	if via == "" {
		via = "queue"
	}
	switch choice {
	case ChoiceAllowSession:
		return choice, fmt.Sprintf(" (approved for session via the approval queue%s)", note)
	case ChoiceAllowOnce:
		return choice, fmt.Sprintf(" (approved via the approval queue%s)", note)
	default:
		return choice, fmt.Sprintf(" (denied via the approval queue%s)", note)
	}
}

// List returns pending items oldest-first.
func (q *Queue) List() []PendingItem {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]PendingItem, 0, len(q.pending))
	now := q.now()
	for _, p := range q.pending {
		out = append(out, PendingItem{Item: p.item, AgeSeconds: int(now.Sub(p.item.Parked).Seconds())})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Decide resolves a parked call. Unknown ids return false. An
// "allow_session" decision also exempts that tool for the rest of the
// session (mirroring the terminal prompt's [s] key).
func (q *Queue) Decide(id int, choice Choice, note string) bool {
	q.mu.Lock()
	p, ok := q.pending[id]
	if !ok {
		q.mu.Unlock()
		return false
	}
	delete(q.pending, id)
	if note != "" {
		p.note = "; " + note
	}
	p.via = "queue"
	if choice == ChoiceAllowSession {
		q.session[p.item.Tool] = true
	}
	q.mu.Unlock()
	p.ch <- choice
	return true
}
