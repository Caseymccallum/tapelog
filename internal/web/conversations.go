package web

// Conversation-first investigation (M14).
//
// The overview answers "what is in this library". These handlers answer the two
// questions a developer asks next: what happened in THIS conversation, and what
// changed between these two? Both are built on the same libraryCalls() and
// behaviour.GroupConversations() the overview uses, so no surface can disagree
// about membership, counts or order.

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/Caseymccallum/tapelog/internal/behaviour"
)

const (
	// conversationPageDefault and conversationPageMax bound one page of a
	// conversation's timeline. The overview caps a conversation at 200 entries;
	// opening one conversation pages through all of it instead of pretending
	// the first 200 are the whole story.
	conversationPageDefault = 100
	conversationPageMax     = 500
)

// conversationAPI serves one conversation: its summary, its bookkeeping and one
// page of its ordered timeline.
//
// A conversation id is CLIENT-DECLARED correlation, so it is validated on read
// exactly as the analysis validates it on write. An id that is malformed, over
// the length bound, or the reserved unattributed sentinel is a bad request, not
// a group: there is no way to ask for "the unattributed remainder" as though it
// were a conversation.
func (s *server) conversationAPI(w http.ResponseWriter, r *http.Request) {
	if !getOnly(w, r) {
		return
	}
	id := r.URL.Query().Get("conversation")
	if !behaviour.ValidConversationID(id) {
		apiError(w, 400, fmt.Errorf("conversation must be a plain client-declared id (1-128 bytes of A-Za-z0-9._:-); the unattributed remainder is not a conversation"))
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
	calls := libraryCalls(idx)
	// One grouping per request: the library is read once, grouped once, and the
	// requested conversation and its page are sliced from that result. Grouping
	// twice to answer one question was measurable on a 20 000-call library.
	group := behaviour.FindConversation(behaviour.GroupConversations(calls), id)
	if group == nil {
		// The library can move under a bookmarked link: a recording may be
		// removed, renamed or filtered out. That is a missing recording, and the
		// answer says so rather than inventing an empty conversation.
		writeJSON(w, map[string]any{
			"conversation": id,
			"found":        false,
			"reason":       "no analysed recording in this view declares this conversation id",
			"library":      librarySummary(idx),
		})
		return
	}
	offset, limit, err := conversationPage(r)
	if err != nil {
		apiError(w, 400, err)
		return
	}
	all := behaviour.ConversationCalls(calls, id)
	page := pageOf(all, offset, limit)
	entries := make([]behaviour.TimelineEntry, 0, len(page))
	for _, c := range page {
		entries = append(entries, behaviour.TimelineEntryFor(c))
	}
	writeJSON(w, map[string]any{
		"conversation": id,
		"found":        true,
		"summary":      group,
		"timeline":     entries,
		"total":        len(all),
		"offset":       offset,
		"limit":        limit,
		"has_more":     offset+len(entries) < len(all),
		"library":      librarySummary(idx),
		"order":        "library order: recordings in the order this view analysed them, then sequence within each recording",
		"evidence":     conversationEvidence(group, idx),
	})
}

// compareConversationsAPI compares two conversations on counted facts only.
// Refusals (the unattributed remainder, the same id twice, a missing
// conversation) are answers with comparable:false, not 400s: the request is
// well formed and the honest response is that the evidence cannot support it.
func (s *server) compareConversationsAPI(w http.ResponseWriter, r *http.Request) {
	if !getOnly(w, r) {
		return
	}
	a := r.URL.Query().Get("a")
	b := r.URL.Query().Get("b")
	for _, id := range []string{a, b} {
		if !behaviour.ValidConversationID(id) {
			apiError(w, 400, fmt.Errorf("a and b must be plain client-declared conversation ids"))
			return
		}
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
	cmp := behaviour.CompareConversations(behaviour.GroupConversations(libraryCalls(idx)), a, b)
	writeJSON(w, map[string]any{
		"comparison": cmp,
		"library":    librarySummary(idx),
		"scope":      "Counted recorded boundary activity only. Differences describe what each recording contains, not how well either conversation went.",
	})
}

// conversationEvidence states the evidence-quality facts about one conversation
// so the view does not have to infer them from counts.
//
// The recording-level facts belong here too. Opening a conversation showed its
// counts and its timeline with no warning even when the recording holding it
// failed its hash-chain check or had no session/end, because the overview's
// warnings were not carried into this answer (M14 review F5). A conversation is
// not a smaller evidence class than the library: it is the same evidence,
// sliced.
func conversationEvidence(g *behaviour.ConversationSummary, idx *behaviourIndex) []string {
	var out []string
	for _, a := range idx.Sessions {
		if !contains(g.Files, a.Info.File) {
			continue
		}
		if a.Info.Integrity == "invalid" {
			out = append(out, "This conversation appears in "+safeFileText(a.Info.File)+", which failed its hash-chain check. Its activity is included, but treat it as untrusted evidence.")
		}
		if !a.Info.Complete {
			out = append(out, "This conversation appears in "+safeFileText(a.Info.File)+", which has no session/end event. A missing end can mean an interruption, a live session or a failed startup; it does not establish a failure.")
		}
		if a.UnpairedResults > 0 {
			out = append(out, fmt.Sprintf("%s holds %s that no recorded call claims, so this conversation may be missing evidence it cannot show.", safeFileText(a.Info.File), countOf(a.UnpairedResults, "recorded result", "recorded results")))
		}
		if a.UnknownSourceDecisions > 0 {
			out = append(out, fmt.Sprintf("%s records %s made while a value's source was unknown. Provenance uncertainty is sticky for the rest of the session, so a decision can rest on activity this conversation does not contain; it means uncertain provenance, not contamination.", safeFileText(a.Info.File), countOf(a.UnknownSourceDecisions, "policy decision", "policy decisions")))
		}
	}
	if g.AmbiguousCalls > 0 {
		out = append(out, "Some calls share a request id with another outstanding call, so which response answers which is unknown. Those counts are unknown, not zero.")
	}
	if g.Unanswered > 0 {
		out = append(out, "Some calls have no recorded result. A denial carries none by design; anything else is a gap in the recording. Neither is a success or a failure.")
	}
	if len(g.Files) > 1 {
		out = append(out, "This id appears in more than one recording. Attribution is client-declared, so the same label in two files may be one conversation or two clients that chose the same name; the recording cannot tell them apart.")
	}
	if g.TimelineTruncated {
		out = append(out, "The overview's timeline for this conversation is truncated; open the conversation to page through every recorded call.")
	}
	return out
}

// countOf renders a counted noun without the "1 calls" mistake.
func countOf(n int, singular, plural string) string {
	if n == 1 {
		return "1 " + singular
	}
	return strconv.Itoa(n) + " " + plural
}

// safeFileText keeps a recording name printable: filenames come from the library
// and a hostile one must not carry control characters into a terminal or a page.
func safeFileText(name string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return '�'
		}
		return r
	}, name)
}

