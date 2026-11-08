package scenario

import (
	"encoding/json"
	"github.com/Caseymccallum/tapelog/internal/replay"
	"testing"
)

func TestM7ResultAbsenceEvidenceLimits(t *testing.T) {
	assertion := &Scenario{Version: 1, Assert: []Check{{ResultNeverContains: &ResultSpec{Tool: "db", Text: "permission denied"}}}}
	cases := []struct {
		name  string
		tape  *replay.Tape
		fails bool
	}{
		{"no matching call", &replay.Tape{}, false},
		{"unanswered only", &replay.Tape{Unanswered: []replay.Interaction{{Tool: "db"}}}, false},
		{"redacted recorded result", &replay.Tape{Interactions: []replay.Interaction{{Tool: "db", Result: json.RawMessage(`{"message":"[REDACTED]"}`)}}}, false},
		{"MCP error", &replay.Tape{Interactions: []replay.Interaction{{Tool: "db", IsError: true, Result: json.RawMessage(`{"message":"permission denied"}`)}}}, true},
		{"second matching call", &replay.Tape{Interactions: []replay.Interaction{{Tool: "db", Result: json.RawMessage(`"ok"`)}, {Tool: "db", Result: json.RawMessage(`"permission denied"`)}}}, true},
		{"changed case", &replay.Tape{Interactions: []replay.Interaction{{Tool: "db", Result: json.RawMessage(`"Permission Denied"`)}}}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := len(Run(assertion, c.tape, nil)) > 0; got != c.fails {
				t.Fatalf("got failure %v", got)
			}
		})
	}
}
