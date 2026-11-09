package lab

import (
	"testing"

	"github.com/Caseymccallum/tapelog/internal/replay"
)

// A denied claim needs a decision that belongs to the call it is about. When the
// request id was shared and the decision could not be tied to the call (no
// parent_seq), the lab must say so rather than read the absence as "this tool
// was not denied" — that reading is what produced a false PASS in review.
func TestDeniedClaimRefusesUnattributableDecision(t *testing.T) {
	tp := &replay.Tape{
		SessionID:    "s",
		AmbiguousIDs: []string{"1"},
		Unanswered: []replay.Interaction{
			{Order: 1, RequestID: "1", Tool: "read_file", Args: []byte(`{"path":"/a"}`), Verdict: "", Ambiguous: true},
			{Order: 2, RequestID: "1", Tool: "read_file", Args: []byte(`{"path":"/b"}`), Verdict: "", Ambiguous: true},
		},
	}
	code, reason := eligibility(Expectation{Kind: "denied", Tool: "read_file"}, tp)
	if code != CodeAmbiguous {
		t.Fatalf("want %q, got %q (%s)", CodeAmbiguous, code, reason)
	}
}

// The same recording with attributable verdicts stays decidable: ambiguity in
// the result pairing does not poison a decision that names its own call.
func TestDeniedClaimDecidesWhenDecisionsAreAttributable(t *testing.T) {
	tp := &replay.Tape{
		SessionID:    "s",
		AmbiguousIDs: []string{"1"},
		Unanswered: []replay.Interaction{
			{Order: 1, RequestID: "1", Tool: "read_file", Args: []byte(`{"path":"/a"}`), Verdict: "allow", Ambiguous: true},
		},
	}
	code, reason := eligibility(Expectation{Kind: "denied", Tool: "read_file"}, tp)
	if code != "" {
		t.Fatalf("want eligible, got %q (%s)", code, reason)
	}
}
