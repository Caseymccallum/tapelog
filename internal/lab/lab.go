// Package lab runs explicitly defined regression expectations against verified
// recorded evidence. It never launches a server or re-executes recorded calls.
package lab

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Caseymccallum/tapelog/internal/replay"
	"github.com/Caseymccallum/tapelog/internal/scenario"
	"github.com/Caseymccallum/tapelog/internal/session"
)

const MaxRecording = 16 << 20
const MaxDefinition = 128 << 10
const MaxChecks = 100
const MaxCases = 100

type Expectation struct {
	Kind     string   `json:"kind"`
	Tool     string   `json:"tool,omitempty"`
	Text     string   `json:"text,omitempty"`
	Sequence []string `json:"sequence,omitempty"`
	Maximum  *int     `json:"maximum,omitempty"`
}
type Case struct {
	Version        int           `json:"version"`
	Title          string        `json:"title"`
	Description    string        `json:"description,omitempty"`
	Evidence       string        `json:"evidence"`
	EvidenceSHA256 string        `json:"evidence_sha256,omitempty"`
	Reference      string        `json:"reference,omitempty"`
	EvidenceSeqs   []uint64      `json:"evidence_seqs,omitempty"`
	Expectations   []Expectation `json:"expectations"`
}
type Suite struct {
	Version int      `json:"version"`
	Title   string   `json:"title"`
	Cases   []string `json:"cases"`
}
type CheckResult struct {
	Kind   string `json:"kind"`
	Status string `json:"status"`
	Detail string `json:"detail"`
	// Code names why evidence could not support the expectation, so the
	// interface can group inconclusive outcomes instead of printing prose.
	Code string `json:"code,omitempty"`
}
type Result struct {
	Version  int           `json:"version"`
	Title    string        `json:"title"`
	Evidence string        `json:"evidence"`
	Revision string        `json:"revision,omitempty"`
	Status   string        `json:"status"`
	Checks   []CheckResult `json:"checks"`
	Note     string        `json:"note"`
}
type SuiteResult struct {
	Version      int      `json:"version"`
	Title        string   `json:"title"`
	Status       string   `json:"status"`
	Pass         int      `json:"pass"`
	Fail         int      `json:"fail"`
	Inconclusive int      `json:"inconclusive"`
	// Invalid counts suite entries whose case definition could not be loaded.
	// They are also counted in Inconclusive, because a broken case never proves
	// the expectation and must never read as a pass.
	Invalid int      `json:"invalid"`
	Cases   []Result `json:"cases"`
}

// Inconclusive codes. A code is a stable, machine-readable reason: callers group
// and count them, and prose details stay free to improve.
const (
	CodeNoCalls         = "no_calls"
	CodeNoResults       = "no_results"
	CodeRedacted        = "redacted_content"
	CodeMissingDecision = "missing_decision"
	CodeNoDenials       = "no_denials"
	CodeShortSequence   = "short_sequence"
	CodeIncomplete      = "incomplete_recording"
	CodeDamaged         = "damaged_recording"
	CodeRevision        = "revision_changed"
	CodeUnreadable      = "evidence_unreadable"
	CodeInvalidCase     = "invalid_case"
)

// EvidenceError says a recording cannot support an evaluation, and why in a way
// the interface can group. It is distinct from a definition error: the case may
// be perfectly well formed while the evidence is not.
type EvidenceError struct {
	Code   string
	Reason string
}

func (e *EvidenceError) Error() string { return e.Reason }

func fail(code, reason string) error { return &EvidenceError{Code: code, Reason: reason} }

func failf(code, format string, args ...any) error {
	return &EvidenceError{Code: code, Reason: fmt.Sprintf(format, args...)}
}

// evidenceCode extracts the code from an inspect failure, defaulting to the
// damaged-recording code for errors this package did not classify.
func evidenceCode(err error) string {
	var e *EvidenceError
	if errors.As(err, &e) {
		return e.Code
	}
	return CodeDamaged
}

