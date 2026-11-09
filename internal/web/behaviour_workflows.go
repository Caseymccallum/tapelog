package web

// Behaviour workflow adapters share the workbench's actual analysis functions.
// They perform no HTTP calls and never start a listener. Kept here while M6's
// recording model and analysers remain in this package.
import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Caseymccallum/tapelog/internal/behaviour"
)

// WorkflowResult is the versioned, payload-minimised terminal contract.
type WorkflowResult struct {
	SchemaVersion              int    `json:"schema_version"`
	Analysis                   string `json:"analysis"`
	Status                     string `json:"status"`
	ArgumentResultTextIncluded bool   `json:"argument_result_text_included"`
	Result                     any    `json:"result"`
}

// workflowSafe omits message-derived labels/fragments and recording task names.
// M6 normalisation is not a redaction guarantee: error messages may contain
// secrets even after normalisation. File and tool identifiers remain visible.
//
// ORDER CONTRACT (M14)
//
// An array of objects has one of two meanings, and the difference is a data
// contract, not a presentation preference:
//
//   - orderProducer — the producer's order IS the information. A timeline, a
//     ranked finding list, groups ordered by first appearance. Sorting it
//     silently rewrites the answer.
//   - orderCanonical — an unordered set whose only stable serialisation is a
//     canonical sort.
//
// The previous design sorted every array of objects EXCEPT the names on an
// exemption list. Adding a new ordered array without remembering the list has
// now cost three separate defects (groups, repeats, timeline), because the
// failure is silent and looks like a correct answer in the wrong order. So the
// default is inverted: an undeclared array keeps its producer order (losing
// order is a correctness bug; not canonicalising a set is cosmetic), and
// canonicalisation is opted into per PATH. TestArrayOrderContract walks real
// payloads and fails when an array of objects has no declared meaning, so
// forgetting is a red test rather than a silent reorder.
type arrayOrder int

const (
	orderProducer arrayOrder = iota
	orderCanonical
)

// arrayContracts declares the meaning of each array of objects by its KEY. A
// key's meaning is a property of the collection, not of where it hangs: a
// "timeline" is recording-ordered whether it sits at the root, under "summary"
// or under "comparison.a". arrayPathOverrides exists for the rare case where
// the same key genuinely means two different things at two depths; nothing uses
// it today, and a path override must come with a comment saying why.
var arrayContracts = map[string]arrayOrder{
	// --- producer-ordered: the order IS the answer -----------------------
	// Attributed conversations first by library position, the unattributed
	// remainder last. Sorting this by canonical JSON once put the remainder
	// first and re-alphabetised the conversations (M12 defect).
	"conversations": orderProducer,
	// A conversation's own findings are ranked by the detector.
	"repeats": orderProducer,
	// Per-tool counts, in first-appearance order inside the conversation.
	"tool_calls": orderProducer,
	// A timeline read out of order is a different timeline. Sorting it was
	// M13's high-severity defect.
	"timeline": orderProducer,
	// Fixed explanatory order: the terms build on each other.
	"definitions": orderProducer,
	// Ranked by the producer (calls, occurrences, confidence).
	"denied":   orderProducer,
	"failures": orderProducer,
	"tools":    orderProducer,
	// Reference comparison: every list is already sorted by the comparator
	// (current count, tool name, kind) or fixed by construction.
	"new_failures":      orderProducer,
	"resolved_failures": orderProducer,
	"new_denials":       orderProducer,
	"resolved_denials":  orderProducer,
	"new_repeats":       orderProducer,
	"resolved_repeats":  orderProducer,
	"rates":             orderProducer,
	// Recurrence is ranked by the producer (conversations, then calls, then
	// tool name). It was missing from the contract because the fixture never
	// produced one, so the gate walked an empty array (M14 review F7).
	"recurrence": orderProducer,
	// A recording comparison's changed repetitions are sorted by the comparator.
	"changed_repeats": orderProducer,
	// Conversation comparison: metric rows are in a stated order (calls,
	// results, unanswered, errors, denials, ambiguity, repetition), tools and
	// repeats are sorted by the comparator.
	"metrics": orderProducer,

	// --- unordered sets: canonicalised so output is byte-stable ----------
	"trend":   orderCanonical,
	"recent":  orderCanonical,
	"skipped": orderCanonical,
}

// arrayPathOverrides wins over the key contract for one exact path.
var arrayPathOverrides = map[string]arrayOrder{
	// A failure group's recent occurrences are a set of examples, not a
	// sequence: the group itself is ranked, the examples inside it are not.
	"failures.recent": orderCanonical,
}

