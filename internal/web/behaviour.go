package web

// Cross-session behaviour intelligence (M6).
//
// Everything here is derived from recordings Tapelog actually mediated:
// tools/call, policy/decision and tools/result events. It infers nothing about
// agent reasoning, prompts, token usage or built-in client operations, because
// none of that is in the log.
//
// Two rules shape the code:
//
//  1. Counts are never mixed. A "call" is one tools/call event, a "result" is
//     one tools/result event, and a deny is a policy/decision verdict. One call
//     can carry several decisions and at most one result, so these numbers do
//     not sum to each other and the API keeps them apart.
//  2. Repetition is reported as an observation, not a verdict. A polling tool
//     and a stuck agent can produce the same trace. Every finding carries the
//     evidence that produced it plus an explicit interpretation that says so.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Caseymccallum/tapelog/internal/behaviour"
	"github.com/Caseymccallum/tapelog/internal/session"
	"github.com/Caseymccallum/tapelog/internal/taint"
)

const (
	// maxAnalysisFiles and maxAnalysisBytes bound one overview request. The
	// catalogue already caps a directory listing at 200 files; analysis reads
	// fewer files and less data because it keeps per-call detail in memory.
	maxAnalysisFiles = 120
	maxAnalysisBytes = 48 << 20

	// minRepeatCount is the threshold for reporting repeated activity. Three is
	// the lowest number that can distinguish a pattern from a coincidence, and
	// it is the threshold other agent runtimes converged on.
	minRepeatCount = behaviour.MinRepeatCount

	// maxFindingsPerSession keeps a pathological recording from flooding the
	// overview with every window it contains.
	maxFindingsPerSession = behaviour.MaxFindingsPerSession

	// hashChars is how much of a SHA-256 digest identifies an argument or
	// result body. 16 hex characters (64 bits) make an accidental collision
	// vanishingly unlikely inside one recording and keep the cache small.
	hashChars = 16
)

// callRecord is one recorded tool call reduced to what behaviour analysis
// needs. Argument and result bodies are deliberately not retained: the index is
// bounded, and the recording stays the source of the full evidence.
type callRecord = behaviour.CallRecord

// failureSignature is an explainable group key for recorded failures: the tool,
// the recorded error code (when there is one) and the normalised opening words
// of the error message. Volatile parts — numbers, identifiers, paths, URLs and
// quoted values — are replaced before matching, so two occurrences of the same
// problem group together while unrelated messages that merely share a word do
// not.
type failureSignature struct {
	ID       string `json:"id"`
	Tool     string `json:"tool"`
	Server   string `json:"server,omitempty"`
	Code     string `json:"code,omitempty"`
	Label    string `json:"label"`
	Fragment string `json:"fragment,omitempty"`
	Criteria string `json:"criteria"`
}

// sessionAnalysis is the bounded per-recording view the behaviour surfaces
// share. It is immutable once published to the cache.
type sessionAnalysis struct {
	Info    SessionInfo
	Calls   []callRecord
	Sigs    map[string]*failureSignature
	Repeats []repeatFinding
	// Attributed is true when any call carried a valid conversation id, in
	// which case repeats are measured per conversation rather than per session.
	Attributed bool
	// UnpairedResults counts recorded tools/result events that no recorded
	// call claims. Such a result cannot be attributed to a conversation at
	// all, so a view that reports "no ambiguity" while holding one is wrong:
	// the recording is incomplete in a way the counts must show.
	UnpairedResults int
	// UnpairedAmbiguous counts unpaired results that were themselves marked
	// ambiguous (their request id was shared).
	UnpairedAmbiguous int
	// UnknownSourceDecisions counts recorded policy decisions whose reason
	// names an unknown value source (taint.UnknownSource). It is what the
	// recording can say about sticky provenance uncertainty: the decisions
	// that were made, not the state of a live policy engine.
	UnknownSourceDecisions int
	Notes                  []string
}

// analysed is one recording as the index saw it, with the bookkeeping needed to
// explain what was skipped and why.
type analysed struct {
	Analysis *sessionAnalysis
	File     string
	Reason   string // why this file was not analysed
	Skipped  bool
}

// --- per-call extraction --------------------------------------------------

// argsKey returns the recording's argument identity: the engine's own
// canonical-JSON SHA-256 (see internal/replay), truncated for storage. The
// comparison and test engines use the same canonical form, so "identical
// arguments" means the same thing on every surface.
func argsKey(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	canon, err := session.CanonicalJSON(raw)
	if err != nil {
		sum := sha256.Sum256(raw)
		return hex.EncodeToString(sum[:])[:hashChars]
	}
	sum := sha256.Sum256([]byte(canon))
	return hex.EncodeToString(sum[:])[:hashChars]
}

// resultKey identifies a recorded result body exactly as it was recorded.
func resultKey(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])[:hashChars]
}

// analyse reduces a decoded recording to the behaviour index. It reuses the
// workbench's activity pairing (parent_seq, falling back to a unique request
// ID), so a call means the same thing here as in the timeline, and reads
// argument and result bodies only to fingerprint and classify them.
func analyseRecording(rec *recording) *sessionAnalysis {
	a := &sessionAnalysis{Info: rec.Info, Sigs: map[string]*failureSignature{}}
	if rec.Info.Problem != "" && rec.Info.Integrity != "verified" {
		a.Notes = append(a.Notes, "Integrity: "+rec.Info.Problem)
	}
	args := make(map[uint64]json.RawMessage, len(rec.Activities))
	results := make(map[uint64]json.RawMessage, len(rec.Activities))
	// Client-declared conversation per call, and results whose pairing was
	// ambiguous. Both are facts read straight from the recording.
	convs := make(map[uint64]string, len(rec.Activities))
	ambiguous := make(map[uint64]bool, len(rec.Activities))
	// A result can be ambiguous even when it could not be paired to a call at
	// all (the shared-id case leaves ResultSeq 0), so ambiguity is tracked by
	// REQUEST ID as well as by result seq.
	ambiguousIDs := map[string]bool{}
	callIDs := make(map[uint64]string, len(rec.Activities))
	for _, e := range rec.Events {
		switch e.Type {
		case session.EventToolCall:
			var p session.ToolCallPayload
			if json.Unmarshal(e.Payload, &p) == nil {
				args[e.Seq] = p.Args
				convs[e.Seq] = p.ConversationID
				callIDs[e.Seq] = string(p.ID)
			}
		case session.EventPolicyDecision:
			// Unknown provenance is a recorded fact when a decision's reason
			// names it. The policy engine keeps that uncertainty sticky for
			// the rest of the session, so a later decision may rest on a call
			// this recording does not show; the count is evidence, not proof
			// that anything was contaminated.
			var p session.PolicyDecisionPayload
			if json.Unmarshal(e.Payload, &p) == nil && strings.Contains(p.Reason, taint.UnknownSource) {
				a.UnknownSourceDecisions++
			}
		case session.EventToolResult:
			var p session.ToolResultPayload
			if json.Unmarshal(e.Payload, &p) == nil {
				results[e.Seq] = p.Result
				if p.AttributionAmbiguous {
					ambiguous[e.Seq] = true
					ambiguousIDs[string(p.ID)] = true
				}
			}
		}
	}
	a.Calls = make([]callRecord, 0, len(rec.Activities))
	for _, act := range rec.Activities {
		c := callRecord{
			Seq:                  act.Seq,
			TS:                   act.TS,
			EpochMS:              epochMillis(act.TS),
			Tool:                 act.Tool,
			Server:               act.Server,
			ArgsKey:              argsKey(args[act.Seq]),
			Status:               act.Status,
			Verdict:              act.Verdict,
			Rule:                 act.Rule,
			DurationMS:           act.DurationMS,
			ResultSeq:            act.ResultSeq,
			IsError:              act.Status == "failed",
			ConversationID:       convs[act.Seq],
			CorrelationAmbiguous: ambiguous[act.ResultSeq] || ambiguousIDs[callIDs[act.Seq]],
		}
		if act.ResultSeq != 0 {
			body := results[act.ResultSeq]
			c.ResultKey = resultKey(body)
			if c.IsError {
				sig := signatureFor(c.Tool, c.Server, body)
				c.Signature = sig.ID
				if _, seen := a.Sigs[sig.ID]; !seen {
					a.Sigs[sig.ID] = sig
				}
			}
		}
		a.Calls = append(a.Calls, c)
	}
	a.Attributed = behaviour.HasAttribution(a.Calls)
	// A result no call claims. It cannot belong to a conversation, and it must
	// not read as "nothing ambiguous here".
	claimed := make(map[uint64]bool, len(rec.Activities))
	for _, act := range rec.Activities {
		if act.ResultSeq != 0 {
			claimed[act.ResultSeq] = true
		}
	}
	for seq := range results {
		if claimed[seq] {
			continue
		}
		a.UnpairedResults++
		if ambiguous[seq] {
			a.UnpairedAmbiguous++
		}
	}
	if a.Attributed {
		// A call repeated in four conversations is four small groups, not one
		// large loop: measure repetition within each conversation.
		a.Repeats = nil
		for _, g := range behaviour.GroupConversations(a.Calls) {
			a.Repeats = append(a.Repeats, g.Repeats...)
		}
	} else {
		a.Repeats = detectRepeats(a.Calls)
	}
	return a
}

