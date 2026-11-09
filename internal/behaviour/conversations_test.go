package behaviour

import (
	"fmt"
	"testing"
)

func rec(seq uint64, conv, tool, argsKey string, err bool) CallRecord {
	return CallRecord{
		Seq: seq, Tool: tool, ArgsKey: argsKey, Status: "ok",
		IsError: err, ResultSeq: seq + 1, ConversationID: conv,
	}
}

// The mission's example: four conversations each call read_file twice. That is
// two calls per conversation and eight overall — NOT an eight-call loop inside
// one conversation, and not one conversation with eight calls.
func TestFourConversationsTwoCallsEach(t *testing.T) {
	var calls []CallRecord
	seq := uint64(2)
	for c := 1; c <= 4; c++ {
		conv := fmt.Sprintf("conv-%d", c)
		for i := 0; i < 2; i++ {
			calls = append(calls, rec(seq, conv, "read_file", `{"path":"/x"}`, false))
			seq += 2
		}
	}

	groups := GroupConversations(calls)
	if len(groups) != 4 {
		t.Fatalf("want 4 conversations, got %d", len(groups))
	}
	for _, g := range groups {
		if !g.Attributed {
			t.Fatalf("group %q must be attributed", g.ID)
		}
		if g.Calls != 2 {
			t.Fatalf("conversation %q must have 2 calls, got %d (this is the loop-vs-recurrence confusion)", g.ID, g.Calls)
		}
	}

	rec := DetectRecurrence(calls)
	if len(rec) != 1 {
		t.Fatalf("want one recurring operation, got %+v", rec)
	}
	if rec[0].Conversations != 4 || rec[0].CallCount != 8 {
		t.Fatalf("recurrence must report 4 conversations / 8 calls, got %+v", rec[0])
	}
}

// Unattributed activity is a remainder, never a conversation: it is reported
// last, is not counted toward recurrence's conversation total, and is not
// merged into any attributed group.
func TestUnattributedIsSeparate(t *testing.T) {
	calls := []CallRecord{
		rec(2, "conv-a", "read_file", `{"path":"/x"}`, false),
		rec(4, "", "read_file", `{"path":"/x"}`, false),
		rec(6, "conv-b", "read_file", `{"path":"/x"}`, false),
	}
	groups := GroupConversations(calls)
	if len(groups) != 3 {
		t.Fatalf("want 3 groups (2 attributed + unattributed), got %d", len(groups))
	}
	last := groups[len(groups)-1]
	if last.Attributed || last.ID != UnattributedID {
		t.Fatalf("the unattributed group must come last and be labelled, got %+v", last)
	}
	if last.Calls != 1 {
		t.Fatalf("unattributed group must hold exactly the unattributed call, got %d", last.Calls)
	}
	for _, r := range DetectRecurrence(calls) {
		if r.Conversations != 2 {
			t.Fatalf("recurrence must count 2 attributed conversations, got %+v", r)
		}
		if r.Unattributed != 1 {
			t.Fatalf("the unattributed call must be counted separately, got %+v", r)
		}
	}
}

// A genuine loop inside ONE conversation is a repeat, and must not be dressed
// up as cross-conversation recurrence.
func TestWithinConversationLoopIsNotRecurrence(t *testing.T) {
	var calls []CallRecord
	for i := 0; i < 5; i++ {
		calls = append(calls, rec(uint64(2+i*2), "conv-a", "read_file", `{"path":"/x"}`, false))
	}
	groups := GroupConversations(calls)
	if len(groups) != 1 || groups[0].Calls != 5 {
		t.Fatalf("want one conversation with 5 calls, got %+v", groups)
	}
	if r := DetectRecurrence(calls); len(r) != 0 {
		t.Fatalf("a single-conversation loop is not recurrence, got %+v", r)
	}
}

// Ambiguous correlation is counted, not silently attributed.
func TestAmbiguousResultsAreCounted(t *testing.T) {
	c := rec(2, "conv-a", "read_file", `{"path":"/x"}`, false)
	c.CorrelationAmbiguous = true
	groups := GroupConversations([]CallRecord{c})
	if groups[0].AmbiguousCalls != 1 {
		t.Fatalf("ambiguous calls must be counted, got %+v", groups[0])
	}
}

// A call with no recorded result is neither answered nor a failure, and the
// group reports it as unanswered.
func TestUnansweredCallsAreCounted(t *testing.T) {
	c := rec(4, "conv-a", "read_file", `{}`, false)
	c.ResultSeq = 0
	groups := GroupConversations([]CallRecord{c})
	if groups[0].Unanswered != 1 || groups[0].Results != 0 || groups[0].Failures != 0 {
		t.Fatalf("an unanswered call is not a result and not a failure, got %+v", groups[0])
	}
}

// A group's position across recordings comes from the library, not from a
// per-recording sequence number that restarts in every file.
func TestConversationsOrderByLibraryPositionAcrossFiles(t *testing.T) {
	newer := rec(2, "new-high", "read_file", `{}`, false)
	newer.LibrarySeq = 40 // seen later in the library
	older := rec(600, "old-low", "read_file", `{}`, false)
	older.LibrarySeq = 3
	groups := GroupConversations([]CallRecord{newer, older})
	if len(groups) != 2 {
		t.Fatalf("want two groups, got %+v", groups)
	}
	// Per-recording seq would put old-low (600) after new-high (2); library
	// position says old-low appeared first.
	if groups[0].ID != "old-low" || groups[1].ID != "new-high" {
		t.Fatalf("groups must follow library order, got %s then %s", groups[0].ID, groups[1].ID)
	}
	if groups[0].FirstLibrarySeq != 3 || groups[1].FirstLibrarySeq != 40 {
		t.Fatalf("first library seq must be reported, got %+v", groups)
	}
}

// When no library position exists the grouping falls back to the recording's
// own sequence, and a group with no position never sorts ahead of one that has
// one.
func TestConversationsFallBackWhenLibraryPositionIsAbsent(t *testing.T) {
	positioned := rec(9, "positioned", "read_file", `{}`, false)
	positioned.LibrarySeq = 7
	unpositioned := rec(1, "unpositioned", "read_file", `{}`, false)
	groups := GroupConversations([]CallRecord{unpositioned, positioned})
	if groups[0].ID != "positioned" || groups[1].ID != "unpositioned" {
		t.Fatalf("an unpositioned group must sort last, got %s then %s", groups[0].ID, groups[1].ID)
	}
}