func leaf(s, ext string) bool {
	return s != "" && s == strings.TrimSpace(s) && s == filepath.Base(s) && !strings.ContainsAny(s, "/\\:\x00\r\n") && strings.HasSuffix(strings.ToLower(s), ext)
}
func literal(s string) bool { return s != "" && len(s) <= 512 && !strings.ContainsAny(s, "*?\x00\r\n") }
func (c Case) Validate() error {
	if c.Version != 1 || strings.TrimSpace(c.Title) == "" || len(c.Title) > 200 || len(c.Description) > 2000 {
		return fmt.Errorf("case needs version 1 and a title (maximum 200 characters)")
	}
	if !leaf(c.Evidence, ".jsonl") || c.Reference != "" && !leaf(c.Reference, ".jsonl") {
		return fmt.Errorf("evidence and reference must be plain recording filenames")
	}
	if c.EvidenceSHA256 != "" {
		v, e := hex.DecodeString(c.EvidenceSHA256)
		if e != nil || len(v) != 32 {
			return fmt.Errorf("evidence_sha256 must be a SHA-256 digest")
		}
	}
	if len(c.EvidenceSeqs) > 10 {
		return fmt.Errorf("at most 10 evidence events")
	}
	seenSeq := map[uint64]bool{}
	for _, n := range c.EvidenceSeqs {
		if n == 0 || seenSeq[n] {
			return fmt.Errorf("evidence event identifiers must be unique and nonzero")
		}
		seenSeq[n] = true
	}
	if len(c.Expectations) == 0 || len(c.Expectations) > MaxChecks {
		return fmt.Errorf("case needs 1 to %d expectations", MaxChecks)
	}
	for _, x := range c.Expectations {
		switch x.Kind {
		case "answered", "denied", "attempted", "no_attempts":
			if !literal(x.Tool) || x.Text != "" || len(x.Sequence) > 0 || x.Maximum != nil {
				return fmt.Errorf("%s needs one literal tool name", x.Kind)
			}
		case "attempt_ceiling":
			if !literal(x.Tool) || x.Maximum == nil || *x.Maximum < 0 || *x.Maximum > 100000 || x.Text != "" || len(x.Sequence) > 0 {
				return fmt.Errorf("attempt_ceiling needs a literal tool and nonnegative maximum")
			}
		case "result_contains", "result_never_contains":
			if !literal(x.Tool) || strings.TrimSpace(x.Text) == "" || len(x.Text) > 512 || len(x.Sequence) > 0 || x.Maximum != nil {
				return fmt.Errorf("%s needs a literal tool and explicit text", x.Kind)
			}
		case "sequence":
			if len(x.Sequence) < 2 || len(x.Sequence) > 10 || x.Tool != "" || x.Text != "" || x.Maximum != nil {
				return fmt.Errorf("sequence needs 2 to 10 literal tools")
			}
			for _, n := range x.Sequence {
				if !literal(n) {
					return fmt.Errorf("sequence needs literal tool names")
				}
			}
		case "no_deny_bypassed":
			if x.Tool != "" || x.Text != "" || len(x.Sequence) > 0 || x.Maximum != nil {
				return fmt.Errorf("invariant takes no parameters")
			}
		default:
			return fmt.Errorf("unsupported expectation %q", x.Kind)
		}
	}
	return nil
}
func parseStrict(data []byte, v any) error {
	if len(data) > MaxDefinition {
		return fmt.Errorf("definition exceeds 128 KiB")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("expected one JSON document")
	}
	return nil
}
func ParseCase(data []byte) (Case, error) {
	var c Case
	if err := parseStrict(data, &c); err != nil {
		return c, err
	}
	return c, c.Validate()
}
func ParseSuite(data []byte) (Suite, error) {
	var s Suite
	if err := parseStrict(data, &s); err != nil {
		return s, err
	}
	if s.Version != 1 || strings.TrimSpace(s.Title) == "" || len(s.Title) > 200 || len(s.Cases) == 0 || len(s.Cases) > MaxCases {
		return s, fmt.Errorf("suite needs version 1, title and 1 to %d cases", MaxCases)
	}
	seen := map[string]bool{}
	for _, n := range s.Cases {
		if !leaf(n, ".json") || seen[n] {
			return s, fmt.Errorf("suite cases must be distinct plain .json filenames")
		}
		seen[n] = true
	}
	return s, nil
}