// epochMillis parses a recorded RFC3339 timestamp. A missing or unparseable
// timestamp yields 0, which every caller treats as "timing unavailable" rather
// than as 1970.
func epochMillis(ts string) int64 {
	if ts == "" {
		return 0
	}
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return 0
	}
	return t.UnixMilli()
}

// --- failure signatures ---------------------------------------------------

// signatureFor derives an explainable group key from a recorded error body.
// MCP servers report failures in several shapes, so the extractor accepts the
// JSON-RPC error object, a plain message and content blocks. When no text can be
// read the signature still exists, with an explicit "no message" label: silent
// failures are worth grouping too.
func signatureFor(tool, server string, body json.RawMessage) *failureSignature {
	code, message := errorParts(body)
	normalised := normaliseMessage(message)
	label := normalised
	if label == "" {
		if code != "" {
			label = "Error reported with no message (code " + code + ")"
		} else {
			label = "Error reported with no recorded message"
		}
	}
	sig := &failureSignature{
		Tool:     tool,
		Server:   server,
		Code:     code,
		Label:    shortText(label, 200),
		Fragment: signatureFragment(message),
	}
	sig.ID = signatureID(tool, server, code, normalised)
	parts := []string{"same tool"}
	if server != "" && server != "Not recorded" {
		parts = append(parts, "same recorded server")
	}
	if code != "" {
		parts = append(parts, "same error code "+code)
	} else {
		parts = append(parts, "no error code")
	}
	if normalised == "" {
		parts = append(parts, "no readable message")
	} else {
		parts = append(parts, "the same first words of the message once numbers, identifiers, paths, URLs and quoted values are replaced and trailing bracketed detail is dropped")
	}
	sig.Criteria = strings.Join(parts, "; ")
	return sig
}

// signatureID hashes the matching key so the API can reference a group without
// treating the message itself as the identifier.
func signatureID(tool, server, code, normalised string) string {
	if server == "Not recorded" {
		server = ""
	}
	sum := sha256.Sum256([]byte(tool + "\x00" + server + "\x00" + code + "\x00" + normalised))
	return hex.EncodeToString(sum[:])[:hashChars]
}

// errorParts pulls a code and the most informative message out of a recorded
// error body.
func errorParts(body json.RawMessage) (code, message string) {
	var v any
	if len(body) == 0 {
		return "", ""
	}
	if json.Unmarshal(body, &v) != nil {
		return "", strings.TrimSpace(string(body))
	}
	return errorFromValue(v)
}

func errorFromValue(v any) (code, message string) {
	switch t := v.(type) {
	case string:
		return "", strings.TrimSpace(t)
	case map[string]any:
		if e, ok := t["error"]; ok {
			if c, m := errorFromValue(e); m != "" || c != "" {
				return c, m
			}
		}
		c := scalarCode(t["code"])
		if m, ok := t["message"].(string); ok && strings.TrimSpace(m) != "" {
			return c, m
		}
		if blocks, ok := t["content"].([]any); ok {
			for _, b := range blocks {
				if bm, ok := b.(map[string]any); ok {
					if text, ok := bm["text"].(string); ok && strings.TrimSpace(text) != "" {
						return c, text
					}
				}
			}
		}
	}
	return "", ""
}

func scalarCode(v any) string {
	switch t := v.(type) {
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case string:
		return strings.TrimSpace(t)
	}
	return ""
}

// Keep the M6 call sites and tests while sharing the normalisation primitive.
func normaliseMessage(message string) string  { return behaviour.NormaliseMessage(message) }
func signatureFragment(message string) string { return behaviour.SignatureFragment(message) }

// --- repeated activity detection -----------------------------------------

// The shared detector is also available to non-HTTP consumers.
type repeatFinding = behaviour.RepeatFinding

const (
	kindPossibleRetryLoop = behaviour.KindPossibleRetryLoop
	kindRepeatedFailure   = behaviour.KindRepeatedFailure
	kindRepeatedDenial    = behaviour.KindRepeatedDenial
	kindRepeatedActivity  = behaviour.KindRepeatedActivity
	kindAlternating       = behaviour.KindAlternating
	kindRepeatingBlock    = behaviour.KindRepeatingBlock
	kindInsufficient      = behaviour.KindInsufficient
)

func detectRepeats(calls []callRecord) []repeatFinding { return behaviour.DetectRepeats(calls) }

// --- bounded index with a snapshot cache ----------------------------------

// maxCacheEntries bounds retained analyses. A cached analysis is small (no
// argument or result bodies are kept) but a long-running workbench over a large
// library should still evict rather than grow.
const maxCacheEntries = 240

type behaviourCacheEntry struct {
	size     int64
	modified time.Time
	analysis *sessionAnalysis
}

type behaviourCache struct {
	mu      sync.Mutex
	entries map[string]*behaviourCacheEntry
	order   []string
}

func newBehaviourCache() *behaviourCache {
	return &behaviourCache{entries: map[string]*behaviourCacheEntry{}}
}

// get narrows cache candidates by metadata; callers must also compare the content revision.
func (c *behaviourCache) get(file string, size int64, modified time.Time) *sessionAnalysis {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[file]
	if !ok || e.size != size || !e.modified.Equal(modified) {
		return nil
	}
	return e.analysis
}

func (c *behaviourCache) put(file string, size int64, modified time.Time, a *sessionAnalysis) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, seen := c.entries[file]; !seen {
		c.order = append(c.order, file)
	}
	c.entries[file] = &behaviourCacheEntry{size: size, modified: modified, analysis: a}
	for len(c.order) > maxCacheEntries {
		oldest := c.order[0]
		c.order = c.order[1:]
		delete(c.entries, oldest)
	}
}

func (c *behaviourCache) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}

// behaviourFilter narrows the library before aggregation. A call-level filter
// would change the meaning of every rate, so filtering happens at recording
// level and the API reports what it excluded.
type behaviourFilter struct {
	From   time.Time
	To     time.Time
	Q      string
	Server string
	Tool   string
}

func (f behaviourFilter) query() string { return strings.ToLower(strings.TrimSpace(f.Q)) }

// windowed reports whether a date filter is set; recordings without a usable
// timestamp can then be neither included nor excluded honestly.
func (f behaviourFilter) windowed() bool { return !f.From.IsZero() || !f.To.IsZero() }

