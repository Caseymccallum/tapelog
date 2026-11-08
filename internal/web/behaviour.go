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
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Caseymccallum/tapelog/internal/session"
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
	minRepeatCount = 3

	// maxFindingsPerSession keeps a pathological recording from flooding the
	// overview with every window it contains.
	maxFindingsPerSession = 20

	// hashChars is how much of a SHA-256 digest identifies an argument or
	// result body. 16 hex characters (64 bits) make an accidental collision
	// vanishingly unlikely inside one recording and keep the cache small.
	hashChars = 16
)

// callRecord is one recorded tool call reduced to what behaviour analysis
// needs. Argument and result bodies are deliberately not retained: the index is
// bounded, and the recording stays the source of the full evidence.
type callRecord struct {
	Seq        uint64   `json:"seq"`
	TS         string   `json:"ts,omitempty"`
	epochMS    int64    // parsed TS; 0 when absent or unparseable
	Tool       string   `json:"tool"`
	Server     string   `json:"server,omitempty"`
	ArgsKey    string   `json:"-"`
	ResultKey  string   `json:"-"`
	Status     string   `json:"status"`
	Verdict    string   `json:"verdict,omitempty"`
	Rule       string   `json:"rule,omitempty"`
	IsError    bool     `json:"is_error,omitempty"`
	DurationMS *float64 `json:"duration_ms"`
	Signature  string   `json:"signature,omitempty"`
	ResultSeq  uint64   `json:"result_seq,omitempty"`
}

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
	Notes   []string
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
	for _, e := range rec.Events {
		switch e.Type {
		case session.EventToolCall:
			var p session.ToolCallPayload
			if json.Unmarshal(e.Payload, &p) == nil {
				args[e.Seq] = p.Args
			}
		case session.EventToolResult:
			var p session.ToolResultPayload
			if json.Unmarshal(e.Payload, &p) == nil {
				results[e.Seq] = p.Result
			}
		}
	}
	a.Calls = make([]callRecord, 0, len(rec.Activities))
	for _, act := range rec.Activities {
		c := callRecord{
			Seq:        act.Seq,
			TS:         act.TS,
			epochMS:    epochMillis(act.TS),
			Tool:       act.Tool,
			Server:     act.Server,
			ArgsKey:    argsKey(args[act.Seq]),
			Status:     act.Status,
			Verdict:    act.Verdict,
			Rule:       act.Rule,
			DurationMS: act.DurationMS,
			ResultSeq:  act.ResultSeq,
			IsError:    act.Status == "failed",
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
	a.Repeats = detectRepeats(a.Calls)
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

var (
	quotedRun = regexp.MustCompile(`"(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*'`)
	urlRun    = regexp.MustCompile(`[a-zA-Z][a-zA-Z0-9+.-]*://[^\s"'<>]+`)
	uuidRun   = regexp.MustCompile(`\b[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}\b`)
	hexRun    = regexp.MustCompile(`\b(?:0x)?[0-9a-fA-F]{12,}\b`)
	pathRun   = regexp.MustCompile(`(?:[A-Za-z]:\\[^\s"']*|(?:^|[\s(=])/(?:[^\s"'/]+/)+[^\s"',)]*)`)
	numberRun = regexp.MustCompile(`(^|[^\w])\d+(?:\.\d+)?`)
	spaceRun  = regexp.MustCompile(`\s+`)
	// trailingDetail matches a bracketed suffix such as "(line 3)", "[errno 2]"
	// or "(attempt 4)". Real servers append this context to otherwise identical
	// messages, so leaving it in would create a new group per occurrence.
	trailingDetail = regexp.MustCompile(`\s*[\(\[](?:[^\)\]]*)[\)\]]\s*$`)
)

// normaliseMessage replaces the parts of an error message that change between
// occurrences with stable placeholders, then keeps the opening words. The
// placeholder names appear in the group label, so a reader can see exactly what
// was collapsed.
func normaliseMessage(message string) string {
	s := strings.TrimSpace(message)
	if s == "" {
		return ""
	}
	s = urlRun.ReplaceAllString(s, "<url>")
	s = uuidRun.ReplaceAllString(s, "<id>")
	s = pathRun.ReplaceAllString(s, "<path>")
	s = quotedRun.ReplaceAllString(s, "<text>")
	s = hexRun.ReplaceAllString(s, "<id>")
	s = numberRun.ReplaceAllString(s, "${1}<n>")
	s = strings.TrimSpace(spaceRun.ReplaceAllString(s, " "))
	for i := 0; i < 3; i++ {
		trimmed := strings.TrimSpace(trailingDetail.ReplaceAllString(s, ""))
		if trimmed == s || trimmed == "" {
			break
		}
		s = trimmed
	}
	words := strings.Fields(s)
	// Eight words identify a problem; a longer explanation with a variable tail
	// would otherwise become its own group every time the tail changed.
	if len(words) > 8 {
		words = words[:8]
	}
	return strings.Join(words, " ")
}

// signatureFragment returns a literal, value-free substring of the recorded
// message that a `result_never_contains` assertion can look for.
//
// The engine does a literal substring match, so the fragment has to appear in
// the recording exactly as written. Volatile spans — numbers, identifiers,
// paths, URLs and quoted values — are excluded, both because they would make
// the assertion unstable and because copying a recorded value into a test file
// is not something this tool should do. Among the remaining literal runs the
// longest is chosen (earliest wins a tie), which recovers a useful phrase from
// messages like `relation "users" does not exist`.
func signatureFragment(message string) string {
	s := strings.TrimSpace(spaceRun.ReplaceAllString(message, " "))
	if s == "" {
		return ""
	}
	type span struct{ a, b int }
	var spans []span
	for _, re := range []*regexp.Regexp{urlRun, uuidRun, pathRun, quotedRun, hexRun, numberRun} {
		for _, loc := range re.FindAllStringIndex(s, -1) {
			spans = append(spans, span{loc[0], loc[1]})
		}
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].a < spans[j].a })
	best := ""
	prev := 0
	for _, sp := range append(spans, span{len(s), len(s)}) {
		if sp.a > prev {
			cand := strings.Trim(strings.TrimSpace(s[prev:sp.a]), ":;,.<>=-([{\"'")
			// A shorter fragment would match unrelated messages, which makes the
			// generated assertion a false positive waiting to happen.
			if len(cand) >= 12 && len(cand) > len(best) {
				best = cand
			}
		}
		if sp.b > prev {
			prev = sp.b
		}
	}
	return shortText(best, 80)
}