// ReadLeaf protects access with os.Root and refuses symlinks/nonregular files.
func ReadLeaf(dir, name, ext string, limit int64) ([]byte, error) {
	if !leaf(name, ext) {
		return nil, fmt.Errorf("invalid %s filename", ext)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	st, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() || st.Size() > limit {
		return nil, fmt.Errorf("file is not regular or exceeds the size limit")
	}
	f, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(st, opened) {
		return nil, fmt.Errorf("file changed while opening")
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("file grew beyond the size limit")
	}
	return data, nil
}
func digest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

// inspect rejects incomplete or ambiguous correlations before replay's
// ID-based loader can pair events. An intact chain alone is not authorship.
func inspect(data []byte) (*replay.Tape, error) {
	v, err := session.Verify(bytes.NewReader(data))
	if err != nil || v == nil || !v.OK() || v.Events == 0 {
		return nil, fail(CodeDamaged, "the recording chain is invalid or empty")
	}
	if v.Events > 10000 {
		return nil, fail(CodeDamaged, "the recording exceeds the 10,000-event lab limit")
	}
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 65536), MaxRecording+1)
	ids := map[string]uint64{}
	seqIDs := map[uint64]string{}
	results := map[string]bool{}
	var first, last session.EventType
	count := 0
	starts, ends := 0, 0
	sessionID := ""
	for sc.Scan() {
		if len(bytes.TrimSpace(sc.Bytes())) == 0 {
			continue
		}
		var e session.Event
		if json.Unmarshal(sc.Bytes(), &e) != nil {
			return nil, fail(CodeDamaged, "the recording contains an unreadable event")
		}
		if count == 0 {
			first = e.Type
		}
		last = e.Type
		if e.Type == session.EventSessionStart {
			starts++
		}
		if e.Type == session.EventSessionEnd {
			ends++
		}
		if e.SessionID == "" {
			return nil, fail(CodeDamaged, "the recording has no session identity")
		}
		if sessionID == "" {
			sessionID = e.SessionID
		} else if sessionID != e.SessionID {
			return nil, fail(CodeDamaged, "the recording mixes more than one session identity")
		}
		count++
		switch e.Type {
		case session.EventToolCall:
			var p session.ToolCallPayload
			if json.Unmarshal(e.Payload, &p) != nil || len(p.ID) == 0 || string(p.ID) == "null" || p.Tool == "" {
				return nil, fail(CodeDamaged, "a recorded call has no usable identity")
			}
			key := string(p.ID)
			if _, exists := ids[key]; exists {
				return nil, fail(CodeDamaged, "a call identity repeats, so calls cannot be paired unambiguously")
			}
			ids[key] = e.Seq
			seqIDs[e.Seq] = key
		case session.EventPolicyDecision, session.EventToolResult:
			var p struct {
				ID     json.RawMessage `json:"id"`
				Parent *uint64         `json:"parent_seq"`
			}
			if json.Unmarshal(e.Payload, &p) != nil || len(p.ID) == 0 {
				return nil, fail(CodeDamaged, "a decision or result has no usable call identity")
			}
			key := string(p.ID)
			seq, ok := ids[key]
			if !ok {
				return nil, fail(CodeDamaged, "a decision or result has no preceding call")
			}
			if p.Parent != nil && (seq != *p.Parent || seqIDs[*p.Parent] != key) {
				return nil, fail(CodeDamaged, "a parent event and call identity disagree")
			}
			if e.Type == session.EventPolicyDecision {
				var d session.PolicyDecisionPayload
				if json.Unmarshal(e.Payload, &d) != nil || (d.Verdict != "allow" && d.Verdict != "deny" && d.Verdict != "confirm") {
					return nil, fail(CodeDamaged, "a policy decision has an unsupported verdict")
				}
			}
			if e.Type == session.EventToolResult {
				var result session.ToolResultPayload
				if json.Unmarshal(e.Payload, &result) != nil || len(result.Result) == 0 {
					return nil, fail(CodeDamaged, "a recorded result is empty or malformed")
				}
				if results[key] {
					return nil, fail(CodeDamaged, "a call has more than one recorded result")
				}
				results[key] = true
			}
		}
	}
	if sc.Err() != nil {
		return nil, fail(CodeDamaged, "the recording cannot be read")
	}
	if starts != 1 || ends != 1 || first != session.EventSessionStart || last != session.EventSessionEnd {
		return nil, fail(CodeIncomplete, "the recording has no complete session/start and session/end")
	}
	tape, err := replay.LoadInline(bytes.NewReader(data))
	if err != nil {
		return nil, failf(CodeDamaged, "the recording cannot be safely loaded: %v", err)
	}
	return tape, nil
}
func testCheck(x Expectation) scenario.Check {
	switch x.Kind {
	case "answered":
		return scenario.Check{Called: &scenario.CalledSpec{Tool: x.Tool}}
	case "denied":
		return scenario.Check{Denied: x.Tool}
	case "attempted":
		return scenario.Check{Attempted: &scenario.AttemptSpec{Tool: x.Tool}}
	case "no_attempts":
		n := 0
		return scenario.Check{Attempted: &scenario.AttemptSpec{Tool: x.Tool, Times: &n}}
	case "attempt_ceiling":
		return scenario.Check{Attempted: &scenario.AttemptSpec{Tool: x.Tool, Max: x.Maximum}}
	case "result_contains":
		return scenario.Check{ResultContains: &scenario.ResultSpec{Tool: x.Tool, Text: x.Text}}
	case "result_never_contains":
		return scenario.Check{ResultNeverContains: &scenario.ResultSpec{Tool: x.Tool, Text: x.Text}}
	case "sequence":
		return scenario.Check{Sequence: x.Sequence}
	case "no_deny_bypassed":
		return scenario.Check{Invariant: "no_deny_bypassed"}
	}
	return scenario.Check{}
}
// eligibility decides whether the recorded evidence is capable of evaluating one
// expectation. It returns an empty code when the scenario engine may decide PASS
// or FAIL; otherwise it names the missing evidence.
//
// The rule differs by claim shape, and that distinction is the whole point:
//
//   - existential claims ("a call was answered", "a result contains X") are
//     satisfied by one eligible witness; other calls that were denied or never
//     answered cannot weaken that witness.
//   - universal claims ("no result contains X") need every matching result, so a
//     single unanswered call or a redacted body makes them inconclusive.
//   - counting claims ("at most N attempts") only need the boundary to have been
//     observed.
func eligibility(x Expectation, t *replay.Tape) (code, reason string) {
	// The boundary invariant is tool-independent: it must see every call, so it
	// is decided before any tool filtering.
	if x.Kind == "no_deny_bypassed" {
		all := append(append([]replay.Interaction{}, t.Interactions...), t.Unanswered...)
		denyVerdicts, missingDecision := 0, 0
		for _, it := range all {
			if it.Verdict == "" {
				missingDecision++
			}
			if it.Verdict == "deny" {
				denyVerdicts++
			}
		}
		switch {
		case len(all) == 0:
			return CodeNoCalls, "no call reached the boundary"
		case missingDecision > 0:
			return CodeMissingDecision, "a recorded call has no policy decision"
		case denyVerdicts == 0:
			return CodeNoDenials, "no deny decision was recorded, so the invariant cannot be demonstrated"
		}
		return "", ""
	}
	// A sequence names several tools, so it is also decided before tool filtering.
	if x.Kind == "sequence" {
		if len(t.Interactions) < len(x.Sequence) {
			return CodeShortSequence, fmt.Sprintf("only %d recorded result(s); the sequence needs %d", len(t.Interactions), len(x.Sequence))
		}
		return "", ""
	}
	var answered []replay.Interaction
	calls, unanswered, missingDecision := 0, 0, 0
	for _, it := range t.Interactions {
		if it.Tool != x.Tool {
			continue
		}
		calls++
		answered = append(answered, it)
	}
	for _, it := range t.Unanswered {
		if it.Tool != x.Tool {
			continue
		}
		calls++
		unanswered++
		if it.Verdict == "" {
			missingDecision++
		}
	}
	redacted := func(it replay.Interaction) bool {
		return strings.Contains(string(it.Result), session.Redacted)
	}
	switch x.Kind {
	case "attempted", "no_attempts", "attempt_ceiling":
		if calls == 0 {
			return CodeNoCalls, "no call to " + x.Tool + " reached the boundary"
		}
		return "", ""
	case "answered":
		if len(answered) > 0 {
			return "", ""
		}
		if calls > 0 {
			return CodeNoResults, "every recorded call to " + x.Tool + " has no recorded result"
		}
		return CodeNoCalls, "no call to " + x.Tool + " reached the boundary"
	case "denied":
		if calls == 0 {
			return CodeNoCalls, "no call to " + x.Tool + " reached the boundary"
		}
		if missingDecision > 0 {
			return CodeMissingDecision, "a call to " + x.Tool + " has no recorded policy decision"
		}
		return "", ""
	case "result_contains":
		if len(answered) == 0 {
			return CodeNoResults, "no recorded result from " + x.Tool + " can be searched"
		}
		for _, it := range answered {
			if strings.Contains(string(it.Result), x.Text) {
				return "", "" // one witness is enough, whatever else happened
			}
		}
		for _, it := range answered {
			if redacted(it) {
				return CodeRedacted, "a recorded result from " + x.Tool + " has redacted content, so the text may be hidden"
			}
		}
		return "", "" // eligible results, none contains the text: a real failure
	case "result_never_contains":
		if len(answered) == 0 {
			return CodeNoResults, "no recorded result from " + x.Tool + " can prove the text is absent"
		}
		if unanswered > 0 {
			return CodeNoResults, "a call to " + x.Tool + " has no recorded result, so absence cannot be proven"
		}
		for _, it := range answered {
			if redacted(it) {
				return CodeRedacted, "a recorded result from " + x.Tool + " has redacted content, so absence cannot be proven"
			}
		}
		return "", ""
	case "sequence":
		if len(answered) < len(x.Sequence) {
			return CodeShortSequence, fmt.Sprintf("only %d recorded result(s); the sequence needs %d", len(answered), len(x.Sequence))
		}
		return "", ""
	}
	return CodeNoCalls, "this expectation has no eligibility rule"
}