type skippedFile struct {
	File   string `json:"file"`
	Reason string `json:"reason"`
}

// behaviourIndex is one bounded pass over the library.
type behaviourIndex struct {
	Sessions  []*sessionAnalysis
	Files     int
	Analysed  int
	Matched   int
	Undated   int
	Cached    int
	Skipped   []skippedFile
	Capped    bool
	Truncated bool
	Filter    behaviourFilter
}

// buildBehaviourIndex walks the newest recordings, analysing what fits inside
// the budget and reporting everything it did not read. Nothing is executed and
// nothing is written: this is the same read the session list already performs,
// with per-call detail kept instead of discarded.
func (s *server) buildBehaviourIndex(f behaviourFilter) (*behaviourIndex, error) {
	idx := &behaviourIndex{Filter: f}
	if s.dir == "" {
		if s.logPath == "" {
			return idx, nil
		}
		rec, err := s.recording("")
		if err != nil {
			return nil, err
		}
		idx.Files, idx.Analysed, idx.Matched = 1, 1, 1
		idx.Sessions = append(idx.Sessions, analyseRecording(rec))
		return idx, nil
	}
	files, capped, err := listRecordings(s.dir)
	if err != nil {
		return nil, err
	}
	idx.Files, idx.Capped = len(files), capped
	budget := int64(maxAnalysisBytes)
	cache := s.behaviourCache()
	for _, file := range files {
		if idx.Analysed >= maxAnalysisFiles {
			idx.Truncated = true
			idx.Skipped = append(idx.Skipped, skippedFile{file.name, "analysis file limit reached; narrow the date range or use the CLI"})
			continue
		}
		// Re-read the bounded snapshot before accepting a cached index. Size
		// and mtime alone can be preserved while bytes change. Verification and
		// revision both describe these exact bytes, not merely file metadata.
		if file.size > budget {
			idx.Skipped = append(idx.Skipped, skippedFile{file.name, "analysis byte budget reached; narrow the date range or open this recording individually"})
			continue
		}
		rec, err := s.recording(file.name, budget)
		if err != nil {
			idx.Skipped = append(idx.Skipped, skippedFile{file.name, err.Error()})
			continue
		}
		budget -= int64(len(rec.Data))
		analysis := cache.get(file.name, file.size, file.modified)
		if analysis != nil && analysis.Info.Revision == rec.Info.Revision {
			idx.Cached++
		} else {
			analysis = analyseRecording(rec)
			cache.put(file.name, file.size, file.modified, analysis)
		}

		idx.Analysed++
		if !f.matches(analysis) {
			if f.windowed() && analysisUndated(analysis) {
				idx.Undated++
			}
			continue
		}
		idx.Matched++
		idx.Sessions = append(idx.Sessions, analysis)
	}
	return idx, nil
}

func analysisUndated(a *sessionAnalysis) bool {
	ts := a.Info.FirstTS
	if ts == "" {
		ts = a.Info.LastTS
	}
	_, err := time.Parse(time.RFC3339Nano, ts)
	return err != nil
}

