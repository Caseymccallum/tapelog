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
	clean = func(x any, key string) any {
		switch v := x.(type) {
		case map[string]any:
			for _, k := range []string{"label", "fragment", "criteria", "reason", "name", "harness", "code"} {
				delete(v, k)
			}
			for k, y := range v {
				v[k] = clean(y, k)
			}
			return v
		case []any:
			for i := range v {
				v[i] = clean(v[i], key)
			}
			// Sort unordered sets of objects by their canonical JSON to break map-order
			// ties. Scalar arrays (including ordered call sequences) retain their order.
			allObjects := len(v) > 0
			for _, y := range v {
				if _, ok := y.(map[string]any); !ok {
					allObjects = false
					break
				}
			}
			if allObjects {
				sort.SliceStable(v, func(i, j int) bool {
					if key == "trend" || key == "recent" {
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
							if key == "recent" {
								return a.After(b)
							}
							return a.Before(b)
						}
					}
					a, _ := json.Marshal(v[i])
					b, _ := json.Marshal(v[j])
					return string(a) < string(b)
				})
			}
			return v
		default:
			return x
		}
	}
	return clean(data, ""), nil
}

func workflowStatus(idx *behaviourIndex) string {
	if idx.Matched == 0 || idx.Capped || idx.Truncated || len(idx.Skipped) > 0 {
		return "partial"
	}
	for _, a := range idx.Sessions {
		if !a.Info.Complete || !a.Info.ChainOK || a.Info.Ambiguous {
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
	case "summary", "repeats", "failures":
	default:
		return nil, fmt.Errorf("unknown behaviour analysis")
	}
	s := &server{dir: dir}
	var idx *behaviourIndex
	var err error
	if len(names) == 0 {
		idx, err = s.buildBehaviourIndex(behaviourFilter{})
		if err != nil {
			return nil, fmt.Errorf("cannot inspect recording directory: %q", err.Error())
		}
	} else {
		if len(names) > maxAnalysisFiles {
			return nil, fmt.Errorf("at most %d explicit recordings", maxAnalysisFiles)
		}
		idx = &behaviourIndex{Files: len(names)}
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
	}
	ov := aggregateOverview(idx)
	safe, err := workflowSafe(ov)
	if err != nil {
		return nil, err
	}
	m := safe.(map[string]any)
	// Stable summary shape across commands: keep counts, denominators, coverage,
	// trend and references, while restricting the specific group lists requested.
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
	return &WorkflowResult{1, kind, workflowStatus(idx), false, m}, nil
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
		if !r.Info.Complete || !r.Info.ChainOK || r.Info.Ambiguous {
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
