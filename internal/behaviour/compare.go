package behaviour

// Two-conversation comparison (M14).
//
// The unit of investigation is the conversation, so the natural question is
// "what changed between these two?" The answer may only contain recorded
// facts, and the dangerous failure mode is a difference that reads like a
// judgement: "B reasoned less effectively" when the recording supports only
// "B recorded three more tool errors".
//
// So this file produces three things and nothing else:
//
//  1. Counts, one per metric, each the difference between two recorded numbers.
//  2. Set differences (tool usage), which are observations about what appears
//     in a recording, not about intent.
//  3. Explicit caveats when the comparison cannot be precise: ambiguous
//     correlation, unattributed activity, missing results, incomplete
//     timelines, or a conversation that is not there.
//
// Nothing here scores, ranks or characterises a conversation.

import (
	"sort"
	"strconv"
)

// MetricDelta is one count compared across two conversations.
type MetricDelta struct {
	Metric string `json:"metric"`
	A      int    `json:"a"`
	B      int    `json:"b"`
	Change int    `json:"change"`
	// Note explains what the number does not mean, where that matters.
	Note string `json:"note,omitempty"`
}

// ToolDelta is a tool's recorded call count in each conversation.
type ToolDelta struct {
	Tool string `json:"tool"`
	A    int    `json:"a"`
	B    int    `json:"b"`
}

// RepeatDelta compares one repetition fingerprint (detector kind + tool). The
// counts are CALLS INVOLVED in the finding, not findings themselves.
type RepeatDelta struct {
	Kind string `json:"kind"`
	Tool string `json:"tool"`
	A    int    `json:"a"`
	B    int    `json:"b"`
}

// ConversationComparison is the whole answer. Comparable is false when the
// evidence cannot support a comparison at all (a missing conversation, or the
// unattributed remainder); Caveats carries the softer limits that still permit
// a comparison of recorded counts.
type ConversationComparison struct {
	A          ConversationSummary `json:"a"`
	B          ConversationSummary `json:"b"`
	Metrics    []MetricDelta       `json:"metrics"`
	Tools      []ToolDelta         `json:"tools"`
	Repeats    []RepeatDelta       `json:"repeats"`
	Findings   []string            `json:"findings"`
	Caveats    []string            `json:"caveats"`
	Comparable bool                `json:"comparable"`
	// Explanation says why a pair cannot be compared. It is deliberately not
	// called "reason": workflowSafe deletes "reason" everywhere (a recorded
	// policy reason can carry sensitive provenance), so a field with that name
	// vanished from the CLI while the API kept it (M14 review F3).
	Explanation string `json:"explanation,omitempty"`
}