// --- repeated activity detection -----------------------------------------

// Repeat findings use a fixed vocabulary so the interface never has to invent a
// category. Only the last two are interpretations; the rest are observations.
const (
	kindPossibleRetryLoop = "possible_retry_loop"
	kindRepeatedFailure   = "repeated_failure"
	kindRepeatedDenial    = "repeated_denial"
	kindRepeatedActivity  = "repeated_activity"
	kindAlternating       = "alternating_sequence"
	kindRepeatingBlock    = "repeating_block"
	kindInsufficient      = "insufficient_evidence"
)

// repeatFinding is one deterministic observation about repetition inside a
// single recording. It always carries the calls it was derived from, the
// evidence that produced it, and an interpretation that states what the
// recording cannot tell you.
type repeatFinding struct {
	Kind             string   `json:"kind"`
	Tool             string   `json:"tool,omitempty"`
	Tools            []string `json:"tools,omitempty"`
	Count            int      `json:"count"`
	Consecutive      int      `json:"consecutive,omitempty"`
	Repeats          int      `json:"repeats,omitempty"`
	BlockLength      int      `json:"block_length,omitempty"`
	Seqs             []uint64 `json:"seqs"`
	FirstSeq         uint64   `json:"first_seq"`
	LastSeq          uint64   `json:"last_seq"`
	FirstIndex       int      `json:"first_index"`
	IdenticalResults bool     `json:"identical_results,omitempty"`
	MedianGapMS      *float64 `json:"median_gap_ms"`
	Evidence         string   `json:"evidence"`
	Interpretation   string   `json:"interpretation"`
	Confidence       string   `json:"confidence"`
	Signature        string   `json:"signature,omitempty"`
}

