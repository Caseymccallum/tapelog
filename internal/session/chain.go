package session

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

// Writer appends hash-chained events to a session log file.
// It is safe for concurrent use.
type Writer struct {
	mu        sync.Mutex
	f         *os.File
	sessionID string
	seq       uint64
	prevHash  string
	now       func() time.Time
}

// NewWriter creates (truncating) a session log at path and returns a Writer
// bound to the given session ID.
func NewWriter(path, sessionID string) (*Writer, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("create session log: %w", err)
	}
	return &Writer{f: f, sessionID: sessionID, now: time.Now}, nil
}

// SetClock overrides the time source (tests / deterministic recording).
func (w *Writer) SetClock(now func() time.Time) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.now = now
}

// Append marshals payload, builds the next chained event, and writes one line.
func (w *Writer) Append(t EventType, payload any) (*Event, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	payloadRaw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal payload: %w", err)
	}
	e := Event{
		Version:   SchemaVersion,
		Seq:       w.seq + 1,
		TS:        w.now().UTC().Format("2006-01-02T15:04:05.000Z"),
		Type:      t,
		SessionID: w.sessionID,
		Payload:   payloadRaw,
		PrevHash:  w.prevHash,
	}
	h, err := e.ComputeHash()
	if err != nil {
		return nil, fmt.Errorf("hash event: %w", err)
	}
	e.Hash = h

	line, err := json.Marshal(e)
	if err != nil {
		return nil, fmt.Errorf("marshal event: %w", err)
	}
	if _, err := w.f.Write(append(line, '\n')); err != nil {
		return nil, fmt.Errorf("write event: %w", err)
	}
	w.seq = e.Seq
	w.prevHash = e.Hash
	return &e, nil
}

// Close flushes and closes the log.
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f == nil {
		return nil
	}
	err := w.f.Close()
	w.f = nil
	return err
}

// VerifyResult reports the outcome of chain verification.
type VerifyResult struct {
	Events      int    `json:"events"`
	FirstBadSeq uint64 `json:"first_bad_seq,omitempty"` // 0 = intact
	Problem     string `json:"problem,omitempty"`
}

// OK reports whether the verified log is fully intact.
func (r *VerifyResult) OK() bool { return r.FirstBadSeq == 0 }

// VerifyFile verifies the hash chain of a session log file.
func VerifyFile(path string) (*VerifyResult, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open session log: %w", err)
	}
	defer f.Close()
	return Verify(f)
}

// Verify recomputes the hash chain of a JSONL session log.
// It checks, per event: hash correctness, prev_hash linkage, and sequence
// contiguity, and reports the first offending sequence number.
func Verify(r io.Reader) (*VerifyResult, error) {
	res := &VerifyResult{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)

	prevHash := ""
	var wantSeq uint64 = 1
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var e Event
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			res.FirstBadSeq = wantSeq
			res.Problem = fmt.Sprintf("line %d: invalid JSON: %v", res.Events+1, err)
			return res, nil
		}
		res.Events++

		if e.Seq != wantSeq {
			res.FirstBadSeq = e.Seq
			res.Problem = fmt.Sprintf("sequence gap: got seq %d, want %d", e.Seq, wantSeq)
			return res, nil
		}
		if e.Version != SchemaVersion {
			res.FirstBadSeq = e.Seq
			res.Problem = fmt.Sprintf("unsupported schema version %d", e.Version)
			return res, nil
		}
		if e.PrevHash != prevHash {
			res.FirstBadSeq = e.Seq
			res.Problem = fmt.Sprintf("broken chain link: prev_hash %q, want %q", e.PrevHash, prevHash)
			return res, nil
		}
		h, err := e.ComputeHash()
		if err != nil {
			res.FirstBadSeq = e.Seq
			res.Problem = fmt.Sprintf("event %d not hashable: %v", e.Seq, err)
			return res, nil
		}
		if h != e.Hash {
			res.FirstBadSeq = e.Seq
			res.Problem = fmt.Sprintf("hash mismatch: event %d was modified after writing", e.Seq)
			return res, nil
		}
		prevHash = e.Hash
		wantSeq++
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read session log: %w", err)
	}
	return res, nil
}