// CompareConversations compares two groups produced by GroupConversations. It
// refuses to compare the unattributed remainder with a conversation: the
// remainder is not a conversation, and treating it as one is how a precise
// claim gets made from evidence that does not exist.
func CompareConversations(groups []ConversationSummary, aID, bID string) ConversationComparison {
	out := ConversationComparison{
		Comparable: true,
		Findings:   []string{}, Caveats: []string{},
		Metrics: []MetricDelta{}, Tools: []ToolDelta{}, Repeats: []RepeatDelta{},
	}
	find := func(id string) *ConversationSummary {
		for i := range groups {
			if groups[i].ID == id {
				return &groups[i]
			}
		}
		return nil
	}
	if aID == bID {
		out.Comparable = false
		out.Explanation = "a conversation cannot be compared with itself"
		return out
	}
	a, b := find(aID), find(bID)
	if a == nil || b == nil {
		out.Comparable = false
		switch {
		case a == nil && b == nil:
			out.Explanation = "neither conversation is present in the analysed recordings"
		case a == nil:
			out.Explanation = "the first conversation is not present in the analysed recordings"
		default:
			out.Explanation = "the second conversation is not present in the analysed recordings"
		}
		return out
	}
	out.A, out.B = *a, *b
	if !a.Attributed || !b.Attributed {
		out.Comparable = false
		out.Explanation = "the unattributed remainder is not a conversation: those calls declared no conversation id, so they cannot be compared as one"
		return out
	}

	count := func(metric string, av, bv int, note string) {
		out.Metrics = append(out.Metrics, MetricDelta{Metric: metric, A: av, B: bv, Change: bv - av, Note: note})
	}
	count("Tool calls", a.Calls, b.Calls, "")
	count("Recorded results", a.Results, b.Results, "a call with no recorded result was denied or never answered; that is not a failure")
	count("Calls with no recorded result", a.Unanswered, b.Unanswered, "denials carry no result by design, so this is a gap, not a failure")
	count("Tool errors", a.Failures, b.Failures, "counted from recorded results only")
	count("Policy denials", a.Denials, b.Denials, "")
	count("Ambiguous calls", a.AmbiguousCalls, b.AmbiguousCalls, "a shared request id means the pairing is unknown, so this number is unknown rather than zero")
	count("Repetition findings", len(a.Repeats), len(b.Repeats), "a repetition is an observation, not proof of a loop")

	// Tool usage, per tool call count, over the union of both conversations.
	usage := map[string]*ToolDelta{}
	for _, pair := range []struct {
		g *ConversationSummary
		a bool
	}{{a, true}, {b, false}} {
		for _, tc := range pair.g.ToolCalls {
			d := usage[tc.Tool]
			if d == nil {
				d = &ToolDelta{Tool: tc.Tool}
				usage[tc.Tool] = d
			}
			if pair.a {
				d.A = tc.Calls
			} else {
				d.B = tc.Calls
			}
		}
	}
	for _, d := range usage {
		out.Tools = append(out.Tools, *d)
	}
	sort.Slice(out.Tools, func(i, j int) bool { return out.Tools[i].Tool < out.Tools[j].Tool })
	for _, t := range out.Tools {
		switch {
		case t.A == 0 && t.B > 0:
			out.Findings = append(out.Findings, t.Tool+" was called only in the second conversation ("+strconv.Itoa(t.B)+").")
		case t.B == 0 && t.A > 0:
			out.Findings = append(out.Findings, t.Tool+" was called only in the first conversation ("+strconv.Itoa(t.A)+").")
		}
	}

	// Repetitions, keyed by detector kind and tool.
	reps := map[string]*RepeatDelta{}
	for _, pair := range []struct {
		g *ConversationSummary
		a bool
	}{{a, true}, {b, false}} {
		for _, f := range pair.g.Repeats {
			key := f.Kind + "\x00" + f.Tool
			d := reps[key]
			if d == nil {
				d = &RepeatDelta{Kind: f.Kind, Tool: f.Tool}
				reps[key] = d
			}
			if pair.a {
				d.A += f.Count
			} else {
				d.B += f.Count
			}
		}
	}
	for _, d := range reps {
		out.Repeats = append(out.Repeats, *d)
	}
	sort.Slice(out.Repeats, func(i, j int) bool {
		if out.Repeats[i].Kind != out.Repeats[j].Kind {
			return out.Repeats[i].Kind < out.Repeats[j].Kind
		}
		return out.Repeats[i].Tool < out.Repeats[j].Tool
	})

	// The findings are the plain sentences the numbers support, produced here
	// so the CLI, the API and the card cannot drift apart: every one is a count
	// with a direction, never a character judgement.
	for _, m := range out.Metrics {
		if m.Change == 0 {
			continue
		}
		out.Findings = append(out.Findings, metricSentence(m.Metric, m.Change))
	}

	// Caveats: what stops these numbers from being a precise comparison.
	if a.AmbiguousCalls > 0 || b.AmbiguousCalls > 0 {
		out.Caveats = append(out.Caveats, "At least one conversation has calls whose result pairing is unknown (a request id was shared). Answered-call and result counts are unknown, not exact.")
	}
	if a.Unanswered > 0 || b.Unanswered > 0 {
		out.Caveats = append(out.Caveats, "At least one conversation has calls with no recorded result. They are counted as unanswered and never as failures, so error counts describe recorded results only.")
	}
	if !sameSet(a.Files, b.Files) {
		out.Caveats = append(out.Caveats, "The two conversations appear in different recordings. The counts describe what each recording contains; the recordings may cover different tasks or partial sessions.")
	}
	if a.TimelineTruncated || b.TimelineTruncated {
		out.Caveats = append(out.Caveats, "At least one conversation's timeline is truncated in this view, so its per-event detail is incomplete even though its counts are not.")
	}
	out.Caveats = append(out.Caveats, "A difference is an observation about recorded boundary activity. It says nothing about the agent's reasoning, quality or intent, none of which is recorded.")
	return out
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := map[string]int{}
	for _, s := range a {
		seen[s]++
	}
	for _, s := range b {
		seen[s]--
	}
	for _, n := range seen {
		if n != 0 {
			return false
		}
	}
	return true
}

// metricSentence turns a delta into "the second conversation recorded 3 more
// tool errors than the first". Direction and magnitude only.
func metricSentence(metric string, change int) string {
	n := change
	if n < 0 {
		n = -n
	}
	unit := lowerFirst(metric)
	if n == 1 {
		unit = singularMetric(metric)
	}
	if change > 0 {
		return "The second conversation recorded " + strconv.Itoa(n) + " more " + unit + " than the first."
	}
	return "The first conversation recorded " + strconv.Itoa(n) + " more " + unit + " than the second."
}

// lowerFirst lowercases the first letter so a metric name reads as part of the
// sentence: "2 more recorded results", not "2 more Recorded results".
func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	if r[0] >= 'A' && r[0] <= 'Z' {
		r[0] += 'a' - 'A'
	}
	return string(r)
}

func singularMetric(s string) string {
	switch s {
	case "Tool calls":
		return "tool call"
	case "Recorded results":
		return "recorded result"
	case "Calls with no recorded result":
		return "call with no recorded result"
	case "Tool errors":
		return "tool error"
	case "Policy denials":
		return "policy denial"
	case "Ambiguous calls":
		return "call with an ambiguous result"
	case "Repetition findings":
		return "repetition finding"
	}
	return s
}
