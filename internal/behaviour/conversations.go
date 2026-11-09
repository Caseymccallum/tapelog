package behaviour

import (
	"sort"
)

// Conversation grouping (M12).
//
// A conversation id is CLIENT-DECLARED: it groups activity, and it is not a
// session boundary, not proof that a conversation finished, and not
// authenticated. Calls with no id are "unattributed" and are never merged into
// a fabricated conversation. Calls whose result correlation was ambiguous are
// counted, never assigned to a conversation they may not belong to.

// UnattributedID is the label used for calls the client did not attribute. It
// is intentionally not a plausible conversation id: a client cannot mint it by
// sending the same string.
const UnattributedID = "(unattributed)"

// ValidConversationID applies the recording contract's identifier rules at the
// ANALYSIS layer, not just at the write boundary. A recording can be handed to
// Tapelog by anyone (or edited), so an id that would be rejected on the wire
// must not become a group here: control characters would reach the terminal,
// an oversized value would become a "conversation", and the literal
// UnattributedID would forge the remainder.
func ValidConversationID(s string) bool {
	if s == "" || len(s) > 128 || s == UnattributedID {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '.' || c == '_' || c == ':' || c == '-':
		default:
			return false
		}
	}
	return true
}

// HasAttribution reports whether any call carries a valid declared conversation
// id. It decides whether repetition is measured per conversation or per session.
func HasAttribution(calls []CallRecord) bool {
	for _, c := range calls {
		if ValidConversationID(c.ConversationID) {
			return true
		}
	}
	return false
}

// ConversationSummary is the observed activity of one attributed conversation,
// or of the unattributed group. Every field is a count of recorded facts; none
// is an estimate, a score, or a claim about the agent's intent.
type ConversationSummary struct {
	ID         string `json:"id"`
	Attributed bool   `json:"attributed"`
	Calls      int    `json:"calls"`
	Results    int    `json:"results"`
	Failures   int    `json:"failures"`
	Denials    int    `json:"denials"`
	// AmbiguousCalls counts CALLS whose result pairing could not be
	// established (a request id was shared by more than one outstanding
	// call). It is deliberately named after calls: one unpaired ambiguous
	// result shared by two calls counts on both, because the recording cannot
	// say which of them the result belongs to. The count is UNKNOWN, not zero.
	AmbiguousCalls int `json:"ambiguous_calls,omitempty"`
	// Unanswered counts calls with no recorded result. That is not a failure
	// and not a success: a denial carries no result by design, and a missing
	// answer is a gap in the recording.
	Unanswered int `json:"unanswered,omitempty"`
	// FirstSeq orders groups by first appearance in the recording.
	FirstSeq uint64 `json:"first_seq"`
	// FirstLibrarySeq is the group's first call's position in the library
	// call list (all analysed recordings, in library order). It exists because
	// FirstSeq restarts at 1 in every recording, so a cross-file sort by it is
	// neither library order nor chronology. Zero means "no library position
	// was assigned" (a caller building groups from one hand-made list); the
	// grouping then falls back to FirstSeq and the view says the order is
	// per-recording rather than library-wide.
	FirstLibrarySeq uint64   `json:"first_library_seq,omitempty"`
	Tools           []string `json:"tools"`
	// ToolCalls is the same set with per-tool call counts, in first-appearance
	// order. It exists so a comparison can state "read_file was called 3 times
	// here and 1 time there" without re-reading a truncated timeline.
	ToolCalls []ToolCount `json:"tool_calls,omitempty"`
	// Repeats are findings confined to THIS conversation.
	Repeats []RepeatFinding `json:"repeats,omitempty"`
	// Files are the recordings this conversation appears in, in encounter
	// order. A conversation is a client-declared label, so it can span files.
	Files []string `json:"files,omitempty"`
	// Timeline is a BOUNDED, ordered view of this conversation's calls. Each
	// entry references its own recording and sequence so the workspace can
	// open the underlying evidence instead of trusting a summary.
	Timeline          []TimelineEntry `json:"timeline,omitempty"`
	TimelineTruncated bool            `json:"timeline_truncated,omitempty"`
}