// detectRepeats runs the deterministic detectors over one recording's calls.
//
// Thresholds: three occurrences of the same argument set, or three repetitions
// of the same short block. Two identical failing calls are reported, but only
// as insufficient evidence, because a single retry is ordinary behaviour.
func detectRepeats(calls []callRecord) []repeatFinding {
	if len(calls) < 2 {
		return nil
	}
	var out []repeatFinding
	blockFindings, covered := detectBlocks(calls)
	out = append(out, blockFindings...)

	type key struct{ tool, args string }
	groups := map[key][]int{}
	var order []key
	for i, c := range calls {
		if c.ArgsKey == "" {
			continue // arguments were unreadable; grouping on nothing would over-merge
		}
		k := key{c.Tool, c.ArgsKey}
		if _, seen := groups[k]; !seen {
			order = append(order, k)
		}
		groups[k] = append(groups[k], i)
	}
	for _, k := range order {
		idx := groups[k]
		if len(idx) < 2 || allCovered(idx, covered) {
			continue
		}
		if f := classifyGroup(calls, idx, k.args); f != nil {
			out = append(out, *f)
		}
	}
	sortFindings(out)
	if len(out) > maxFindingsPerSession {
		out = out[:maxFindingsPerSession]
	}
	return out
}

func allCovered(idx []int, covered map[int]bool) bool {
	for _, i := range idx {
		if !covered[i] {
			return false
		}
	}
	return true
}

// classifyGroup turns one group of identical-argument calls into a finding.
// The digest is included so a reader can confirm two calls really share
// arguments without the workbench printing argument values.
func classifyGroup(calls []callRecord, idx []int, digest string) *repeatFinding {
	var errored, denied, succeeded, unanswered int
	resultKeys := map[string]bool{}
	for _, i := range idx {
		c := calls[i]
		switch {
		case c.ResultKey != "" && c.IsError:
			errored++
		case c.ResultKey != "":
			succeeded++
		case c.Verdict == "deny":
			denied++
		default:
			unanswered++
		}
		if c.ResultKey != "" {
			resultKeys[c.ResultKey] = true
		}
	}
	seqs := make([]uint64, 0, len(idx))
	for _, i := range idx {
		seqs = append(seqs, calls[i].Seq)
	}
	n := len(idx)
	f := &repeatFinding{
		Tool:             calls[idx[0]].Tool,
		Count:            n,
		Consecutive:      longestRun(idx),
		Seqs:             seqs,
		FirstSeq:         seqs[0],
		LastSeq:          seqs[len(seqs)-1],
		FirstIndex:       idx[0],
		MedianGapMS:      medianGap(calls, idx),
		Signature:        calls[idx[0]].Signature,
		IdenticalResults: len(resultKeys) == 1,
	}
	identity := fmt.Sprintf("argument digest %s", shortDigest(digest))
	switch {
	case denied == n && n >= minRepeatCount:
		f.Kind = kindRepeatedDenial
		f.Confidence = "observed"
		f.Evidence = fmt.Sprintf("%d calls with the same tool and %s were all denied by the boundary", n, identity)
		f.Interpretation = "The boundary denied every one of these identical attempts. The recording shows the denials, not whether the agent understood them: a repeated denial may be a probe, a retry, or an agent that discarded the error."
	case errored == n && n >= minRepeatCount:
		f.Kind = kindRepeatedFailure
		f.Confidence = "observed"
		f.Evidence = fmt.Sprintf("%d calls with %s all returned recorded errors", n, identity)
		if f.IdenticalResults {
			f.Kind = kindPossibleRetryLoop
			f.Confidence = "possible"
			f.Evidence += "; every error result is byte-identical"
			f.Interpretation = "Possible retry loop: identical arguments produced byte-identical error results each time, so no attempt after the first could have made progress. This is an interpretation drawn from the recording — the agent's reasoning is not recorded — and a deliberate bounded retry can look the same."
		} else {
			f.Interpretation = "The same call failed repeatedly with differing error detail. Repeated failure is observed behaviour, not proof that the agent was stuck: a bounded retry against a flaky server produces the same trace."
		}
	case errored >= 2:
		f.Kind = kindRepeatedFailure
		f.Confidence = "observed"
		f.Evidence = fmt.Sprintf("%d calls with %s produced %d errors, %d recorded results and %d denials", n, identity, errored, succeeded, denied)
		f.Interpretation = "The same call repeatedly failed alongside other outcomes. Read this as a prompt to check the evidence, not as a diagnosis: Tapelog records outcomes, not the decisions behind them."
	case succeeded == n && n >= minRepeatCount:
		f.Kind = kindRepeatedActivity
		f.Confidence = "observed"
		f.Evidence = fmt.Sprintf("%d calls with %s each recorded a result", n, identity)
		if f.Consecutive >= minRepeatCount {
			f.Evidence += fmt.Sprintf("; %d of them ran back to back", f.Consecutive)
		}
		f.Interpretation = "Repeated activity observed. Tapelog cannot tell whether it was necessary: polling, pagination and read-after-write loops call the same tool with the same arguments by design. Whether this repetition is worth attention is a judgement about your tool, not a finding."
	case unanswered == n:
		f.Kind = kindInsufficient
		f.Confidence = "insufficient"
		f.Evidence = fmt.Sprintf("%d calls with %s have no recorded result and no recorded decision", n, identity)
		f.Interpretation = "Insufficient evidence: these calls were never answered, so the recording cannot show whether they made progress."
	case n < minRepeatCount:
		// Two identical calls are reported, but only as a statement about the
		// evidence: a single retry, a confirmation prompt or a poll pair is
		// ordinary behaviour and cannot support a pattern claim.
		f.Kind = kindInsufficient
		f.Confidence = "insufficient"
		f.Evidence = fmt.Sprintf("%d calls with %s produced %d errors, %d recorded results and %d denials", n, identity, errored, succeeded, denied)
		f.Interpretation = fmt.Sprintf("Insufficient evidence: %d identical calls are below the %d-occurrence threshold Tapelog uses to report a repeating pattern, and a single retry is ordinary behaviour.", n, minRepeatCount)
	default:
		return nil
	}
	return f
}