// matches applies the recording-level filters.
func (f behaviourFilter) matches(a *sessionAnalysis) bool {
	info := a.Info
	if f.windowed() {
		ts := info.FirstTS
		if ts == "" {
			ts = info.LastTS
		}
		when, err := time.Parse(time.RFC3339Nano, ts)
		if err != nil {
			return false
		}
		if !f.From.IsZero() && when.Before(f.From) {
			return false
		}
		if !f.To.IsZero() && when.After(f.To) {
			return false
		}
	}
	if f.Server != "" {
		found := false
		for _, name := range info.Servers {
			if name == f.Server {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if q := f.query(); q != "" && !strings.Contains(strings.ToLower(sessionHaystack(a)), q) {
		return false
	}
	if f.Tool != "" {
		for _, c := range a.Calls {
			if c.Tool == f.Tool {
				return true
			}
		}
		return false
	}
	return true
}

// sessionHaystack is the searchable text of one recording: its recorded
// identity plus the tools it called. Argument values are never included.
func sessionHaystack(a *sessionAnalysis) string {
	info := a.Info
	parts := []string{info.Name, info.File, info.SessionID, info.Harness, strings.Join(info.Servers, " ")}
	seen := map[string]bool{}
	for _, c := range a.Calls {
		if !seen[c.Tool] {
			seen[c.Tool] = true
			parts = append(parts, c.Tool)
		}
	}
	return strings.Join(parts, " ")
}

// --- aggregation ----------------------------------------------------------

// behaviourCounts keeps calls, results, decisions and events apart, because
// adding them together would misreport what Tapelog observed.
type behaviourCounts struct {
	Sessions         int `json:"sessions"`
	Identities       int `json:"identities"`
	Conversations    int `json:"conversations"`
	Calls            int `json:"calls"`
	Results          int `json:"results"`
	Successes        int `json:"successes"`
	Failures         int `json:"failures"`
	Denies           int `json:"denies"`
	DenyDecisions    int `json:"deny_decisions"`
	Confirms         int `json:"confirms"`
	ConfirmDecisions int `json:"confirm_decisions"`
	Unanswered       int `json:"unanswered"`
	// AmbiguousCalls counts calls whose result pairing is unknown (a shared
	// request id). Unknown, never zero.
	AmbiguousCalls int `json:"ambiguous_calls"`
	// UnpairedResults counts recorded results that no call claims: evidence
	// the analysis can neither attribute nor rule out.
	UnpairedResults int `json:"unpaired_results"`
	// UnknownSourceDecisions counts recorded policy decisions whose reason
	// names an unknown value source. Provenance uncertainty is sticky at
	// decision time, so this is a fact about the recording, not a score.
	UnknownSourceDecisions int `json:"unknown_source_decisions"`
	Discovery              int `json:"discovery"`
	Events                 int `json:"events"`
	RepeatFindings         int `json:"repeat_findings"`
	RepeatAttention        int `json:"repeat_attention"`
	SignatureGroups        int `json:"signature_groups"`
}

// toolStat is per-tool activity across the analysed recordings.
type toolStat struct {
	Tool      string   `json:"tool"`
	Calls     int      `json:"calls"`
	Results   int      `json:"results"`
	Failures  int      `json:"failures"`
	Denies    int      `json:"denies"`
	Sessions  int      `json:"sessions"`
	ErrorRate *float64 `json:"error_rate"`
}

// deniedStat is one (tool, rule) pair that recorded denials. Sessions counts the
// recordings the pair appeared in, which is not the same as its call count.
type deniedStat struct {
	Tool     string `json:"tool"`
	Rule     string `json:"rule"`
	Reason   string `json:"reason,omitempty"`
	Calls    int    `json:"calls"`
	Sessions int    `json:"sessions"`
	LastTS   string `json:"last_ts,omitempty"`
	seen     map[string]bool
}

// occurrence is a pointer to one recorded event the reader can open. Index is
// the call's position in the recording's activity list, so the interface can
// page straight to it.
type occurrence struct {
	File      string `json:"file"`
	Seq       uint64 `json:"seq"`
	ResultSeq uint64 `json:"result_seq,omitempty"`
	TS        string `json:"ts,omitempty"`
	Tool      string `json:"tool"`
	Index     int    `json:"index"`
}

// signatureStat is one recurring failure group across recordings.
type signatureStat struct {
	ID          string       `json:"id"`
	Tool        string       `json:"tool"`
	Server      string       `json:"server,omitempty"`
	Code        string       `json:"code,omitempty"`
	Label       string       `json:"label"`
	Fragment    string       `json:"fragment,omitempty"`
	Criteria    string       `json:"criteria"`
	Occurrences int          `json:"occurrences"`
	Sessions    int          `json:"sessions"`
	FirstTS     string       `json:"first_ts,omitempty"`
	LastTS      string       `json:"last_ts,omitempty"`
	Files       []string     `json:"files"`
	Recent      []occurrence `json:"recent"`
}

// repeatStat aggregates one repeat finding across recordings.
type repeatStat struct {
	Confidence  string   `json:"confidence"`
	Kind        string   `json:"kind"`
	Tool        string   `json:"tool,omitempty"`
	Tools       []string `json:"tools,omitempty"`
	Findings    int      `json:"findings"`
	Occurrences int      `json:"occurrences"`
	Sessions    int      `json:"sessions"`
	// Scope says whether occurrences were counted within one conversation or
	// across the whole session: "conversation" or "session".
	Scope    string      `json:"scope,omitempty"`
	MaxCount int         `json:"max_count"`
	Files    []string    `json:"files"`
	Sample   *occurrence `json:"sample,omitempty"`
}

// sessionRow is one recording's behaviour, used for the trend table and the
// reference comparison. Rates are nullable so a reader can see when a
// percentage is undefined rather than zero.
type sessionRow struct {
	File           string   `json:"file"`
	Name           string   `json:"name"`
	Harness        string   `json:"harness,omitempty"`
	Servers        []string `json:"servers"`
	FirstTS        string   `json:"first_ts,omitempty"`
	LastTS         string   `json:"last_ts,omitempty"`
	Calls          int      `json:"calls"`
	Results        int      `json:"results"`
	Failures       int      `json:"failures"`
	Denies         int      `json:"denies"`
	Unanswered     int      `json:"unanswered"`
	AmbiguousCalls int      `json:"ambiguous_calls"`
	// Unpaired is the number of recorded results in this file that no call
	// claims, and UnknownSource the number of policy decisions whose reason
	// named an unknown value source. Both are recorded facts, not estimates.
	Unpaired        int      `json:"unpaired_results"`
	UnknownSource   int      `json:"unknown_source_decisions"`
	RepeatFindings  int      `json:"repeat_findings"`
	RepeatAttention int      `json:"repeat_attention"`
	ErrorRate       *float64 `json:"error_rate"`
	DenialRate      *float64 `json:"denial_rate"`
	DurationMS      *float64 `json:"duration_ms"`
	Complete        bool     `json:"complete"`
	Integrity       string   `json:"integrity"`
	ChainOK         bool     `json:"chain_ok"`
	SessionID       string   `json:"session_id,omitempty"`
	Structure       string   `json:"structure"`
}

// behaviourOverview is the whole cross-session answer in one response.
type behaviourOverview struct {
	Window   behaviourWindow  `json:"window"`
	Library  behaviourLibrary `json:"library"`
	Counts   behaviourCounts  `json:"counts"`
	Tools    []toolStat       `json:"tools"`
	Denied   []deniedStat     `json:"denied"`
	Failures []signatureStat  `json:"failures"`
	Repeats  []repeatStat     `json:"repeats"`
	// Conversations groups calls by CLIENT-DECLARED conversation id. The
	// unattributed remainder is reported last and never merged.
	Conversations []behaviour.ConversationSummary `json:"conversations"`
	// ConversationOrder says what ordered that list: "library" when every
	// group had a library-wide position (recordings in library order, then
	// sequence within each), "per-recording" when only a recording's own
	// sequence number was available. It exists so a per-recording order is
	// never read as a chronology.
	ConversationOrder string                 `json:"conversation_order"`
	Recurrence        []behaviour.Recurrence `json:"recurrence"`
	Trend             []sessionRow           `json:"trend"`
	Recent            []sessionRow           `json:"recent"`
	Definitions       []definition           `json:"definitions"`
	Notes             []string               `json:"notes"`
	Scope             string                 `json:"scope"`
}

type behaviourWindow struct {
	From string `json:"from,omitempty"`
	To   string `json:"to,omitempty"`
}

type behaviourLibrary struct {
	Files       int           `json:"files"`
	Analysed    int           `json:"analysed"`
	Matched     int           `json:"matched"`
	Undated     int           `json:"undated"`
	Cached      int           `json:"cached"`
	Capped      bool          `json:"capped"`
	Truncated   bool          `json:"truncated"`
	Skipped     []skippedFile `json:"skipped"`
	BudgetFiles int           `json:"budget_files"`
	BudgetBytes int           `json:"budget_bytes"`
}

// definition lets the interface explain a term using the same wording the
// engine uses, instead of a hand-written help string that can drift.
type definition struct {
	Term    string `json:"term"`
	Meaning string `json:"meaning"`
}

// rate returns a proportion and its honest empty case: a zero denominator
// yields nil, which the interface shows as undefined, never as 0%.
func rate(numerator, denominator int) *float64 {
	if denominator <= 0 {
		return nil
	}
	v := float64(numerator) / float64(denominator)
	return &v
}

// attention reports whether a finding is more than plain repetition. Findings
// that only describe repeats are counted separately so the interface does not
// imply every repeat is a problem.
func attention(kind string) bool {
	return kind != kindRepeatedActivity && kind != kindInsufficient
}

// aggregateOverview folds the analysed recordings into the cross-session view.
func aggregateOverview(idx *behaviourIndex) *behaviourOverview {
	ov := &behaviourOverview{
		Library: behaviourLibrary{
			Files: idx.Files, Analysed: idx.Analysed, Matched: idx.Matched,
			Undated: idx.Undated, Cached: idx.Cached, Capped: idx.Capped,
			Truncated: idx.Truncated, Skipped: idx.Skipped,
			BudgetFiles: maxAnalysisFiles, BudgetBytes: maxAnalysisBytes,
		},
		Tools:       []toolStat{},
		Denied:      []deniedStat{},
		Failures:    []signatureStat{},
		Repeats:     []repeatStat{},
		Trend:       []sessionRow{},
		Recent:      []sessionRow{},
		Definitions: behaviourDefinitions(),
	}
	if idx.Filter.From != (time.Time{}) {
		ov.Window.From = idx.Filter.From.Format("2006-01-02")
	}
	if idx.Filter.To != (time.Time{}) {
		ov.Window.To = idx.Filter.To.Format("2006-01-02")
	}
	tools := map[string]*toolStat{}
	denied := map[string]*deniedStat{}
	sigs := map[string]*signatureStat{}
	repeats := map[string]*repeatStat{}
	identities := map[string]bool{}
	var allCalls []behaviour.CallRecord
	for _, a := range idx.Sessions {
		row := rowFor(a)
		for _, id := range a.Info.SessionIDs {
			if id != "" {
				identities[id] = true
			}
		}
		if len(a.Info.SessionIDs) == 0 && a.Info.SessionID != "" {
			identities[a.Info.SessionID] = true
		}
		sessionTools := map[string]bool{}
		for i, c := range a.Calls {
			t := tools[c.Tool]
			if t == nil {
				t = &toolStat{Tool: c.Tool}
				tools[c.Tool] = t
			}
			t.Calls++
			if !sessionTools[c.Tool] {
				sessionTools[c.Tool] = true
				t.Sessions++
			}
			if c.ResultKey != "" {
				t.Results++
				if c.IsError {
					t.Failures++
				}
			}
			if c.Verdict == "deny" {
				t.Denies++
				d := denied[c.Tool+"\x00"+c.Rule]
				if d == nil {
					d = &deniedStat{Tool: c.Tool, Rule: c.Rule, seen: map[string]bool{}}
					denied[c.Tool+"\x00"+c.Rule] = d
				}
				d.Calls++
				if !d.seen[a.Info.File] {
					d.seen[a.Info.File] = true
					d.Sessions++
				}
				if d.LastTS == "" || c.TS > d.LastTS {
					d.LastTS = c.TS
				}
			}
			if c.Signature != "" {
				s := sigs[c.Signature]
				if s == nil {
					def := a.Sigs[c.Signature]
					s = &signatureStat{Files: []string{}, Recent: []occurrence{}}
					if def != nil {
						s.ID, s.Tool, s.Server, s.Code = def.ID, def.Tool, def.Server, def.Code
						s.Label, s.Fragment, s.Criteria = def.Label, def.Fragment, def.Criteria
					}
					sigs[c.Signature] = s
				}
				s.Occurrences++
				if s.FirstTS == "" || (c.TS != "" && c.TS < s.FirstTS) {
					s.FirstTS = c.TS
				}
				if c.TS > s.LastTS {
					s.LastTS = c.TS
				}
				if !contains(s.Files, a.Info.File) {
					s.Files = append(s.Files, a.Info.File)
					s.Sessions++
				}
				if len(s.Recent) < 3 {
					s.Recent = append(s.Recent, occurrence{File: a.Info.File, Seq: c.Seq, ResultSeq: c.ResultSeq, TS: c.TS, Tool: c.Tool, Index: i})
				}
			}
		}
		for _, f := range a.Repeats {
			bucket := f.Kind + "\x00" + f.Tool
			r := repeats[bucket]
			if r == nil {
				r = &repeatStat{Kind: f.Kind, Tool: f.Tool, Tools: f.Tools, Files: []string{}, Confidence: f.Confidence}
				repeats[bucket] = r
			}
			r.Findings++
			if a.Attributed {
				r.Scope = "conversation"
			} else {
				r.Scope = "session"
			}
			r.Occurrences += f.Count
			if f.Count > r.MaxCount {
				r.MaxCount = f.Count
			}
			if r.Sample == nil {
				r.Sample = &occurrence{File: a.Info.File, Seq: f.FirstSeq, Tool: f.Tool, Index: f.FirstIndex}
			}
			if !contains(r.Files, a.Info.File) {
				r.Files = append(r.Files, a.Info.File)
				r.Sessions++
			}
		}
		ov.Counts.RepeatFindings += row.RepeatFindings
		ov.Counts.RepeatAttention += row.RepeatAttention
		ov.Counts.Sessions++
		ov.Counts.Calls += row.Calls
		ov.Counts.Results += row.Results
		ov.Counts.Failures += row.Failures
		ov.Counts.Denies += row.Denies
		ov.Counts.Unanswered += row.Unanswered
		ov.Counts.AmbiguousCalls += row.AmbiguousCalls
		ov.Counts.UnpairedResults += row.Unpaired
		ov.Counts.UnknownSourceDecisions += row.UnknownSource
		ov.Counts.DenyDecisions += a.Info.Denies
		ov.Counts.ConfirmDecisions += a.Info.Confirms
		ov.Counts.Discovery += a.Info.Discovery
		ov.Counts.Events += a.Info.Events
		ov.Trend = append(ov.Trend, row)
	}

	ov.Counts.Successes = ov.Counts.Results - ov.Counts.Failures
	ov.Counts.Identities = len(identities)
	allCalls = libraryCalls(idx)
	ov.Conversations = behaviour.GroupConversations(allCalls)
	ov.Recurrence = behaviour.DetectRecurrence(allCalls)
	ov.ConversationOrder = "library"
	for _, g := range ov.Conversations {
		if g.Attributed {
			ov.Counts.Conversations++
		}
		if g.FirstLibrarySeq == 0 && len(allCalls) > 0 {
			// A group with no library position means the order fell back to a
			// per-recording sequence number; say so rather than implying one
			// library-wide chronology.
			ov.ConversationOrder = "per-recording"
		}
	}
	ov.Counts.SignatureGroups = len(sigs)
	ov.Failures = rankSignatures(sigs)
	ov.Tools = rankTools(tools)
	ov.Denied = rankDenied(denied)
	ov.Repeats = rankRepeats(repeats)
	sort.SliceStable(ov.Trend, func(i, j int) bool { return recordedTimeBefore(ov.Trend[i].FirstTS, ov.Trend[j].FirstTS, false) })
	for i := len(ov.Trend) - 1; i >= 0 && len(ov.Recent) < 8; i-- {
		ov.Recent = append(ov.Recent, ov.Trend[i])
	}
	ov.Notes = behaviourNotes(idx, ov)
	ov.Scope = "Derived only from recorded tools/call, policy/decision and tools/result events. A call is not a decision and a decision is not a result; denials are decision verdicts. Repeated activity is reported as an observation with its evidence, not as proof of a loop or of wasted work."
	return ov
}

// libraryCalls flattens the analysed recordings into one call list in library
// order: recordings in the order the index read them, then sequence within each
// file. Each call is a COPY stamped with its file, its ordinal within that file
// and its library-wide position — the cached analysis is shared between
// requests and must never be written to (that was a confirmed data race).
//
// It is the single definition of "the calls the library holds", so the overview,
// the conversation view and the comparison cannot disagree about which calls
// exist or what order they are in.
func libraryCalls(idx *behaviourIndex) []behaviour.CallRecord {
	var out []behaviour.CallRecord
	position := 0
	for _, a := range idx.Sessions {
		for i := range a.Calls {
			c := a.Calls[i]
			c.File = a.Info.File
			c.Index = i
			position++
			c.LibrarySeq = uint64(position)
			out = append(out, c)
		}
	}
	return out
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// rowFor computes one recording's behaviour row. It is the single definition of
// these numbers, so the trend table, the reference comparison and the overview
// totals cannot disagree.
func rowFor(a *sessionAnalysis) sessionRow {
	row := sessionRow{
		File: a.Info.File, Name: a.Info.Name, Harness: a.Info.Harness,
		Servers: a.Info.Servers, FirstTS: a.Info.FirstTS, LastTS: a.Info.LastTS,
		DurationMS: a.Info.DurationMS, Complete: a.Info.Complete,
		Integrity: a.Info.Integrity, ChainOK: a.Info.ChainOK,
		SessionID: a.Info.SessionID, Structure: a.Info.Structure,
		RepeatFindings: len(a.Repeats),
		Unpaired:       a.UnpairedResults, UnknownSource: a.UnknownSourceDecisions,
	}
	if row.Servers == nil {
		row.Servers = []string{}
	}
	for _, c := range a.Calls {
		row.Calls++
		if c.ResultKey != "" {
			row.Results++
			if c.IsError {
				row.Failures++
			}
		} else {
			row.Unanswered++
		}
		if c.CorrelationAmbiguous {
			row.AmbiguousCalls++
		}
		if c.Verdict == "deny" {
			row.Denies++
		}
	}
	for _, f := range a.Repeats {
		if attention(f.Kind) {
			row.RepeatAttention++
		}
	}
	row.ErrorRate = rate(row.Failures, row.Results)
	row.DenialRate = rate(row.Denies, row.Calls)
	return row
}

// rankSignatures orders failure groups by how often they happened, then by how
// widely they spread. The order is stable so a refresh does not reshuffle the
// list a reader is working through.
func rankSignatures(m map[string]*signatureStat) []signatureStat {
	out := make([]signatureStat, 0, len(m))
	for _, s := range m {
		out = append(out, *s)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Occurrences != out[j].Occurrences {
			return out[i].Occurrences > out[j].Occurrences
		}
		if out[i].Sessions != out[j].Sessions {
			return out[i].Sessions > out[j].Sessions
		}
		if out[i].Tool != out[j].Tool {
			return out[i].Tool < out[j].Tool
		}
		return out[i].Label < out[j].Label
	})
	return out
}

func rankTools(m map[string]*toolStat) []toolStat {
	out := make([]toolStat, 0, len(m))
	for _, t := range m {
		t.ErrorRate = rate(t.Failures, t.Results)
		out = append(out, *t)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Calls != out[j].Calls {
			return out[i].Calls > out[j].Calls
		}
		return out[i].Tool < out[j].Tool
	})
	return out
}

func rankDenied(m map[string]*deniedStat) []deniedStat {
	out := make([]deniedStat, 0, len(m))
	for _, d := range m {
		out = append(out, *d)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Calls != out[j].Calls {
			return out[i].Calls > out[j].Calls
		}
		if out[i].Tool != out[j].Tool {
			return out[i].Tool < out[j].Tool
		}
		return out[i].Rule < out[j].Rule
	})
	return out
}

func rankRepeats(m map[string]*repeatStat) []repeatStat {
	out := make([]repeatStat, 0, len(m))
	for _, r := range m {
		out = append(out, *r)
	}
	sort.SliceStable(out, func(i, j int) bool {
		ai, aj := attention(out[i].Kind), attention(out[j].Kind)
		if ai != aj {
			return ai
		}
		if out[i].Occurrences != out[j].Occurrences {
			return out[i].Occurrences > out[j].Occurrences
		}
		return out[i].Tool < out[j].Tool
	})
	return out
}

// behaviourDefinitions is the glossary the interface renders beside the counts.
// Keeping it in the API means the explanation cannot drift from the numbers.
func behaviourDefinitions() []definition {
	return []definition{
		{"Tool call", "One tools/call event that reached this boundary. A call is recorded whether or not it was answered."},
		{"Recorded result", "One tools/result event: the call was answered, successfully or with a tool error."},
		{"Tool error", "An answered call whose recorded result carried is_error. This is the server reporting a failure, not Tapelog deciding one."},
		{"Deny decision", "A policy/decision verdict of deny. One call can carry several decisions, so deny decisions and tool calls do not add up to each other."},
		{"Unanswered", "A call with no recorded result, including calls the boundary denied."},
		{"Discovered", "A tools/list event. Discovery is not activity: nothing was invoked."},
		{"Repeated activity", "Three or more calls with the same tool and identical canonical arguments, within one recording."},
		{"Failure group", "Failures with the same tool, error code and the same opening words of the message once numbers, identifiers, paths, URLs and quoted values are replaced and trailing bracketed detail is dropped."},
	}
}

// behaviourNotes states, in the response itself, everything that limits the
// numbers above.
func behaviourNotes(idx *behaviourIndex, ov *behaviourOverview) []string {
	var notes []string
	if ov.Counts.Calls == 0 && idx.Analysed > 0 {
		notes = append(notes, "No tool calls were found in the analysed recordings. Discovery and session boundaries are visible in each session's All events tab.")
	}
	if idx.Matched < idx.Analysed {
		notes = append(notes, fmt.Sprintf("%d of %d analysed recordings matched the current filters; the totals describe the matched set only.", idx.Matched, idx.Analysed))
	}
	if idx.Undated > 0 {
		notes = append(notes, fmt.Sprintf("%d recordings have no usable timestamp and are excluded while a date range is set.", idx.Undated))
	}
	if idx.Truncated {
		notes = append(notes, fmt.Sprintf("The index covers at most %d recordings per request; older recordings were not analysed. Narrow the date range to include them.", maxAnalysisFiles))
	}
	if len(idx.Skipped) > 0 {
		notes = append(notes, fmt.Sprintf("%d recordings could not be analysed inside the current budget; the list below says why. Open one individually to inspect it.", len(idx.Skipped)))
	}
	if idx.Capped {
		notes = append(notes, "The library holds more files than the workbench lists; only the newest recordings were considered.")
	}
	if ov.Counts.Unanswered > 0 {
		noun := "calls were"
		if ov.Counts.Unanswered == 1 {
			noun = "call was"
		}
		notes = append(notes, fmt.Sprintf("%d %s never answered. Rates are calculated over recorded results only, so they do not absorb %s.", ov.Counts.Unanswered, noun, map[bool]string{true: "that call", false: "those calls"}[ov.Counts.Unanswered == 1]))
	}
	if ov.Counts.Results == 0 && ov.Counts.Calls > 0 {
		notes = append(notes, "No results were recorded, so no error rate can be calculated. A percentage would be undefined here, not zero.")
	}
	// Three more evidence-quality facts. Each is emitted on its own, for the
	// same reason the integrity notes are: one defect must not hide another.
	if ov.Counts.AmbiguousCalls > 0 {
		notes = append(notes, fmt.Sprintf("%d calls share a request id with another outstanding call, so which response answers which is unknown. Their results are counted as ambiguous, never as success or failure.", ov.Counts.AmbiguousCalls))
	}
	if ov.Counts.UnpairedResults > 0 {
		noun, verb, subject, object := "recorded results", "are", "they", "them"
		if ov.Counts.UnpairedResults == 1 {
			noun, verb, subject, object = "recorded result", "is", "it", "it"
		}
		notes = append(notes, fmt.Sprintf("%d %s %s not claimed by any recorded call, so %s cannot be attributed to a conversation; a conversation that looks complete may be missing %s.", ov.Counts.UnpairedResults, noun, verb, subject, object))
	}
	if ov.Counts.UnknownSourceDecisions > 0 {
		what := "policy decisions were"
		if ov.Counts.UnknownSourceDecisions == 1 {
			what = "policy decision was"
		}
		notes = append(notes, fmt.Sprintf("%d %s made while a value's source was unknown. The policy engine keeps that uncertainty for the rest of the session, so a later decision can rest on activity that is not in this view. It means provenance is uncertain, not that anything was contaminated.", ov.Counts.UnknownSourceDecisions, what))
	}
	// Both defects can be present at once. Stopping at the first one meant the
	// warning a reader saw depended on file order, and the broken-chain warning
	// — the more serious of the two — could be suppressed entirely.
	var sawInvalid, sawIncomplete, sawStructureInvalid bool
	for _, a := range idx.Sessions {
		if a.Info.Integrity == "invalid" {
			sawInvalid = true
		}
		if a.Info.Structure == "invalid" {
			sawStructureInvalid = true
		}
		if !a.Info.Complete {
			sawIncomplete = true
		}
	}
	if sawInvalid {
		notes = append(notes, "At least one analysed recording failed its hash-chain check. Its activity is included, but treat it as untrusted evidence.")
	}
	// A broken chain and an invalid structure are different questions: the chain
	// can be intact on a mixed-identity or start-less file. Saying only the first
	// left structurally invalid evidence counted in these totals with no warning.
	if sawStructureInvalid {
		notes = append(notes, "At least one analysed recording is structurally invalid (for example more than one session identity, or no first session/start). Its activity is still counted here, so those totals include evidence that cannot establish one complete session.")
	}
	if sawIncomplete {
		notes = append(notes, "At least one analysed recording has no session/end event. A missing end can mean an interruption, a live session or a failed startup; it does not establish a failure.")
	}
	return notes
}

// --- HTTP surfaces --------------------------------------------------------

// parseWindow reads an inclusive date window from the query string. Dates are
// local calendar days, which is what a developer means when they say "last
// week"; the individual events keep their own recorded timestamps.
func parseWindow(q map[string][]string) (behaviourFilter, error) {
	var f behaviourFilter
	get := func(key string) string {
		if v, ok := q[key]; ok && len(v) > 0 {
			return strings.TrimSpace(v[0])
		}
		return ""
	}
	if v := get("from"); v != "" {
		t, err := time.Parse("2006-01-02", v)
		if err != nil {
			return f, fmt.Errorf("from must be a date like 2026-01-31")
		}
		f.From = t
	}
	if v := get("to"); v != "" {
		t, err := time.Parse("2006-01-02", v)
		if err != nil {
			return f, fmt.Errorf("to must be a date like 2026-01-31")
		}
		f.To = t.Add(24*time.Hour - time.Nanosecond)
	}
	if !f.From.IsZero() && !f.To.IsZero() && f.To.Before(f.From) {
		return f, fmt.Errorf("the end date is before the start date")
	}
	f.Q, f.Server, f.Tool = get("q"), get("server"), get("tool")
	return f, nil
}

func (s *server) behaviourAPI(w http.ResponseWriter, r *http.Request) {
	if !getOnly(w, r) {
		return
	}
	f, err := parseWindow(r.URL.Query())
	if err != nil {
		apiError(w, 400, err)
		return
	}
	idx, err := s.buildBehaviourIndex(f)
	if err != nil {
		apiError(w, 400, err)
		return
	}
	writeJSON(w, aggregateOverview(idx))
}

// repeatsAPI serves one recording's repeat findings, for the session tab. It
// always reads the current file rather than a cached snapshot, because the
// reader is looking at that exact recording.
func (s *server) repeatsAPI(w http.ResponseWriter, r *http.Request) {
	if !getOnly(w, r) {
		return
	}
	rec, err := s.recording(r.URL.Query().Get("file"))
	if err != nil {
		apiError(w, 400, err)
		return
	}
	a := analyseRecording(rec)
	used := map[string]bool{}
	for _, f := range a.Repeats {
		if f.Signature != "" {
			used[f.Signature] = true
		}
	}
	labels := map[string]string{}
	for id := range used {
		if def := a.Sigs[id]; def != nil {
			labels[id] = def.Label
		}
	}
	writeJSON(w, map[string]any{
		"session":    rec.Info,
		"findings":   a.Repeats,
		"signatures": labels,
		"notes":      a.Notes,
		"thresholds": map[string]any{"repeat_count": minRepeatCount, "block_lengths": []int{2, 3, 4}, "max_findings": maxFindingsPerSession},
		"scope":      "Deterministic detectors over recorded calls in this file. Identical arguments means the same canonical-JSON digest the comparison and test engines use. Repetition is reported, not judged: polling, pagination and retries all repeat. No argument or result values appear in these findings.",
	})
}

// signatureAPI explains one failure group and lists where it occurred, so a
// reader can check the grouping instead of trusting it.
func (s *server) signatureAPI(w http.ResponseWriter, r *http.Request) {
	if !getOnly(w, r) {
		return
	}
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" {
		apiError(w, 400, fmt.Errorf("id is required"))
		return
	}
	f, err := parseWindow(r.URL.Query())
	if err != nil {
		apiError(w, 400, err)
		return
	}
	idx, err := s.buildBehaviourIndex(f)
	if err != nil {
		apiError(w, 400, err)
		return
	}
	occurrences := []occurrence{}
	sessions := []string{}
	var def *failureSignature
	total := 0
	for _, a := range idx.Sessions {
		for i, c := range a.Calls {
			if c.Signature != id {
				continue
			}
			total++
			if def == nil {
				def = a.Sigs[id]
			}
			if !contains(sessions, a.Info.File) {
				sessions = append(sessions, a.Info.File)
			}
			if len(occurrences) < 200 {
				occurrences = append(occurrences, occurrence{File: a.Info.File, Seq: c.Seq, ResultSeq: c.ResultSeq, TS: c.TS, Tool: c.Tool, Index: i})
			}
		}
	}
	if total == 0 || def == nil {
		apiError(w, 404, fmt.Errorf("no analysed recording contains this failure group under the current filters"))
		return
	}
	writeJSON(w, map[string]any{
		"signature":   def,
		"occurrences": occurrences,
		"total":       total,
		"sessions":    sessions,
		"truncated":   total > len(occurrences),
		"library": map[string]any{
			"analysed": idx.Analysed, "matched": idx.Matched, "undated": idx.Undated,
			"skipped": idx.Skipped, "truncated": idx.Truncated,
		},
	})
}