// FindConversation returns the summary for one declared id, or nil when no
// group carries it. It exists so every surface looks a conversation up in the
// same way instead of re-grouping the library for a single answer.
func FindConversation(groups []ConversationSummary, id string) *ConversationSummary {
	if !ValidConversationID(id) {
		return nil
	}
	for i := range groups {
		if groups[i].ID == id {
			out := groups[i]
			return &out
		}
	}
	return nil
}

// ToolCount is how many calls one tool received inside one conversation.
type ToolCount struct {
	Tool  string `json:"tool"`
	Calls int    `json:"calls"`
}

// TimelineEntry is one call as it appears inside a conversation. It carries no
// result text: the workspace opens the event for that.
type TimelineEntry struct {
	File      string `json:"file,omitempty"`
	Seq       uint64 `json:"seq"`
	Index     int    `json:"index"`
	TS        string `json:"ts,omitempty"`
	Tool      string `json:"tool"`
	Status    string `json:"status,omitempty"`
	Verdict   string `json:"verdict,omitempty"`
	IsError   bool   `json:"is_error,omitempty"`
	Ambiguous bool   `json:"ambiguous,omitempty"`
	// Answered says whether a result was recorded and paired. A missing result
	// is not a success and is never drawn as one.
	Answered bool `json:"answered"`
}

// maxTimeline bounds per-conversation accumulation: the analysis runs on every
// request and a long-lived gateway recording can hold tens of thousands of
// calls under one id.
const maxTimeline = 200

// TimelineEntryFor is the workspace's view of one recorded call. It carries no
// result text: the workspace opens the event for that.
func TimelineEntryFor(c CallRecord) TimelineEntry {
	return TimelineEntry{
		File: c.File, Seq: c.Seq, Index: c.Index, TS: c.TS, Tool: c.Tool, Status: c.Status,
		Verdict: c.Verdict, IsError: c.IsError,
		Ambiguous: c.CorrelationAmbiguous, Answered: c.ResultSeq != 0,
	}
}

// ConversationCalls returns every call that belongs to one conversation, in the
// order the caller supplied (library order). It re-applies the identifier rules
// on read, so a hand-edited recording cannot forge membership, and it is
// deliberately uncapped: the summary's Timeline is bounded for the overview, but
// a developer who opens one conversation must be able to page through all of it.
func ConversationCalls(calls []CallRecord, id string) []CallRecord {
	if !ValidConversationID(id) {
		return nil
	}
	var out []CallRecord
	for _, c := range calls {
		if c.ConversationID == id {
			out = append(out, c)
		}
	}
	return out
}

