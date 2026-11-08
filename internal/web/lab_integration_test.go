package web

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Caseymccallum/tapelog/internal/lab"
)

func TestLabCasePreviewUsesVerifiedEvidenceWithoutCopyingPayload(t *testing.T) {
	dir := t.TempDir()
	path := writeWorkbenchFixture(t, dir, "a.jsonl", "deny", "ok", time.Now())
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	rec := decodeRecording(original, "a.jsonl")
	h := DirHandler(dir, "")
	req := map[string]any{"file": "a.jsonl", "revision": rec.Info.Revision, "seq": 6, "kind": "denied", "fixture": "a.jsonl", "run": true, "lab": true, "title": "Recorded denial"}
	rw, body := requestJSON(t, h, "POST", "/api/test", req)
	if rw.Code != 200 {
		t.Fatal(rw.Body.String())
	}
	var generated string
	if err := json.Unmarshal(body["case_json"], &generated); err != nil {
		t.Fatal(err)
	}
	c, err := lab.ParseCase([]byte(generated))
	if err != nil {
		t.Fatal(err)
	}
	if c.EvidenceSHA256 != rec.Info.Revision {
		t.Fatal("case does not preserve evidence revision")
	}
	if strings.Contains(generated, "FICTIONAL-SENSITIVE-ARG") || strings.Contains(generated, "Fictional policy explanation") {
		t.Fatal("case copied recording payload")
	}
	var result lab.Result
	if err := json.Unmarshal(body["result"], &result); err != nil {
		t.Fatal(err)
	}
	if result.Status != "PASS" {
		t.Fatalf("unexpected result: %+v", result)
	}
	req["revision"] = "stale"
	rw, _ = requestJSON(t, h, "POST", "/api/test", req)
	if rw.Code != 409 {
		t.Fatalf("stale revision accepted: %d", rw.Code)
	}
	after, _ := os.ReadFile(path)
	if string(original) != string(after) {
		t.Fatal("recording changed")
	}
	files, _ := os.ReadDir(dir)
	if len(files) != 1 {
		t.Fatal("preview wrote a file")
	}
}