// --- reference comparison -------------------------------------------------

// signatureDelta is one failure group as seen in a reference recording and in
// the recording under investigation.
type signatureDelta struct {
	ID        string `json:"id"`
	Tool      string `json:"tool"`
	Label     string `json:"label"`
	Criteria  string `json:"criteria,omitempty"`
	Reference int    `json:"reference"`
	Current   int    `json:"current"`
}

// repeatDelta is one repeat pattern counted in each recording.
type repeatDelta struct {
	Confidence           string   `json:"confidence"`
	Kind                 string   `json:"kind"`
	Tool                 string   `json:"tool,omitempty"`
	Tools                []string `json:"tools,omitempty"`
	Reference            int      `json:"reference"`
	Current              int      `json:"current"`
	ReferenceOccurrences int      `json:"reference_occurrences"`
	CurrentOccurrences   int      `json:"current_occurrences"`
}

// rateDelta is one metric shown with the denominator it was calculated over.
// Unit distinguishes a proportion from a rate like calls-per-minute, which must
// not be rendered as a percentage.
type rateDelta struct {
	ReferenceDurationMS *float64 `json:"reference_duration_ms"`
	CurrentDurationMS   *float64 `json:"current_duration_ms"`
	Metric              string   `json:"metric"`
	Unit                string   `json:"unit"`
	Reference           *float64 `json:"reference"`
	Current             *float64 `json:"current"`
	ReferenceN          int      `json:"reference_n"`
	CurrentN            int      `json:"current_n"`
	Note                string   `json:"note,omitempty"`
}