// shortDigest abbreviates a stored argument digest for display.
func shortDigest(digest string) string {
	if len(digest) > 8 {
		return digest[:8]
	}
	return digest
}

// longestRun reports the longest run of consecutive array positions.
func longestRun(idx []int) int {
	best, run := 1, 1
	for i := 1; i < len(idx); i++ {
		if idx[i] == idx[i-1]+1 {
			run++
		} else {
			run = 1
		}
		if run > best {
			best = run
		}
	}
	return best
}

// medianGap reports the middle inter-call interval when timestamps exist. It is
// the only way to separate a rapid burst from slow polling, so it is reported as
// data rather than turned into a conclusion.
func medianGap(calls []callRecord, idx []int) *float64 {
	var gaps []int64
	var prev int64
	for _, i := range idx {
		ts := calls[i].epochMS
		if ts == 0 {
			continue
		}
		if prev != 0 && ts > prev {
			gaps = append(gaps, ts-prev)
		}
		prev = ts
	}
	if len(gaps) == 0 {
		return nil
	}
	sort.Slice(gaps, func(i, j int) bool { return gaps[i] < gaps[j] })
	mid := float64(gaps[len(gaps)/2])
	return &mid
}

// detectBlocks finds short call sequences that repeat, in order, at least three
// times. It scans left to right and consumes each match, so one long repeated
// run produces one finding rather than one per starting offset. Block length 1
// is excluded on purpose: single-call repeats belong to the group detector and
// would otherwise be reported twice.
func detectBlocks(calls []callRecord) ([]repeatFinding, map[int]bool) {
	covered := map[int]bool{}
	var out []repeatFinding
	same := func(a, b int) bool {
		return calls[a].Tool == calls[b].Tool && calls[a].ArgsKey != "" && calls[a].ArgsKey == calls[b].ArgsKey
	}
	for i := 0; i < len(calls); i++ {
		matched := false
		for k := 2; k <= 4 && !matched; k++ {
			if i+2*k > len(calls) {
				continue
			}
			repeats := 1
			for {
				next := i + repeats*k
				if next+k > len(calls) {
					break
				}
				ok := true
				for j := 0; j < k; j++ {
					if !same(i+j, next+j) {
						ok = false
						break
					}
				}
				if !ok {
					break
				}
				repeats++
			}
			if repeats < minRepeatCount {
				continue
			}
			// A block of identical calls is a single-call run, which the group
			// detector owns. Requiring two distinct calls keeps a polling tool
			// from being reported as an "alternating sequence".
			distinct := map[string]bool{}
			for j := 0; j < k; j++ {
				distinct[calls[i+j].Tool+"\x00"+calls[i+j].ArgsKey] = true
			}
			if len(distinct) < 2 {
				continue
			}
			end := i + repeats*k
			f := repeatFinding{Count: end - i, Repeats: repeats, BlockLength: k, Seqs: make([]uint64, 0, end-i)}
			seen := map[string]bool{}
			for j := i; j < end; j++ {
				f.Seqs = append(f.Seqs, calls[j].Seq)
				if !seen[calls[j].Tool] {
					seen[calls[j].Tool] = true
					f.Tools = append(f.Tools, calls[j].Tool)
				}
				covered[j] = true
			}
			f.FirstSeq, f.LastSeq = f.Seqs[0], f.Seqs[len(f.Seqs)-1]
			f.FirstIndex = i
			f.Confidence = "observed"
			f.Tool = strings.Join(f.Tools, " → ")
			if k == 2 {
				f.Kind = kindAlternating
				f.Evidence = fmt.Sprintf("two calls alternated %d times in the same order with identical arguments", repeats)
				f.Interpretation = "An alternating sequence was observed. Some agents iterate over a fixed list of two operations; read the arguments in the timeline before treating this as a loop."
			} else {
				f.Kind = kindRepeatingBlock
				f.Evidence = fmt.Sprintf("a block of %d calls repeated %d times in the same order with identical arguments", k, repeats)
				f.Interpretation = "A repeating sequence was observed. It may be legitimate batch work over a fixed list; Tapelog reports the shape of the repetition, not its purpose."
			}
			out = append(out, f)
			i = end - 1
			matched = true
		}
	}
	return out, covered
}

