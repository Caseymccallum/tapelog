// Package replay implements tapelog's deterministic re-execution: a
// recorded session log ("tapelog") can be replayed as a hermetic MCP
// server that answers tool calls from the recording — VCR semantics for
// agent sessions.
//
// Rules (documented in spec/session-log-v0.md):
//   - matching runs on REDACTED canonical arguments: recordings store
//     redacted data, redaction is deterministic, so live calls containing
//     fresh secrets still match their recording.
//   - each recorded interaction is consumed once (FIFO); calling a tool
//     twice requires it to have been recorded twice.
//   - fail-loud: a call without a matching recording gets a structured
//     error and is counted — never silently invented.
package replay

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Caseymccallum/tapelog/internal/session"
)

// Interaction is one recorded tool call with its recorded result.
type Interaction struct {
	// RequestID is the JSON-RPC id the call was made under. When that id was
	// shared by more than one outstanding call, Ambiguous is set: this
	// interaction's result cannot be tied to this request.
	RequestID string
	Ambiguous bool
	Order     int             // position in the recorded session
	Tool      string          // tool name
	Args      json.RawMessage // redacted, canonical arguments
	ArgsHash  string          // hex SHA-256 of canonical args
	IsError   bool            // recorded outcome was an error
	Result    json.RawMessage // recorded result (or error object when IsError)
	Verdict   string          // policy verdict at record time
	RuleID    string          // policy rule at record time
	Reason    string          // policy decision reason (incl. flow provenance)
	// ConversationID is the CLIENT-DECLARED conversation this call named in its
	// request `_meta`. It is correlation only: not a session boundary, not an
	// identity, and a client that declares nothing leaves it empty. It exists
	// so a regression case can scope a claim to one conversation.
	ConversationID string
	// verdictGuessed records that Verdict was attached by request id alone,
	// before the id was known to be shared. If the id turns out ambiguous the
	// guess is cleared, so a reader cannot mistake it for a recorded decision.
	verdictGuessed bool
}

// Tape is a loaded session log ready for replay.
type Tape struct {
	SessionID    string
	PolicyID     string
	Tools        []json.RawMessage // recorded tools/list descriptors
	Interactions []Interaction     // calls that have recorded results
	Unanswered   []Interaction     // calls without results (e.g. denied)
	// AmbiguousIDs lists request ids that were shared by more than one
	// outstanding call. The transport cannot say which of those calls a
	// later result answers, so serving such a recording would present a
	// guess as the recorded outcome.
	AmbiguousIDs []string
}

// LoadVerified parses a session log AFTER verifying its hash chain and its
// session structure. It refuses to serve altered evidence as "the recorded
// session" (first bad seq + problem are named in the error) unless
// allowUnverified, and refuses a file that is not one complete valid session
// unless allowInvalidStructure. The returned VerifyResult is always the chain
// verdict, so callers can surface a loud warning when replaying unverified.
//
// Plain Load stays permissive by design: `tapelog test`/fuzz/whatif
// assert over cassettes (behavioural semantics), and integrity is
// `tapelog verify`'s job. `tapelog replay` is the trust-bearing
// surface and uses this gate.
func LoadVerified(path string, allowUnverified bool) (*Tape, *session.VerifyResult, error) {
	return LoadVerifiedStructure(path, allowUnverified, false)
}

// LoadVerifiedStructure is LoadVerified with an explicit escape hatch for the
// structure gate. allowUnverified bypasses the hash chain; allowInvalidStructure
// bypasses the requirement that the file be one complete valid session log.
func LoadVerifiedStructure(path string, allowUnverified, allowInvalidStructure bool) (*Tape, *session.VerifyResult, error) {
	return LoadVerifiedAttributable(path, allowUnverified, allowInvalidStructure, false)
}

// LoadVerifiedAttributable is LoadVerifiedStructure with an explicit escape
// hatch for request-id ambiguity. When two calls share a request id while
// both are outstanding, the recording cannot say which call a later result
// answers; replay refuses it rather than pairing by assumption, unless
// allowAmbiguous is set (tamper-testing and forensic workflows).
func LoadVerifiedAttributable(path string, allowUnverified, allowInvalidStructure, allowAmbiguous bool) (*Tape, *session.VerifyResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	res, err := session.Verify(bytes.NewReader(data))
	if err != nil {
		return nil, nil, err
	}
	if !res.OK() && !allowUnverified {
		return nil, res, fmt.Errorf(
			"session log failed verification (first bad event: seq %d: %s)\n"+
				"replay will not serve altered evidence as the recorded session; use --allow-unverified to replay anyway (tamper-testing workflows)",
			res.FirstBadSeq, res.Problem)
	}
	structure, serr := session.InspectStructure(bytes.NewReader(data))
	if serr != nil {
		return nil, res, fmt.Errorf("session log structure could not be read: %w", serr)
	}
	if (!structure.Valid() || !structure.Complete()) && !allowInvalidStructure {
		p := structure.FirstProblem()
		if p == nil {
			return nil, res, fmt.Errorf("session log is not one complete valid session; use --allow-invalid-structure to replay anyway")
		}
		return nil, res, fmt.Errorf("session log is not one complete valid session (%s); use --allow-invalid-structure to replay anyway", p.Describe())
	}
	// Parse the bytes that were just verified, not the path: re-opening it here
	// meant the chain gate and the structure gate inspected one read while the
	// served tape came from a second one, so a file swapped in between would be
	// served without ever having passed either gate.
	blobs := session.NewBlobStore(session.DefaultBlobDir(path))
	tape, err := loadReader(bytes.NewReader(data), blobs.Resolve)
	if err != nil {
		return nil, res, err
	}
	if len(tape.AmbiguousIDs) > 0 && !allowAmbiguous {
		return nil, res, fmt.Errorf(
			"recording reuses request id(s) %s while more than one call was outstanding; "+
				"which call a later result answers cannot be determined from the transport, "+
				"so replay will not present a guess as the recorded outcome — use --allow-ambiguous-ids to replay anyway",
			strings.Join(tape.AmbiguousIDs, ", "))
	}
	return tape, res, nil
}