// behaviourDelta compares one recording with a chosen reference. It reports
// differences without ranking them: "new" means "absent from the reference",
// never "worse".
type behaviourDelta struct {
	Reference        sessionRow       `json:"reference"`
	Current          sessionRow       `json:"current"`
	NewFailures      []signatureDelta `json:"new_failures"`
	ResolvedFailures []signatureDelta `json:"resolved_failures"`
	NewDenials       []deniedStat     `json:"new_denials"`
	ResolvedDenials  []deniedStat     `json:"resolved_denials"`
	NewRepeats       []repeatDelta    `json:"new_repeats"`
	ResolvedRepeats  []repeatDelta    `json:"resolved_repeats"`
	Rates            []rateDelta      `json:"rates"`
	Caveats          []string         `json:"caveats"`
	Scope            string           `json:"scope"`
}

func signatureCounts(a *sessionAnalysis) map[string]int {
	out := map[string]int{}
	for _, c := range a.Calls {
		if c.Signature != "" {
			out[c.Signature]++
		}
	}
	return out
}

func denialCounts(a *sessionAnalysis) map[string]*deniedStat {
	out := map[string]*deniedStat{}
	for _, c := range a.Calls {
		if c.Verdict != "deny" {
			continue
		}
		k := c.Tool + "\x00" + c.Rule
		d := out[k]
		if d == nil {
			d = &deniedStat{Tool: c.Tool, Rule: c.Rule, Sessions: 1}
			out[k] = d
		}
		d.Calls++
		if d.LastTS == "" || c.TS > d.LastTS {
			d.LastTS = c.TS
		}
	}
	return out
}

