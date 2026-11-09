package web

import (
	"testing"

	"github.com/Caseymccallum/tapelog/internal/behaviour"
)

// workflowSafe sorts arrays of objects by canonical JSON to make output
// deterministic. Producer-ordered arrays must be exempt: sorting conversation
// groups would rewrite "attributed first, unattributed last".
func TestConversationOrderSurvivesWorkflowSafe(t *testing.T) {
	type wrap struct {
		Conversations []behaviour.ConversationSummary `json:"conversations"`
	}
	in := wrap{Conversations: []behaviour.ConversationSummary{
		{ID: "conv-z", Attributed: true, FirstSeq: 4},
		{ID: "conv-a", Attributed: true, FirstSeq: 2},
		{ID: behaviour.UnattributedID, Attributed: false, FirstSeq: 9},
	}}
	v, err := workflowSafe(in)
	if err != nil {
		t.Fatal(err)
	}
	rows := v.(map[string]any)["conversations"].([]any)
	var got []string
	for _, r := range rows {
		got = append(got, r.(map[string]any)["id"].(string))
	}
	want := []string{"conv-z", "conv-a", behaviour.UnattributedID}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("producer order must survive workflowSafe: want %v, got %v", want, got)
		}
	}
}

// A timeline's meaning IS its order: canonical-JSON sorting the array rewrote
// the recording sequence the CLI printed.
func TestTimelineOrderSurvivesWorkflowSafe(t *testing.T) {
	conv := behaviour.ConversationSummary{
		ID: "c", Attributed: true,
		Timeline: []behaviour.TimelineEntry{
			{Seq: 9, Tool: "z_tool", Index: 4},
			{Seq: 2, Tool: "a_tool", Index: 0},
			{Seq: 6, Tool: "m_tool", Index: 2},
		},
	}
	type wrap struct {
		Conversations []behaviour.ConversationSummary `json:"conversations"`
	}
	v, err := workflowSafe(wrap{Conversations: []behaviour.ConversationSummary{conv}})
	if err != nil {
		t.Fatal(err)
	}
	g := v.(map[string]any)["conversations"].([]any)[0].(map[string]any)
	rows := g["timeline"].([]any)
	want := []float64{9, 2, 6}
	for i, w := range want {
		if got := rows[i].(map[string]any)["seq"].(float64); got != w {
			t.Fatalf("timeline order changed: want seq %v at %d, got %v", want, i, got)
		}
	}
	// index 0 must survive as a field, not be dropped as "empty".
	if _, ok := rows[1].(map[string]any)["index"]; !ok {
		t.Fatal("index 0 must be present, not omitted")
	}
}
