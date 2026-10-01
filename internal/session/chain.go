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
//
// The file is opened per append and closed again — deliberately no
// long-lived handle. A held handle would (a) lock editors out of the
// file (Windows sharing violations), and (b) after an external rewrite
// keep appending at a stale offset into an orphaned handle, silently
// losing events. With per-append opens every event lands at the true
// end of whatever file is at the path, the session survives mid-session
// tampering, and the hash chain exposes the edit.
type Writer struct {
	mu        sync.Mutex
	path      string
	sessionID string
	seq       uint64
	prevHash  string
	wrote     int64 // bytes written so far; drift = external modification
	now       func() time.Time
}

// NewWriter creates (truncating) a session log at path and returns a Writer
// bound to the given session ID. Nothing keeps the file open afterwards.
//
// A flight recorder must not silently destroy evidence: if the target
// already exists with content, warn loudly before truncating — shared by
// every command that writes a log (`record` and `mux` behave identically).
func NewWriter(path, sessionID string) (*Writer, error) {
	if st, err := os.Stat(path); err == nil && st.Size() > 0 {
		fmt.Fprintf(os.Stderr, "tapelog: WARNING — %s exists (%d bytes) and will be OVERWRITTEN; copy it first if you want to keep it\n", path, st.Size())
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("create session log: %w", err)
	}
	if err := f.Close(); err != nil {
		return nil, fmt.Errorf("close session log: %w", err)
	}
	return &Writer{path: path, sessionID: sessionID, now: time.Now}, nil
}

// SetClock overrides the time source (tests / deterministic recording).
func (w *Writer) SetClock(now func() time.Time) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.now = now
}

// Append marshals payload, builds the next chained event, and writes one line.
// The file is opened for this append only (O_APPEND), so the write always
// lands at the current end of the file even if the log was rewritten
// externally between events.
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
	if st, err := os.Stat(w.path); err == nil && st.Size() != w.wrote {
		// The log changed under us: someone edited or replaced it
		// mid-session. Record loudly and keep going — the chain
		// (held in memory) continues from the last event WE wrote,
		// so `tapelog verify` pinpoints exactly where the file
		// stopped being ours.
		fmt.Fprintf(os.Stderr, "tapelog: WARNING — %s changed on disk (%d bytes, expected %d): modified outside tapelog while recording; this append continues the chain and `tapelog verify` will flag the edit\n", w.path, st.Size(), w.wrote)
		w.wrote = st.Size()
	}
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open session log: %w", err)
	}
	n, err := f.Write(append(line, '\n'))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return nil, fmt.Errorf("write event: %w", err)
	}
	w.wrote += int64(n)
	w.seq = e.Seq
	w.prevHash = e.Hash
	return &e, nil
}

// Close flushes and closes the log. The writer holds no long-lived
// handle (see the Writer doc comment), so this is a no-op kept for
// API compatibility.
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return nil
}

// VerifyResult reports the outcome of chain verification.
type VerifyResult struct {
	Events      int    `json:"events"`
	SessionID   string `json:"session_id,omitempty"` // session id of the first event
	FirstBadSeq uint64 `json:"first_bad_seq,omitempty"` // 0 = intact
	Problem     string `json:"problem,omitempty"`
	LastHash    string `json:"last_hash,omitempty"` // chain head (last event hash)
}

// OK reports whether the verified log is fully intact.
func (r *VerifyResult) OK() bool { return r.FirstBadSeq == 0 }

// HashAt returns the hash of the event at the given sequence number —
// the chain head as of that event (used to pin mid-session checkpoints).
// The chain through seq is verified as a side effect.
func HashAt(path string, seq uint64) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open session log: %w", err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
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
			return "", fmt.Errorf("event %d: invalid JSON: %w", wantSeq, err)
		}
		if e.Seq != wantSeq {
			return "", fmt.Errorf("sequence gap: got seq %d, want %d", e.Seq, wantSeq)
		}
		if e.PrevHash != prevHash {
			return "", fmt.Errorf("broken chain link at seq %d", e.Seq)
		}
		h, err := e.ComputeHash()
		if err != nil || h != e.Hash {
			return "", fmt.Errorf("event %d hash mismatch", e.Seq)
		}
		prevHash = e.Hash
		if e.Seq == seq {
			return e.Hash, nil
		}
		wantSeq++
	}
	if err := sc.Err(); err != nil {
		return "", fmt.Errorf("read session log: %w", err)
	}
	return "", fmt.Errorf("seq %d not found", seq)
}

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
		if res.SessionID == "" {
			res.SessionID = e.SessionID
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
	res.LastHash = prevHash // chain head: anchor point for `verify --expect`
	return res, nil
}