// repeatTally counts one detector pattern inside a single recording. Both sides
// of a comparison use it, so neither side can be read from the other's field.
type repeatTally struct {
	confidence  string
	findings    int
	occurrences int
	tools       []string
}

func repeatCounts(a *sessionAnalysis) map[string]*repeatTally {
	out := map[string]*repeatTally{}
	for _, f := range a.Repeats {
		k := f.Kind + "\x00" + f.Tool
		t := out[k]
		if t == nil {
			t = &repeatTally{tools: f.Tools, confidence: f.Confidence}
			out[k] = t
		}
		t.findings++
		t.occurrences += f.Count
	}
	return out
}

// compareBehaviour diffs two analysed recordings at the behaviour level.
func compareBehaviour(ref, cur *sessionAnalysis) *behaviourDelta {
	d := &behaviourDelta{
		Reference:        rowFor(ref),
		Current:          rowFor(cur),
		NewFailures:      []signatureDelta{},
		ResolvedFailures: []signatureDelta{},
		NewDenials:       []deniedStat{},
		ResolvedDenials:  []deniedStat{},
		NewRepeats:       []repeatDelta{},
		ResolvedRepeats:  []repeatDelta{},
	}
	refSigs, curSigs := signatureCounts(ref), signatureCounts(cur)
	for id, n := range curSigs {
		if refSigs[id] != 0 {
			continue
		}
		s := signatureDelta{ID: id, Current: n}
		if def := cur.Sigs[id]; def != nil {
			s.Tool, s.Label, s.Criteria = def.Tool, def.Label, def.Criteria
		}
		d.NewFailures = append(d.NewFailures, s)
	}
	for id, n := range refSigs {
		if curSigs[id] != 0 {
			continue
		}
		s := signatureDelta{ID: id, Reference: n}
		if def := ref.Sigs[id]; def != nil {
			s.Tool, s.Label, s.Criteria = def.Tool, def.Label, def.Criteria
		}
		d.ResolvedFailures = append(d.ResolvedFailures, s)
	}
	sort.SliceStable(d.NewFailures, func(i, j int) bool {
		if d.NewFailures[i].Current != d.NewFailures[j].Current {
			return d.NewFailures[i].Current > d.NewFailures[j].Current
		}
		return d.NewFailures[i].Tool < d.NewFailures[j].Tool
	})
	sort.SliceStable(d.ResolvedFailures, func(i, j int) bool {
		return d.ResolvedFailures[i].Tool < d.ResolvedFailures[j].Tool
	})
	refDenied, curDenied := denialCounts(ref), denialCounts(cur)
	for k, v := range curDenied {
		if refDenied[k] == nil {
			d.NewDenials = append(d.NewDenials, *v)
		}
	}
	for k, v := range refDenied {
		if curDenied[k] == nil {
			d.ResolvedDenials = append(d.ResolvedDenials, *v)
		}
	}
	sort.SliceStable(d.NewDenials, func(i, j int) bool { return d.NewDenials[i].Calls > d.NewDenials[j].Calls })
	sort.SliceStable(d.ResolvedDenials, func(i, j int) bool { return d.ResolvedDenials[i].Calls > d.ResolvedDenials[j].Calls })
	refRep, curRep := repeatCounts(ref), repeatCounts(cur)
	split := func(key string) (kind, tool string) {
		parts := strings.SplitN(key, "\x00", 2)
		if len(parts) == 2 {
			return parts[0], parts[1]
		}
		return parts[0], ""
	}
	for k, v := range curRep {
		if refRep[k] == nil {
			kind, tool := split(k)
			d.NewRepeats = append(d.NewRepeats, repeatDelta{
				Kind: kind, Tool: tool, Tools: v.tools, Confidence: v.confidence,
				Current: v.findings, CurrentOccurrences: v.occurrences,
			})
		}
	}
	for k, v := range refRep {
		if curRep[k] == nil {
			kind, tool := split(k)
			d.ResolvedRepeats = append(d.ResolvedRepeats, repeatDelta{
				Kind: kind, Tool: tool, Tools: v.tools, Confidence: v.confidence,
				Reference: v.findings, ReferenceOccurrences: v.occurrences,
			})
		}
	}
	sort.SliceStable(d.NewRepeats, func(i, j int) bool { return d.NewRepeats[i].Current > d.NewRepeats[j].Current })
	sort.SliceStable(d.ResolvedRepeats, func(i, j int) bool { return d.ResolvedRepeats[i].Reference > d.ResolvedRepeats[j].Reference })
	d.Rates = []rateDelta{
		{Metric: "Tool-error rate (errors / recorded results)", Unit: "ratio", Reference: d.Reference.ErrorRate, Current: d.Current.ErrorRate, ReferenceN: d.Reference.Results, CurrentN: d.Current.Results},
		{Metric: "Deny rate (calls denied / calls)", Unit: "ratio", Reference: d.Reference.DenialRate, Current: d.Current.DenialRate, ReferenceN: d.Reference.Calls, CurrentN: d.Current.Calls},
		{Metric: "Unanswered rate (calls without a result / calls)", Unit: "ratio", Reference: rate(d.Reference.Unanswered, d.Reference.Calls), Current: rate(d.Current.Unanswered, d.Current.Calls), ReferenceN: d.Reference.Calls, CurrentN: d.Current.Calls},
		{Metric: "Calls per minute", Unit: "per_minute", Reference: perMinute(d.Reference), Current: perMinute(d.Current), ReferenceN: d.Reference.Calls, CurrentN: d.Current.Calls, ReferenceDurationMS: d.Reference.DurationMS, CurrentDurationMS: d.Current.DurationMS, Note: "N is the number of calls; duration_ms is the time denominator. Unequal durations make this a rough comparison."},
	}
	for i := range d.Rates {
		if d.Rates[i].ReferenceN == 0 || d.Rates[i].CurrentN == 0 {
			d.Rates[i].Note = strings.TrimSpace(d.Rates[i].Note + " Undefined on one side because its denominator is zero.")
		}
	}
	d.Caveats = []string{
		"A difference is an observation, not a regression or an improvement. The two recordings may cover different tasks, so neither raw counts nor rates establish cause.",
		"Failure groups are matched by tool, error code and the opening words of the normalised message, not by position in the recording.",
		"Repeat patterns are matched by detector kind and tool; occurrence counts are shown because one recording can contain the same pattern more often.",
	}
	if !ref.Info.Complete || !cur.Info.Complete {
		d.Caveats = append(d.Caveats, "At least one recording has no session/end event, so its totals may be truncated.")
	}
	if ref.Info.Integrity == "invalid" || cur.Info.Integrity == "invalid" {
		d.Caveats = append(d.Caveats, "At least one recording failed its hash-chain check; compare it with care.")
	}
	if ref.Info.Ambiguous || cur.Info.Ambiguous {
		d.Caveats = append(d.Caveats, "Request relationships are ambiguous in at least one recording. Per-call counts remain valid, but call-level pairing is uncertain; the Compare tab states its own caveats.")
	}
	if d.Reference.Results == 0 || d.Current.Results == 0 {
		d.Caveats = append(d.Caveats, "At least one recording has no recorded results, so its error rate is undefined rather than zero.")
	}
	d.Scope = "Behaviour-level comparison of two recordings. Nothing is executed and unmatched calls are not hidden: use the session Compare tab for the call-level diff."
	return d
}

