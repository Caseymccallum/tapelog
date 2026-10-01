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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/Caseymccallum/tapelog/internal/session"
)

// Interaction is one recorded tool call with its recorded result.
type Interaction struct {
	Order    int             // position in the recorded session
	Tool     string          // tool name
	Args     json.RawMessage // redacted, canonical arguments
	ArgsHash string          // hex SHA-256 of canonical args
	IsError  bool            // recorded outcome was an error
	Result   json.RawMessage // recorded result (or error object when IsError)
	Verdict  string          // policy verdict at record time
	RuleID   string          // policy rule at record time
	Reason   string          // policy decision reason (incl. flow provenance)
}

// Tape is a loaded session log ready for replay.
type Tape struct {
	SessionID    string
	PolicyID     string
	Tools        []json.RawMessage // recorded tools/list descriptors
	Interactions []Interaction     // calls that have recorded results
	Unanswered   []Interaction     // calls without results (e.g. denied)
}

// LoadVerified parses a session log AFTER verifying its hash chain.
// It refuses to serve altered evidence as "the recorded session"
// (first bad seq + problem are named in the error) unless
// allowUnverified — the explicit escape hatch for tamper-testing
// workflows. The returned VerifyResult is always the chain verdict, so
// callers can surface a loud warning when replaying unverified.
//
// Plain Load stays permissive by design: `tapelog test`/fuzz/whatif
// assert over cassettes (behavioural semantics), and integrity is
// `tapelog verify`'s job. `tapelog replay` is the trust-bearing
// surface and uses this gate.
func LoadVerified(path string, allowUnverified bool) (*Tape, *session.VerifyResult, error) {
	res, err := session.VerifyFile(path)
	if err != nil {
		return nil, nil, err
	}
	if !res.OK() && !allowUnverified {
		return nil, res, fmt.Errorf(
			"session log failed verification (first bad event: seq %d: %s)\n"+
				"replay will not serve altered evidence as the recorded session; use --allow-unverified to replay anyway (tamper-testing workflows)",
			res.FirstBadSeq, res.Problem)
	}
	tape, err := Load(path)
	if err != nil {
		return nil, res, err
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
	resolve := func(raw json.RawMessage, what string, lineNo int) (json.RawMessage, error) {
		out, err := blobs.Resolve(raw)
		if err != nil {
			return nil, fmt.Errorf("line %d: %s: %w", lineNo, what, err)
		}
		return out, nil
	}

	c := &Tape{}
	type pending struct {
		idx      int
		answered bool
	}
	var calls []*Interaction
	byID := map[string]*pending{}

	sc := bufio.NewScanner(f)
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
			c.SessionID = e.SessionID
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
			it := Interaction{
				Order:    len(calls) + 1,
				Tool:     p.Tool,
				Args:     args,
				ArgsHash: hashArgs(args),
			}
			byID[string(p.ID)] = &pending{idx: len(calls)}
			calls = append(calls, &it)
		case session.EventPolicyDecision:
			var p session.PolicyDecisionPayload
			if err := json.Unmarshal(e.Payload, &p); err == nil {
				if pend := byID[string(p.ID)]; pend != nil {
					calls[pend.idx].Verdict = p.Verdict
					calls[pend.idx].RuleID = p.RuleID
					calls[pend.idx].Reason = p.Reason
				}
			}
		case session.EventToolResult:
			var p session.ToolResultPayload
			if err := json.Unmarshal(e.Payload, &p); err == nil {
				if pend := byID[string(p.ID)]; pend != nil {
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

	for _, pend := range byID {
		if pend.answered {
			c.Interactions = append(c.Interactions, *calls[pend.idx])
		} else {
			c.Unanswered = append(c.Unanswered, *calls[pend.idx])
		}
	}
	sortInteractions(c.Interactions)
	sortInteractions(c.Unanswered)
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