// Load parses a session log produced by `tapelog record`. Blob
// references in args/result are resolved from the content-addressed
// store next to the log (`<log>.blobs/` — spec/session-log-v0.md §6.2)
// and fail loudly when missing or tampered: replay needs the blob store.
func Load(path string) (*Tape, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open tapelog: %w", err)
	}
	defer f.Close()

	blobs := session.NewBlobStore(session.DefaultBlobDir(path))
	return loadReader(f, blobs.Resolve)
}

// LoadInline reads a cassette without opening any external files. Workbench
// callers supply an already bounded, verified snapshot. Blob-backed recordings
// remain available in the CLI; this surface never follows untrusted blob paths.
func LoadInline(r io.Reader) (*Tape, error) {
	return loadReader(r, func(raw json.RawMessage) (json.RawMessage, error) {
		if session.IsBlobRef(raw) {
			return nil, fmt.Errorf("blob-backed payload: use the CLI with its blob store")
		}
		return raw, nil
	})
}

func loadReader(r io.Reader, resolveRaw func(json.RawMessage) (json.RawMessage, error)) (*Tape, error) {
	resolve := func(raw json.RawMessage, what string, lineNo int) (json.RawMessage, error) {
		out, err := resolveRaw(raw)
		if err != nil {
			return nil, fmt.Errorf("line %d: %s: %w", lineNo, what, err)
		}
		return out, nil
	}

	c := &Tape{}
	type pending struct {
		idx      int
		answered bool
		seq      uint64 // seq of the tools/call event, the target of parent_seq
	}
	var calls []*Interaction
	var pendings []*pending
	byID := map[string][]*pending{}
	// outstanding counts unanswered calls per request id: a second call under
	// an id that is still waiting makes the id ambiguous, because the
	// transport carries nothing that could tell the two responses apart.
	outstanding := map[string]int{}
	// ambiguousSet marks EVERY ambiguous id (used to label interactions);
	// ambiguousList is only the bounded, human-facing list in the error.
	ambiguousSet := map[string]bool{}
	var ambiguous []string
	noteAmbiguous := func(id string) {
		if ambiguousSet[id] {
			return
		}
		ambiguousSet[id] = true
		if len(ambiguous) < 16 {
			ambiguous = append(ambiguous, id)
		}
	}

	// firstOpen returns the earliest entry still open by the predicate.
	// Request ids may legitimately repeat across a long-lived recording,
	// so matching is FIFO per id: a last-write-wins map silently dropped
	// the earlier call and paired its result with the wrong request.
	firstOpen := func(q []*pending, open func(*pending) bool) *pending {
		for _, p := range q {
			if open(p) {
				return p
			}
		}
		return nil
	}

	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var e session.Event
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			return nil, fmt.Errorf("line %d: invalid event: %w", lineNo, err)
		}
		switch e.Type {
		case session.EventSessionStart:
			if c.SessionID == "" {
				c.SessionID = e.SessionID
			}
			var p session.SessionStartPayload
			if err := json.Unmarshal(e.Payload, &p); err == nil {
				c.PolicyID = p.PolicyID
			}
		case session.EventToolsList:
			var p session.ToolsListPayload
			if err := json.Unmarshal(e.Payload, &p); err == nil {
				c.Tools = mergeTools(c.Tools, p.Tools)
			}
		case session.EventToolCall:
			var p session.ToolCallPayload
			if err := json.Unmarshal(e.Payload, &p); err != nil {
				return nil, fmt.Errorf("line %d: bad tools/call payload: %w", lineNo, err)
			}
			args, err := resolve(p.Args, "args", lineNo)
			if err != nil {
				return nil, err
			}
			key := string(p.ID)
			it := Interaction{
				Order:          len(calls) + 1,
				RequestID:      key,
				Tool:           p.Tool,
				Args:           args,
				ArgsHash:       hashArgs(args),
				ConversationID: p.ConversationID,
			}
			pend := &pending{idx: len(calls), seq: e.Seq}
			if outstanding[key] > 0 {
				noteAmbiguous(key)
			}
			outstanding[key]++
			byID[key] = append(byID[key], pend)
			pendings = append(pendings, pend)
			calls = append(calls, &it)
		case session.EventPolicyDecision:
			var p session.PolicyDecisionPayload
			if err := json.Unmarshal(e.Payload, &p); err == nil {
				if p.Verdict == "deny" {
					// A denied call is settled by its decision: no upstream
					// result is expected. Decrement the outstanding count, or
					// the next call under this id looks falsely ambiguous.
					// The call still accepts a result if the recording has one
					// — a result after a deny is the no_deny_bypassed witness,
					// and dropping it would hide the very invariant we test.
					if key := string(p.ID); outstanding[key] > 0 {
						outstanding[key]--
					}
				}
				// A decision may be superseded by a later one for the same call
				// (decision history: the last recorded verdict wins). It names the
				// call it decided in parent_seq, so use that: matching by request id
				// alone attached the second call's decision to the first when an id
				// was shared by two outstanding calls, which let the Regression Lab
				// report a denial for a tool that was allowed. FIFO by id stays as
				// the fallback for logs written before parent_seq existed, but only
				// when the id was never shared — otherwise the verdict would be a
				// guess indistinguishable from evidence.
				var target *pending
				if p.ParentSeq != nil {
					for _, q := range byID[string(p.ID)] {
						if q.seq == *p.ParentSeq {
							target = q
							break
						}
					}
					if target == nil {
						// parent_seq names a call this reader did not index: do not
						// silently attach the verdict to some other call.
						break
					}
				} else if !ambiguousSet[string(p.ID)] {
					target = firstOpen(byID[string(p.ID)], func(q *pending) bool { return !q.answered })
				}
				if target != nil {
					calls[target.idx].Verdict = p.Verdict
					calls[target.idx].RuleID = p.RuleID
					calls[target.idx].Reason = p.Reason
					calls[target.idx].verdictGuessed = p.ParentSeq == nil
				}
			}
		case session.EventToolResult:
			var p session.ToolResultPayload
			if err := json.Unmarshal(e.Payload, &p); err == nil {
				if pend := firstOpen(byID[string(p.ID)], func(q *pending) bool { return !q.answered }); pend != nil {
					key := string(p.ID)
					if outstanding[key] > 0 {
						outstanding[key]--
					}
					it := calls[pend.idx]
					it.IsError = p.IsError
					result, err := resolve(p.Result, "result", lineNo)
					if err != nil {
						return nil, err
					}
					it.Result = result
					pend.answered = true
				}
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read tapelog: %w", err)
	}

	for _, pend := range pendings {
		if pend.answered {
			c.Interactions = append(c.Interactions, *calls[pend.idx])
		} else {
			c.Unanswered = append(c.Unanswered, *calls[pend.idx])
		}
	}
	sortInteractions(c.Interactions)
	sortInteractions(c.Unanswered)
	c.AmbiguousIDs = ambiguous // file order, deduped on insertion
	if len(ambiguous) > 0 {
		// Ambiguity can be discovered after a call was parsed (a second call
		// under the same id arrives later), so mark interactions at the end.
		mark := ambiguousSet
		for i := range c.Interactions {
			c.Interactions[i].Ambiguous = mark[c.Interactions[i].RequestID]
			if c.Interactions[i].Ambiguous && c.Interactions[i].verdictGuessed {
				c.Interactions[i].Verdict, c.Interactions[i].RuleID, c.Interactions[i].Reason = "", "", ""
			}
		}
		for i := range c.Unanswered {
			c.Unanswered[i].Ambiguous = mark[c.Unanswered[i].RequestID]
			if c.Unanswered[i].Ambiguous && c.Unanswered[i].verdictGuessed {
				c.Unanswered[i].Verdict, c.Unanswered[i].RuleID, c.Unanswered[i].Reason = "", "", ""
			}
		}
	}
	return c, nil
}

// sortInteractions orders interactions by recorded position (insertion
// sort keeps the file dependency-free and n is small).
func sortInteractions(s []Interaction) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j].Order < s[j-1].Order; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// mergeTools merges a recorded listing into the tool catalog. The newest
// descriptor per tool name wins (faithful to what the agent last saw).
func mergeTools(catalog, listing []json.RawMessage) []json.RawMessage {
	for _, tool := range listing {
		var desc struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(tool, &desc); err != nil || desc.Name == "" {
			continue
		}
		replaced := false
		for i, existing := range catalog {
			var e struct {
				Name string `json:"name"`
			}
			_ = json.Unmarshal(existing, &e)
			if e.Name == desc.Name {
				catalog[i] = tool
				replaced = true
				break
			}
		}
		if !replaced {
			catalog = append(catalog, tool)
		}
	}
	return catalog
}

// hashArgs returns the hex SHA-256 of an argument object's canonical form.
func hashArgs(args json.RawMessage) string {
	canon, err := session.CanonicalJSON(args)
	if err != nil {
		sum := sha256.Sum256(args)
		return hex.EncodeToString(sum[:])
	}
	sum := sha256.Sum256([]byte(canon))
	return hex.EncodeToString(sum[:])
}