// sortFindings orders observations by how much attention they deserve, then by
// size and recording order, so the list is deterministic.
func sortFindings(f []repeatFinding) {
	rank := map[string]int{
		kindPossibleRetryLoop: 0, kindRepeatedFailure: 1, kindRepeatedDenial: 2,
		kindAlternating: 3, kindRepeatingBlock: 3, kindRepeatedActivity: 4, kindInsufficient: 5,
	}
	sort.SliceStable(f, func(i, j int) bool {
		if rank[f[i].Kind] != rank[f[j].Kind] {
			return rank[f[i].Kind] < rank[f[j].Kind]
		}
		if f[i].Count != f[j].Count {
			return f[i].Count > f[j].Count
		}
		return f[i].FirstSeq < f[j].FirstSeq
	})
}

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

// get returns a cached analysis only while the file still looks identical.
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
		analysis := cache.get(file.name, file.size, file.modified)
		if analysis != nil {
			idx.Cached++
		} else {
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
	Calls            int `json:"calls"`
	Results          int `json:"results"`
	Successes        int `json:"successes"`
	Failures         int `json:"failures"`
	Denies           int `json:"denies"`
	DenyDecisions    int `json:"deny_decisions"`
	Confirms         int `json:"confirms"`
	ConfirmDecisions int `json:"confirm_decisions"`
	Unanswered       int `json:"unanswered"`
	Discovery        int `json:"discovery"`
	Events           int `json:"events"`
	RepeatFindings   int `json:"repeat_findings"`
	RepeatAttention  int `json:"repeat_attention"`
	SignatureGroups  int `json:"signature_groups"`
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
	Kind        string      `json:"kind"`
	Tool        string      `json:"tool,omitempty"`
	Tools       []string    `json:"tools,omitempty"`
	Findings    int         `json:"findings"`
	Occurrences int         `json:"occurrences"`
	Sessions    int         `json:"sessions"`
	MaxCount    int         `json:"max_count"`
	Files       []string    `json:"files"`
	Sample      *occurrence `json:"sample,omitempty"`
}

