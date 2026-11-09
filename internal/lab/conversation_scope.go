package lab

// Conversation-scoped regression cases (M14).
//
// A case may name a client-declared conversation id. The scope means: judge this
// claim on the calls THAT conversation declared, and on nothing else. It exists
// because "the agent called delete_file" is a weaker statement than "the
// conversation that asked for a cleanup called delete_file", and because a
// library recording often carries several conversations.
//
// Three rules keep the scope from becoming a way to launder uncertainty:
//
//  1. A scope that no recorded call declares is not an empty scope, it is an
//     absent one: the claim is INCONCLUSIVE, never PASS. A missing conversation
//     cannot satisfy "there were no attempts".
//  2. Absence-style claims (no attempts, at most N attempts, never contains,
//     the deny-bypass invariant) need the WHOLE conversation to be observable.
//     Unattributed calls and ambiguous correlation elsewhere in the recording
//     could belong to this conversation, so those claims go INCONCLUSIVE with
//     conversation_scope_unknown rather than concluding from a partial view.
//  3. Existential claims (attempted, answered, denied, contains, sequence) are
//     decided by a witness inside the scope. Filtering cannot manufacture a
//     witness: an out-of-scope call is simply not there.
//
// The scope never touches recording integrity, structure or completeness: those
// gates run before this one and stay fatal.

import (
	"fmt"

	"github.com/Caseymccallum/tapelog/internal/behaviour"
	"github.com/Caseymccallum/tapelog/internal/replay"
)

const (
	// CodeConversationAbsent: no recorded call declares this conversation.
	CodeConversationAbsent = "conversation_absent"
	// CodeConversationScopeUnknown: the recording holds activity that cannot be
	// placed in or out of this conversation, so absence cannot be concluded.
	CodeConversationScopeUnknown = "conversation_scope_unknown"
	// CodeConversationScopeInvariant: a recording-level invariant cannot be
	// demonstrated from one conversation's slice of the evidence.
	CodeConversationScopeInvariant = "conversation_scope_invariant"
)

// absenceClaim reports whether one expectation asserts that something did NOT
// happen (or happened at most n times) and therefore needs a complete view.
func absenceClaim(kind string) bool {
	switch kind {
	case "no_attempts", "attempt_ceiling", "result_never_contains", "no_deny_bypassed":
		return true
	}
	return false
}

// scopedTape filters a recording's interactions down to one declared
// conversation. It returns the scope's tape, whether the scope exists at all,
// and whether the rest of the recording holds activity whose attribution is
// unknown (unattributed calls or calls whose result pairing is ambiguous) —
// activity that could belong to the scope and therefore blocks absence claims.
func scopedTape(t *replay.Tape, id string) (scoped *replay.Tape, found bool, unknownOutside int) {
	scoped = &replay.Tape{SessionID: t.SessionID, PolicyID: t.PolicyID, Tools: t.Tools}
	keep := func(items []replay.Interaction) []replay.Interaction {
		var out []replay.Interaction
		for _, it := range items {
			if it.ConversationID == id {
				found = true
				out = append(out, it)
				continue
			}
			// Activity with no declared id, an id the analysis layer REJECTS, or
			// a call whose result owner is unknown cannot be ruled out of the
			// scope. The rejected-id case is not pedantry: the analysis layer
			// treats such a call as unattributed (it cannot be shown to belong to
			// any conversation), so a scope must treat it the same way. Skipping
			// this check let a scoped absence claim PASS while a call with an
			// invalid id sat in the recording (M14 review F1).
			if it.ConversationID == "" || it.Ambiguous || !behaviour.ValidConversationID(it.ConversationID) {
				unknownOutside++
			}
		}
		return out
	}
	scoped.Interactions = keep(t.Interactions)
	scoped.Unanswered = keep(t.Unanswered)
	scoped.AmbiguousIDs = append([]string(nil), t.AmbiguousIDs...)
	return scoped, found, unknownOutside
}

// scopeCase applies a case's conversation scope to a tape and reports why a
// claim cannot be decided, if it cannot. An empty code means "decide it on the
// scoped tape".
func scopeCase(c Case, t *replay.Tape, kind string) (scoped *replay.Tape, code, reason string) {
	if c.Conversation == "" {
		return t, "", ""
	}
	scoped, found, unknownOutside := scopedTape(t, c.Conversation)
	if !found {
		return scoped, CodeConversationAbsent, "no call in this recording declares the conversation " + c.Conversation + ", so it cannot be the subject of a claim"
	}
	if kind == "no_deny_bypassed" {
		return scoped, CodeConversationScopeInvariant, "the deny-bypass invariant is a recording-level claim: one conversation's slice cannot demonstrate it"
	}
	if absenceClaim(kind) && unknownOutside > 0 {
		noun, declare, share, pronoun := fmt.Sprintf("%d recorded calls", unknownOutside), "declare", "share", "they"
		if unknownOutside == 1 {
			noun, declare, share, pronoun = "1 recorded call", "declares", "shares", "it"
		}
		return scoped, CodeConversationScopeUnknown, fmt.Sprintf("%s %s no conversation or %s a request id, so %s could belong to %s; absence cannot be concluded from a partial view", noun, declare, share, pronoun, c.Conversation)
	}
	return scoped, "", ""
}