// orderOf resolves an array's declared meaning: path override first, then the
// key's inherent meaning, then the safe default (keep producer order).
func orderOf(path, key string) arrayOrder {
	if o, ok := arrayPathOverrides[path]; ok {
		return o
	}
	if o, ok := arrayContracts[key]; ok {
		return o
	}
	return orderProducer
}

// arrayKey is the last segment of a JSON path: "comparison.a.timeline" ->
// "timeline".
func arrayKey(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '.' {
			return path[i+1:]
		}
	}
	return path
}

// arrayOrderDeclared reports whether this exact array has a declared meaning,
// either by path override or by its key's inherent meaning. Only an undeclared
// array is a contract gap.
func arrayOrderDeclared(path string) bool {
	if _, ok := arrayPathOverrides[path]; ok {
		return true
	}
	_, ok := arrayContracts[arrayKey(path)]
	return ok
}

func workflowSafe(v any) (any, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var data any
	if err = json.Unmarshal(raw, &data); err != nil {
		return nil, err
	}
	var clean func(any, string) any
	clean = func(x any, path string) any {
		switch v := x.(type) {
		case map[string]any:
			for _, k := range []string{"label", "fragment", "criteria", "reason", "name", "harness", "code"} {
				delete(v, k)
			}
			for k, y := range v {
				child := k
				if path != "" {
					child = path + "." + k
				}
				v[k] = clean(y, child)
			}
			return v
		case []any:
			for i := range v {
				v[i] = clean(v[i], path)
			}
			// The order below is never global: it is decided by this array's
			// declared meaning. Scalars are always left alone (they carry no
			// canonical form worth reordering, and sequences of scalars — a
			// recorded call sequence — are producer-ordered by definition).
			key := arrayKey(path)
			if orderOf(path, key) != orderCanonical {
				return v
			}
			allObjects := len(v) > 0
			for _, y := range v {
				if _, ok := y.(map[string]any); !ok {
					allObjects = false
					break
				}
			}
			if !allObjects {
				return v
			}
			sort.SliceStable(v, func(i, j int) bool {
				if key == "trend" || key == "recent" || path == "failures.recent" {
					getTime := func(item any) (time.Time, error) {
						m := item.(map[string]any)
						value, _ := m["first_ts"].(string)
						if value == "" {
							value, _ = m["ts"].(string)
						}
						return time.Parse(time.RFC3339Nano, value)
					}
					a, ea := getTime(v[i])
					b, eb := getTime(v[j])
					if ea == nil && eb != nil {
						return true
					}
					if ea != nil && eb == nil {
						return false
					}
					if ea == nil && eb == nil && !a.Equal(b) {
						if path == "recent" || path == "failures.recent" {
							return a.After(b)
						}
						return a.Before(b)
					}
				}
				a, _ := json.Marshal(v[i])
				b, _ := json.Marshal(v[j])
				return string(a) < string(b)
			})
			return v
		default:
			return x
		}
	}
	return clean(data, ""), nil
}

// undeclaredArrays lists every path in v holding an array of objects with no
// declared order contract. It exists so a contract gap is a failing test
// instead of a silently reordered payload.
func undeclaredArrays(v any) ([]string, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var data any
	if err = json.Unmarshal(raw, &data); err != nil {
		return nil, err
	}
	var found []string
	var walk func(any, string)
	walk = func(x any, path string) {
		switch v := x.(type) {
		case map[string]any:
			for k, y := range v {
				child := k
				if path != "" {
					child = path + "." + k
				}
				walk(y, child)
			}
		case []any:
			allObjects := len(v) > 0
			for _, y := range v {
				if _, ok := y.(map[string]any); !ok {
					allObjects = false
					break
				}
			}
			if allObjects {
				if !arrayOrderDeclared(path) {
					found = append(found, path)
				}
			}
			for _, y := range v {
				walk(y, path)
			}
		}
	}
	walk(data, "")
	sort.Strings(found)
	return found, nil
}

func workflowStatus(idx *behaviourIndex) string {
	if idx.Matched == 0 || idx.Capped || idx.Truncated || len(idx.Skipped) > 0 {
		return "partial"
	}
	for _, a := range idx.Sessions {
		if !a.Info.Complete || !a.Info.ChainOK || a.Info.Ambiguous || a.Info.Structure == "invalid" {
			return "partial"
		}
	}
	return "complete"
}