// GroupConversations buckets calls by their declared conversation id. Groups are
// ordered by first appearance; the unattributed group is always last so it
// reads as a remainder rather than as a conversation.
func GroupConversations(calls []CallRecord) []ConversationSummary {
	// Single pass: bucket once, then detect repeats inside each bucket. An
	// earlier shape rescanned the whole call list per group, which is O(n*g)
	// and measured over a second at 12k conversations.
	type bucket struct {
		sum   ConversationSummary
		calls []CallRecord
		// per-tool counts, kept in first-appearance order.
		toolOrder []string
		toolCalls map[string]int
	}
	index := map[string]*bucket{}
	var order []string
	for _, c := range calls {
		id := c.ConversationID
		if !ValidConversationID(id) {
			id = "" // absent, rejected, or forged sentinel: unattributed
		}
		label := id
		if label == "" {
			label = UnattributedID
		}
		b, ok := index[label]
		if !ok {
			b = &bucket{sum: ConversationSummary{ID: label, Attributed: id != "", FirstSeq: c.Seq}, toolCalls: map[string]int{}}
			index[label] = b
			order = append(order, label)
		}
		if _, seen := b.toolCalls[c.Tool]; !seen {
			b.toolOrder = append(b.toolOrder, c.Tool)
		}
		b.toolCalls[c.Tool]++
		b.sum.Calls++
		if c.ResultSeq != 0 {
			b.sum.Results++
		} else {
			b.sum.Unanswered++
		}
		if c.IsError {
			b.sum.Failures++
		}
		if c.Verdict == "deny" {
			b.sum.Denials++
		}
		if c.CorrelationAmbiguous {
			b.sum.AmbiguousCalls++
		}
		if b.sum.FirstLibrarySeq == 0 && c.LibrarySeq != 0 {
			b.sum.FirstLibrarySeq = c.LibrarySeq
		}
		if !containsString(b.sum.Tools, c.Tool) {
			b.sum.Tools = append(b.sum.Tools, c.Tool)
		}
		if c.File != "" && !containsString(b.sum.Files, c.File) {
			b.sum.Files = append(b.sum.Files, c.File)
		}
		if len(b.sum.Timeline) < maxTimeline {
			b.sum.Timeline = append(b.sum.Timeline, TimelineEntryFor(c))
		} else {
			b.sum.TimelineTruncated = true
		}
		b.calls = append(b.calls, c)
	}

	out := make([]ConversationSummary, 0, len(order))
	for _, label := range order {
		b := index[label]
		// Repeats are computed per group: a call repeated in four
		// conversations is four small groups, not one large loop.
		b.sum.Repeats = DetectRepeats(b.calls)
		for _, tool := range b.toolOrder {
			b.sum.ToolCalls = append(b.sum.ToolCalls, ToolCount{Tool: tool, Calls: b.toolCalls[tool]})
		}
		out = append(out, b.sum)
	}
	sort.SliceStable(out, func(a, b int) bool {
		if out[a].Attributed != out[b].Attributed {
			return out[a].Attributed // unattributed last
		}
		// Library order when it is known on both sides: a per-recording
		// sequence number restarts at 1, so it cannot order two groups that
		// first appeared in different files. A group with no library position
		// sorts after positioned groups rather than pretending to a place.
		pa, pb := out[a].FirstLibrarySeq, out[b].FirstLibrarySeq
		if pa != pb {
			if pa == 0 || pb == 0 {
				return pb == 0
			}
			return pa < pb
		}
		return out[a].FirstSeq < out[b].FirstSeq
	})
	return out
}

// Recurrence describes one tool+operation fingerprint observed in more than one
// conversation. It is the honest form of "the same thing happened again": it
// does not claim a loop, and it counts calls rather than implying one.
type Recurrence struct {
	Tool      string `json:"tool"`
	Key       string `json:"key"` // redacted fingerprint, already safe to print
	CallCount int    `json:"calls"`
	// Conversations is the number of distinct attributed conversations the
	// operation appeared in. Unattributed activity is counted separately and
	// never folded into this number.
	Conversations int `json:"conversations"`
	// Unattributed is how many of the calls had no declared conversation.
	Unattributed int `json:"unattributed,omitempty"`
}

// DetectRecurrence finds operations that appear in several conversations. It
// deliberately requires at least two distinct conversations: repeated calls
// inside ONE conversation are a loop and belong to DetectRepeats, not here.
func DetectRecurrence(calls []CallRecord) []Recurrence {
	type acc struct {
		tool  string
		key   string
		calls int
		convs map[string]bool
		un    int
	}
	groups := map[string]*acc{}
	for _, c := range calls {
		gk := c.Tool + "\x00" + c.ArgsKey
		g, ok := groups[gk]
		if !ok {
			g = &acc{tool: c.Tool, key: c.ArgsKey, convs: map[string]bool{}}
			groups[gk] = g
		}
		g.calls++
		if c.ConversationID == "" {
			g.un++
		} else {
			g.convs[c.ConversationID] = true
		}
	}
	var out []Recurrence
	for _, g := range groups {
		if len(g.convs) < 2 {
			continue // one conversation (or none) is not recurrence
		}
		out = append(out, Recurrence{
			Tool: g.tool, Key: g.key, CallCount: g.calls,
			Conversations: len(g.convs), Unattributed: g.un,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Conversations != out[j].Conversations {
			return out[i].Conversations > out[j].Conversations
		}
		if out[i].CallCount != out[j].CallCount {
			return out[i].CallCount > out[j].CallCount
		}
		return out[i].Tool < out[j].Tool
	})
	return out
}

func containsString(hay []string, needle string) bool {
	for _, s := range hay {
		if s == needle {
			return true
		}
	}
	return false
}