// librarySummary is the coverage block every conversation answer carries, so a
// reader can see what was analysed rather than assuming the library is whole.
func librarySummary(idx *behaviourIndex) map[string]any {
	return map[string]any{
		"files":     idx.Files,
		"analysed":  idx.Analysed,
		"matched":   idx.Matched,
		"capped":    idx.Capped,
		"truncated": idx.Truncated,
	}
}

func conversationPage(r *http.Request) (offset, limit int, err error) {
	limit = conversationPageDefault
	q := r.URL.Query()
	if raw := q.Get("limit"); raw != "" {
		n, convErr := strconv.Atoi(raw)
		if convErr != nil || n < 1 {
			return 0, 0, fmt.Errorf("limit must be a positive integer")
		}
		if n > conversationPageMax {
			n = conversationPageMax
		}
		limit = n
	}
	if raw := q.Get("offset"); raw != "" {
		n, convErr := strconv.Atoi(raw)
		if convErr != nil || n < 0 {
			return 0, 0, fmt.Errorf("offset must be zero or a positive integer")
		}
		offset = n
	}
	return offset, limit, nil
}

func pageOf(calls []behaviour.CallRecord, offset, limit int) []behaviour.CallRecord {
	if offset >= len(calls) {
		return nil
	}
	end := offset + limit
	if end > len(calls) {
		end = len(calls)
	}
	return calls[offset:end]
}