func Evaluate(c Case, data []byte, candidate string) Result {
	name := c.Evidence
	if candidate != "" {
		name = candidate
	}
	r := Result{Version: 1, Title: c.Title, Evidence: name, Revision: digest(data), Status: "PASS", Checks: []CheckResult{}, Note: "Observed MCP boundary only; verified chain is not proof of authorship or actions outside the recording."}
	tape, err := inspect(data)
	if err == nil && candidate == "" && c.EvidenceSHA256 != "" && c.EvidenceSHA256 != r.Revision {
		err = &EvidenceError{Code: CodeRevision, Reason: "the recorded evidence no longer matches the saved revision"}
	}
	if err != nil {
		r.Status = "INCONCLUSIVE"
		code := evidenceCode(err)
		for _, x := range c.Expectations {
			r.Checks = append(r.Checks, CheckResult{Kind: x.Kind, Status: "INCONCLUSIVE", Detail: err.Error(), Code: code})
		}
		return r
	}
	for _, x := range c.Expectations {
		check := CheckResult{Kind: x.Kind, Status: "PASS", Detail: "Expectation satisfied by eligible recorded evidence."}
		if code, reason := eligibility(x, tape); code != "" {
			check.Status = "INCONCLUSIVE"
			check.Detail = reason
			check.Code = code
		} else {
			failures := scenario.Run(&scenario.Scenario{Version: 1, Assert: []scenario.Check{testCheck(x)}}, tape, nil)
			if len(failures) > 0 {
				check.Status = "FAIL"
				check.Detail = failures[0].Detail
			}
		}
		r.Checks = append(r.Checks, check)
		if check.Status == "FAIL" {
			r.Status = "FAIL"
		} else if check.Status == "INCONCLUSIVE" && r.Status != "FAIL" {
			r.Status = "INCONCLUSIVE"
		}
	}
	return r
}
func RunCase(dir string, c Case, candidate string) (Result, error) {
	if err := c.Validate(); err != nil {
		return Result{}, err
	}
	name := c.Evidence
	if candidate != "" {
		if !leaf(candidate, ".jsonl") {
			return Result{}, fmt.Errorf("candidate must be a plain .jsonl filename")
		}
		name = candidate
	}
	data, err := ReadLeaf(dir, name, ".jsonl", MaxRecording)
	if err != nil {
		// The case is well formed; the evidence is not available. Say so once per
		// expectation, with a code the interface can group, instead of returning a
		// result with no reasons at all.
		r := Result{Version: 1, Title: c.Title, Evidence: name, Status: "INCONCLUSIVE", Checks: []CheckResult{}, Note: "Evidence unavailable, unreadable or outside the lab limits."}
		for _, x := range c.Expectations {
			r.Checks = append(r.Checks, CheckResult{Kind: x.Kind, Status: "INCONCLUSIVE", Code: CodeUnreadable, Detail: "the recording " + name + " cannot be read: " + err.Error()})
		}
		return r, nil
	}
	return Evaluate(c, data, candidate), nil
}