// sessionRow is one recording's behaviour, used for the trend table and the
// reference comparison. Rates are nullable so a reader can see when a
// percentage is undefined rather than zero.
type sessionRow struct {
	File            string   `json:"file"`
	Name            string   `json:"name"`
	Harness         string   `json:"harness,omitempty"`
	Servers         []string `json:"servers"`
	FirstTS         string   `json:"first_ts,omitempty"`
	LastTS          string   `json:"last_ts,omitempty"`
	Calls           int      `json:"calls"`
	Results         int      `json:"results"`
	Failures        int      `json:"failures"`
	Denies          int      `json:"denies"`
	Unanswered      int      `json:"unanswered"`
	RepeatFindings  int      `json:"repeat_findings"`
	RepeatAttention int      `json:"repeat_attention"`
	ErrorRate       *float64 `json:"error_rate"`
	DenialRate      *float64 `json:"denial_rate"`
	DurationMS      *float64 `json:"duration_ms"`
	Complete        bool     `json:"complete"`
	Integrity       string   `json:"integrity"`
	ChainOK         bool     `json:"chain_ok"`
}

// behaviourOverview is the whole cross-session answer in one response.
type behaviourOverview struct {
	Window      behaviourWindow  `json:"window"`
	Library     behaviourLibrary `json:"library"`
	Counts      behaviourCounts  `json:"counts"`
	Tools       []toolStat       `json:"tools"`
	Denied      []deniedStat     `json:"denied"`
	Failures    []signatureStat  `json:"failures"`
	Repeats     []repeatStat     `json:"repeats"`
	Trend       []sessionRow     `json:"trend"`
	Recent      []sessionRow     `json:"recent"`
	Definitions []definition     `json:"definitions"`
	Notes       []string         `json:"notes"`
	Scope       string           `json:"scope"`
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
	for _, a := range idx.Sessions {
		row := rowFor(a)
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
				r = &repeatStat{Kind: f.Kind, Tool: f.Tool, Tools: f.Tools, Files: []string{}}
				repeats[bucket] = r
			}
			r.Findings++
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
		ov.Counts.DenyDecisions += a.Info.Denies
		ov.Counts.ConfirmDecisions += a.Info.Confirms
		ov.Counts.Discovery += a.Info.Discovery
		ov.Counts.Events += a.Info.Events
		ov.Trend = append(ov.Trend, row)
	}

	ov.Counts.Successes = ov.Counts.Results - ov.Counts.Failures
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
		RepeatFindings: len(a.Repeats),
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
		notes = append(notes, fmt.Sprintf("%d calls were never answered. Rates are calculated over recorded results only, so they do not absorb those calls.", ov.Counts.Unanswered))
	}
	if ov.Counts.Results == 0 && ov.Counts.Calls > 0 {
		notes = append(notes, "No results were recorded, so no error rate can be calculated. A percentage would be undefined here, not zero.")
	}
	for _, a := range idx.Sessions {
		if a.Info.Integrity == "invalid" {
			notes = append(notes, "At least one analysed recording failed its hash-chain check. Its activity is included, but treat it as untrusted evidence.")
			break
		}
		if !a.Info.Complete {
			notes = append(notes, "At least one analysed recording has no session/end event. A missing end can mean an interruption, a live session or a failed startup; it does not establish a failure.")
			break
		}
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
	Metric     string   `json:"metric"`
	Unit       string   `json:"unit"`
	Reference  *float64 `json:"reference"`
	Current    *float64 `json:"current"`
	ReferenceN int      `json:"reference_n"`
	CurrentN   int      `json:"current_n"`
	Note       string   `json:"note,omitempty"`
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
			d = &deniedStat{Tool: c.Tool, Rule: c.Rule}
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
			t = &repeatTally{tools: f.Tools}
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
				Kind: kind, Tool: tool, Tools: v.tools,
				Current: v.findings, CurrentOccurrences: v.occurrences,
			})
		}
	}
	for k, v := range refRep {
		if curRep[k] == nil {
			kind, tool := split(k)
			d.ResolvedRepeats = append(d.ResolvedRepeats, repeatDelta{
				Kind: kind, Tool: tool, Tools: v.tools,
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
		{Metric: "Calls per minute", Unit: "per_minute", Reference: perMinute(d.Reference), Current: perMinute(d.Current), ReferenceN: d.Reference.Calls, CurrentN: d.Current.Calls, Note: "Uses the recorded session duration; unequal durations make this a rough comparison."},
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