// BehaviourWorkflow analyses either the bounded directory catalogue or a set
// of explicit plain file names. All file access uses the workbench's confined
// os.Root reader and its byte/event bounds, with a new cache per invocation.
func BehaviourWorkflow(dir string, names []string, kind string) (*WorkflowResult, error) {
	if dir == "" {
		return nil, fmt.Errorf("--dir is required")
	}
	switch kind {
	case "summary", "repeats", "failures", "conversations":
	default:
		return nil, fmt.Errorf("unknown behaviour analysis")
	}
	idx, err := buildWorkflowIndex(dir, names)
	if err != nil {
		return nil, err
	}
	ov := aggregateOverview(idx)
	safe, err := workflowSafe(ov)
	if err != nil {
		return nil, err
	}
	m := safe.(map[string]any)
	// Stable summary shape across commands: keep counts, denominators, coverage,
	// trend and references, while restricting the specific group lists requested.
	pruneForKind(m, kind)
	return &WorkflowResult{1, kind, workflowStatus(idx), false, m}, nil
}

// buildWorkflowIndex reads the library (or an explicit list of recordings) the
// way every behaviour workflow does: same bounds, same skip reporting, same
// per-invocation cache.
func buildWorkflowIndex(dir string, names []string) (*behaviourIndex, error) {
	s := &server{dir: dir}
	if len(names) == 0 {
		idx, err := s.buildBehaviourIndex(behaviourFilter{})
		if err != nil {
			return nil, fmt.Errorf("cannot inspect recording directory: %q", err.Error())
		}
		return idx, nil
	}
	if len(names) > maxAnalysisFiles {
		return nil, fmt.Errorf("at most %d explicit recordings", maxAnalysisFiles)
	}
	idx := &behaviourIndex{Files: len(names)}
	seen := map[string]bool{}
	budget := int64(maxAnalysisBytes)
	names = append([]string(nil), names...)
	sort.Strings(names)
	for _, name := range names {
		if seen[name] {
			return nil, fmt.Errorf("duplicate recording name")
		}
		seen[name] = true
		if _, ok := safeJoin(dir, name); !ok {
			return nil, fmt.Errorf("invalid recording name %q: use a plain .jsonl filename inside --dir", name)
		}
		rec, e := s.recording(name, budget)
		if e != nil {
			idx.Skipped = append(idx.Skipped, skippedFile{File: name, Reason: e.Error()})
			continue
		}
		budget -= int64(len(rec.Data))
		idx.Analysed++
		idx.Matched++
		idx.Sessions = append(idx.Sessions, analyseRecording(rec))
	}
	return idx, nil
}

// ConversationWorkflow answers "what happened in this conversation" without a
// browser: the same summary the API serves, plus one page of its ordered,
// uncapped timeline.
func ConversationWorkflow(dir string, names []string, id string, offset, limit int) (*WorkflowResult, error) {
	if dir == "" {
		return nil, fmt.Errorf("--dir is required")
	}
	if !behaviour.ValidConversationID(id) {
		return nil, fmt.Errorf("--conversation must be a plain client-declared id (1-128 bytes of A-Za-z0-9._:-); the unattributed remainder is not a conversation")
	}
	if offset < 0 {
		return nil, fmt.Errorf("--offset must be zero or positive")
	}
	if limit < 1 {
		return nil, fmt.Errorf("--limit must be at least 1")
	}
	if limit > conversationPageMax {
		limit = conversationPageMax
	}
	idx, err := buildWorkflowIndex(dir, names)
	if err != nil {
		return nil, err
	}
	calls := libraryCalls(idx)
	summary := behaviour.FindConversation(behaviour.GroupConversations(calls), id)
	if summary == nil {
		return nil, fmt.Errorf("no analysed recording in this view declares conversation %q", id)
	}
	all := behaviour.ConversationCalls(calls, id)
	page := pageOf(all, offset, limit)
	entries := make([]behaviour.TimelineEntry, 0, len(page))
	for _, c := range page {
		entries = append(entries, behaviour.TimelineEntryFor(c))
	}
	payload := map[string]any{
		"conversation": id,
		"summary":      *summary,
		"timeline":     entries,
		"total":        len(all),
		"offset":       offset,
		"limit":        limit,
		"has_more":     offset+len(entries) < len(all),
		"evidence":     conversationEvidence(summary, idx),
	}
	safe, err := workflowSafe(payload)
	if err != nil {
		return nil, err
	}
	return &WorkflowResult{1, "conversation", workflowStatus(idx), false, safe}, nil
}

// CompareConversationsWorkflow compares two conversations on counted facts.
func CompareConversationsWorkflow(dir string, names []string, a, b string) (*WorkflowResult, error) {
	if dir == "" {
		return nil, fmt.Errorf("--dir is required")
	}
	for _, id := range []string{a, b} {
		if !behaviour.ValidConversationID(id) {
			return nil, fmt.Errorf("--a and --b must be plain client-declared conversation ids")
		}
	}
	idx, err := buildWorkflowIndex(dir, names)
	if err != nil {
		return nil, err
	}
	cmp := behaviour.CompareConversations(behaviour.GroupConversations(libraryCalls(idx)), a, b)
	safe, err := workflowSafe(map[string]any{"comparison": cmp})
	if err != nil {
		return nil, err
	}
	status := workflowStatus(idx)
	if !cmp.Comparable {
		status = "partial"
	}
	return &WorkflowResult{1, "compare-conversations", status, false, safe}, nil
}

