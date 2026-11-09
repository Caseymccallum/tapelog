package session

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// Structure problem codes. Every code except StructNoEnd makes a file
// structurally invalid — it cannot be one session log. StructNoEnd is a
// completeness defect: the boundary was never observed, so the recording is
// incomplete rather than invalid.
const (
	StructNoEvents       = "no_events"
	StructNoIdentity     = "no_identity"
	StructMixedIdentity  = "mixed_identity"
	StructNoStart        = "no_start"
	StructMultipleStarts = "multiple_starts"
	StructStartNotFirst  = "start_not_first"
	StructNoEnd          = "no_end"
	StructMultipleEnds   = "multiple_ends"
	StructEndNotLast     = "end_not_last"
)

// Problem kinds. KindInvalid means the file is not one session log;
// KindIncomplete means it is one, but its end was never observed.
const (
	KindInvalid     = "invalid"
	KindIncomplete = "incomplete"
)

// Problem is one structural or completeness defect in a session log.
type Problem struct {
	Code string `json:"code"`
	Kind string `json:"kind"`
	// Seq is the offending event's sequence number. HasSeq distinguishes a
	// problem located at seq 0 from a problem that is not tied to an event:
	// a uint64 zero sentinel cannot represent both.
	Seq    uint64 `json:"seq"`
	HasSeq bool   `json:"-"`
	Detail string `json:"detail,omitempty"`
}

// Describe renders the problem for a diagnostic line.
func (p Problem) Describe() string {
	if p.HasSeq {
		return fmt.Sprintf("%s at seq %d", p.Code, p.Seq)
	}
	return p.Code
}

// StructureReport is the result of structural validation: is this file one
// session log, and was its boundary observed? It says nothing about the hash
// chain — that is Verify's job.
type StructureReport struct {
	Events     int        `json:"events"`
	SessionIDs []string   `json:"session_ids,omitempty"`
	Starts     int        `json:"starts"`
	Ends       int        `json:"ends"`
	FirstType  EventType `json:"first_type,omitempty"`
	LastType   EventType `json:"last_type,omitempty"`
	Problems   []Problem  `json:"problems,omitempty"`
}

// Valid reports whether the file is structurally one session log: at least one
// event, one non-empty session_id shared by every event, exactly one
// session/start as the first event, and any session/end as the last event.
// A missing end is not a structural defect, so an open recording stays
// "incomplete" rather than becoming "invalid".
func (r *StructureReport) Valid() bool {
	for _, p := range r.Problems {
		if p.Kind == KindInvalid {
			return false
		}
	}
	return true
}

// Complete reports whether the boundary was observed: structurally valid and
// exactly one session/end, last.
func (r *StructureReport) Complete() bool { return len(r.Problems) == 0 }

// FirstProblem returns the most fundamental problem, or nil when there is none.
func (r *StructureReport) FirstProblem() *Problem {
	if len(r.Problems) == 0 {
		return nil
	}
	p := r.Problems[0]
	return &p
}

// InspectStructure validates the session structure of a JSONL log: one
// identity, one start first, one end last. It does not verify the hash chain
// and does not resolve blobs. An unreadable line is an error; a readable but
// malformed session is a report.
func InspectStructure(r io.Reader) (*StructureReport, error) {
	rep := &StructureReport{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)

	seen := map[string]bool{}
	firstID := ""
	var identityProblems []Problem
	haveEmpty, haveMixed := false, false
	line := 0
	for sc.Scan() {
		line++
		text := strings.TrimSpace(sc.Text())
		if text == "" {
			continue
		}
		var e Event
		if err := json.Unmarshal([]byte(text), &e); err != nil {
			return nil, fmt.Errorf("structure: line %d: invalid JSON: %w", line, err)
		}
		rep.Events++
		if rep.Events == 1 {
			rep.FirstType = e.Type
		}
		rep.LastType = e.Type
		if e.Type == EventSessionStart {
			rep.Starts++
		}
		if e.Type == EventSessionEnd {
			rep.Ends++
		}
		if e.SessionID == "" {
			if !haveEmpty {
				haveEmpty = true
				identityProblems = append(identityProblems, Problem{
					Code: StructNoIdentity, Kind: KindInvalid, Seq: e.Seq, HasSeq: true,
					Detail: "an event carries an empty session_id",
				})
			}
			continue
		}
		if firstID == "" {
			firstID = e.SessionID
		} else if e.SessionID != firstID && !haveMixed {
			haveMixed = true
			identityProblems = append(identityProblems, Problem{
				Code: StructMixedIdentity, Kind: KindInvalid, Seq: e.Seq, HasSeq: true,
				Detail: fmt.Sprintf("%d session ids in one file", len(rep.SessionIDs)+1),
			})
		}
		if !seen[e.SessionID] {
			seen[e.SessionID] = true
			rep.SessionIDs = append(rep.SessionIDs, e.SessionID)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("structure: read: %w", err)
	}

	switch {
	case rep.Events == 0:
		rep.Problems = append(rep.Problems, Problem{
			Code: StructNoEvents, Kind: KindInvalid,
			Detail: "the file contains no events",
		})
		return rep, nil
	}
	rep.Problems = append(rep.Problems, identityProblems...)
	switch {
	case rep.Starts == 0:
		rep.Problems = append(rep.Problems, Problem{
			Code: StructNoStart, Kind: KindInvalid,
			Detail: "no session/start event",
		})
	case rep.Starts > 1:
		rep.Problems = append(rep.Problems, Problem{
			Code: StructMultipleStarts, Kind: KindInvalid,
			Detail: fmt.Sprintf("%d session/start events", rep.Starts),
		})
	case rep.FirstType != EventSessionStart:
		rep.Problems = append(rep.Problems, Problem{
			Code: StructStartNotFirst, Kind: KindInvalid,
			Detail: fmt.Sprintf("first event is %q", rep.FirstType),
		})
	}
	switch {
	case rep.Ends == 0:
		rep.Problems = append(rep.Problems, Problem{
			Code: StructNoEnd, Kind: KindIncomplete,
			Detail: "no session/end event",
		})
	case rep.Ends > 1:
		rep.Problems = append(rep.Problems, Problem{
			Code: StructMultipleEnds, Kind: KindInvalid,
			Detail: fmt.Sprintf("%d session/end events", rep.Ends),
		})
	case rep.LastType != EventSessionEnd:
		rep.Problems = append(rep.Problems, Problem{
			Code: StructEndNotLast, Kind: KindInvalid,
			Detail: fmt.Sprintf("last event is %q", rep.LastType),
		})
	}
	return rep, nil
}