// perMinute converts a session's recorded duration into a call rate.
func perMinute(r sessionRow) *float64 {
	if r.DurationMS == nil || *r.DurationMS <= 0 {
		return nil
	}
	v := float64(r.Calls) / (*r.DurationMS / 60000.0)
	return &v
}

// deltaAPI compares the recording under investigation (b) with a reference (a).
func (s *server) deltaAPI(w http.ResponseWriter, r *http.Request) {
	if !getOnly(w, r) {
		return
	}
	if s.dir == "" {
		apiError(w, 400, fmt.Errorf("comparison needs directory mode"))
		return
	}
	q := r.URL.Query()
	refName, curName := q.Get("a"), q.Get("b")
	if refName == "" || curName == "" {
		apiError(w, 400, fmt.Errorf("choose both a reference recording (a) and the recording to examine (b)"))
		return
	}
	if refName == curName {
		apiError(w, 400, fmt.Errorf("choose two different recordings"))
		return
	}
	refRec, err := s.recording(refName)
	if err != nil {
		apiError(w, 400, err)
		return
	}
	curRec, err := s.recording(curName)
	if err != nil {
		apiError(w, 400, err)
		return
	}
	writeJSON(w, compareBehaviour(analyseRecording(refRec), analyseRecording(curRec)))
}