// pruneForKind restricts one payload to the group lists its command asks for.
// It is shared with the order-contract test so the test walks exactly what a
// consumer receives.
func pruneForKind(m map[string]any, kind string) {
	if kind == "repeats" {
		delete(m, "failures")
		delete(m, "denied")
		delete(m, "tools")
	}
	if kind == "failures" {
		delete(m, "repeats")
		delete(m, "denied")
		delete(m, "tools")
	}
	if kind == "conversations" {
		// Conversation grouping is the whole answer: drop the per-tool and
		// per-signature lists that summary would otherwise carry.
		for _, k := range []string{"failures", "denied", "tools", "repeats", "trend", "recent"} {
			delete(m, k)
		}
	}
}

// BehaviourWorkflowCompare uses the exact same M6 comparator as the workbench.
func BehaviourWorkflowCompare(dir, reference, candidate string) (*WorkflowResult, error) {
	if dir == "" || reference == "" || candidate == "" {
		return nil, fmt.Errorf("--dir, --reference and --candidate are required")
	}
	if reference == candidate {
		return nil, fmt.Errorf("reference and candidate must be different recordings")
	}
	for _, n := range []string{reference, candidate} {
		if _, ok := safeJoin(dir, n); !ok {
			return nil, fmt.Errorf("invalid recording name %q", n)
		}
	}
	s := &server{dir: dir}
	a, err := s.recording(reference)
	if err != nil {
		return nil, fmt.Errorf("cannot open reference recording: %q", err.Error())
	}
	b, err := s.recording(candidate)
	if err != nil {
		return nil, fmt.Errorf("cannot open candidate recording: %q", err.Error())
	}
	aa, bb := analyseRecording(a), analyseRecording(b)
	delta := compareBehaviour(aa, bb)
	safe, err := workflowSafe(delta)
	if err != nil {
		return nil, err
	}
	status := "complete"
	for _, r := range []*recording{a, b} {
		if !r.Info.Complete || !r.Info.ChainOK || r.Info.Ambiguous || r.Info.Structure == "invalid" {
			status = "partial"
		}
	}
	// Tool usage differences are an additive view over the existing M6 per-tool
	// counts, not a second detector.
	ta := aggregateOverview(&behaviourIndex{Sessions: []*sessionAnalysis{aa}}).Tools
	tb := aggregateOverview(&behaviourIndex{Sessions: []*sessionAnalysis{bb}}).Tools
	type counts struct {
		Reference int `json:"reference"`
		Candidate int `json:"candidate"`
	}
	tools := map[string]counts{}
	for _, t := range ta {
		v := tools[t.Tool]
		v.Reference = t.Calls
		tools[t.Tool] = v
	}
	for _, t := range tb {
		v := tools[t.Tool]
		v.Candidate = t.Calls
		tools[t.Tool] = v
	}
	m := safe.(map[string]any)
	safeTools, _ := workflowSafe(tools)
	m["tool_usage"] = safeTools
	changed := []repeatDelta{}
	ar, br := repeatCounts(aa), repeatCounts(bb)
	for key, x := range ar {
		if y := br[key]; y != nil && (x.findings != y.findings || x.occurrences != y.occurrences) {
			parts := strings.SplitN(key, "\x00", 2)
			kind, tool := parts[0], ""
			if len(parts) > 1 {
				tool = parts[1]
			}
			changed = append(changed, repeatDelta{Kind: kind, Tool: tool, Tools: y.tools, Confidence: y.confidence, Reference: x.findings, Current: y.findings, ReferenceOccurrences: x.occurrences, CurrentOccurrences: y.occurrences})
		}
	}
	sort.Slice(changed, func(i, j int) bool {
		if changed[i].Kind != changed[j].Kind {
			return changed[i].Kind < changed[j].Kind
		}
		return changed[i].Tool < changed[j].Tool
	})
	safeChanged, _ := workflowSafe(changed)
	m["changed_repeats"] = safeChanged
	at, ae := workbenchTape(a)
	bt, be := workbenchTape(b)
	if ae == nil && be == nil {
		comparison := compareAttempts(at, bt)
		comparison["available"] = true
		m["call_comparison"] = comparison
	} else {
		m["call_comparison"] = map[string]any{"available": false, "reason": "Call-level comparison requires complete, verified, unambiguous inline recordings within 10,000 events."}
		status = "partial"
	}
	return &WorkflowResult{1, "compare", status, false, m}, nil
}