// invalidCaseResult represents a suite entry that could not even be loaded, so a
// broken case is reported as one inconclusive entry rather than hiding the rest
// of the suite behind a single error.
func invalidCaseResult(name, reason string) Result {
	return Result{
		Version: 1, Title: strings.TrimSuffix(name, ".json"), Evidence: "",
		Status: "INCONCLUSIVE", Note: "The case definition could not be used.",
		Checks: []CheckResult{{Kind: "case", Status: "INCONCLUSIVE", Code: CodeInvalidCase, Detail: reason}},
	}
}
func RunSuite(dir string, s Suite) (SuiteResult, error) { return RunSuiteCandidate(dir, s, "") }

// RunSuiteCandidate applies an explicitly chosen recording to every case.
func RunSuiteCandidate(dir string, s Suite, candidate string) (SuiteResult, error) {
	out := SuiteResult{Version: 1, Title: s.Title, Status: "PASS", Cases: []Result{}}
	if len(s.Cases) == 0 || len(s.Cases) > MaxCases {
		return out, fmt.Errorf("invalid suite size")
	}
	for _, name := range s.Cases {
		data, err := ReadLeaf(dir, name, ".json", MaxDefinition)
		if err != nil {
			out.Cases = append(out.Cases, invalidCaseResult(name, "the case file cannot be opened inside the evidence directory"))
			out.Inconclusive++
			out.Invalid++
			if out.Status != "FAIL" {
				out.Status = "INCONCLUSIVE"
			}
			continue
		}
		c, err := ParseCase(data)
		if err != nil {
			out.Cases = append(out.Cases, invalidCaseResult(name, err.Error()))
			out.Inconclusive++
			out.Invalid++
			if out.Status != "FAIL" {
				out.Status = "INCONCLUSIVE"
			}
			continue
		}
		r, err := RunCase(dir, c, candidate)
		if err != nil {
			// A definition that parses but cannot be run is still one bad entry,
			// not a reason to hide the outcomes of the cases around it.
			out.Cases = append(out.Cases, invalidCaseResult(name, err.Error()))
			out.Inconclusive++
			out.Invalid++
			if out.Status != "FAIL" {
				out.Status = "INCONCLUSIVE"
			}
			continue
		}
		out.Cases = append(out.Cases, r)
		switch r.Status {
		case "PASS":
			out.Pass++
		case "FAIL":
			out.Fail++
			out.Status = "FAIL"
		default:
			out.Inconclusive++
			if out.Status != "FAIL" {
				out.Status = "INCONCLUSIVE"
			}
		}
	}
	return out, nil
}
