package web

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Caseymccallum/tapelog/internal/lab"
)

func TestWorkspaceCaseModeAndEvidenceLinksSurviveLibraryAndSuite(t *testing.T) {
	dir := labFixture(t)
	c := lab.Case{Version: 1, Eligibility: "complete-boundary", Title: "Absent operation stays absent", Evidence: "evidence.jsonl", EvidenceSeqs: []uint64{2}, Expectations: []lab.Expectation{{Kind: "no_attempts", Tool: "absent__tool"}}}
	before, err := os.ReadFile(filepath.Join(dir, c.Evidence))
	if err != nil {
		t.Fatal(err)
	}
	h := DirHandler(dir, "")
	rw, saved := requestJSON(t, h, "POST", "/api/lab/case", map[string]any{"file": "mode.json", "case": c})
	if rw.Code != 200 {
		t.Fatal(rw.Body.String())
	}
	var entry labCaseEntry
	if err := json.Unmarshal(saved["case"], &entry); err != nil {
		t.Fatal(err)
	}
	if entry.Eligibility != c.Eligibility || len(entry.EvidenceSeqs) != 1 || entry.EvidenceSeqs[0] != 2 {
		t.Fatalf("case metadata lost: %+v", entry)
	}
	rw, data := requestJSON(t, h, "POST", "/api/lab/run", map[string]any{"file": "mode.json"})
	if rw.Code != 200 {
		t.Fatal(rw.Body.String())
	}
	var browser lab.Result
	if err := json.Unmarshal(data["result"], &browser); err != nil {
		t.Fatal(err)
	}
	offline, err := lab.RunCase(dir, c, "")
	if err != nil || browser.Status != "PASS" || browser.Status != offline.Status {
		t.Fatalf("web/offline mismatch: %+v %+v %v", browser, offline, err)
	}
	rw, _ = requestJSON(t, h, "POST", "/api/lab/suite", map[string]any{"file": "mode-suite.json", "suite": lab.Suite{Version: 1, Title: "Workspace", Cases: []string{"mode.json"}}})
	if rw.Code != 200 {
		t.Fatal(rw.Body.String())
	}
	rw, data = requestJSON(t, h, "POST", "/api/lab/suite/run", map[string]any{"file": "mode-suite.json"})
	if rw.Code != 200 {
		t.Fatal(rw.Body.String())
	}
	var files []string
	if err := json.Unmarshal(data["case_files"], &files); err != nil || len(files) != 1 || files[0] != "mode.json" {
		t.Fatal("suite cannot link its case outcome", files, err)
	}
	after, _ := os.ReadFile(filepath.Join(dir, c.Evidence))
	if string(before) != string(after) {
		t.Fatal("workspace mutated original evidence")
	}
	c.Eligibility = ""
	old, err := lab.RunCase(dir, c, "")
	if err != nil || old.Status != "INCONCLUSIVE" {
		t.Fatal("old case semantics changed", old, err)
	}
}
