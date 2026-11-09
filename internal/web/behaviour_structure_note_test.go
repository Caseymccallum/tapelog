package web

import (
	"strings"
	"testing"
)

// A structurally invalid recording is still aggregated into the behaviour
// totals. The browser must say so, or a mixed-identity or start-less file looks
// like ordinary evidence beside the numbers.
func TestBehaviourNotesNameStructurallyInvalidEvidence(t *testing.T) {
	invalid := &behaviourIndex{
		Analysed: 1, Matched: 1,
		Sessions: []*sessionAnalysis{{Info: SessionInfo{Structure: "invalid", Integrity: "verified", Complete: true}}},
	}
	ov := &behaviourOverview{}
	joined := strings.Join(behaviourNotes(invalid, ov), " ")
	if !strings.Contains(joined, "structurally invalid") {
		t.Fatalf("notes must name the structurally invalid recording: %q", joined)
	}

	valid := &behaviourIndex{
		Analysed: 1, Matched: 1,
		Sessions: []*sessionAnalysis{{Info: SessionInfo{Structure: "valid", Integrity: "verified", Complete: true}}},
	}
	if joined := strings.Join(behaviourNotes(valid, ov), " "); strings.Contains(joined, "structure is invalid") {
		t.Fatalf("valid evidence must not be warned about: %q", joined)
	}
}
