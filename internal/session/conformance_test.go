package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// vectorsDir is the spec's conformance vector directory relative to this
// package. The spec and the reference implementation must not drift:
// these tests fail if canonical-form or verification behavior changes
// without regenerating the vectors (and bumping the spec).
const vectorsDir = "../../spec/test-vectors"

type canonicalVectors struct {
	Comment string `json:"$comment"`
	Cases   []struct {
		Name          string `json:"name"`
		Event         Event  `json:"event"`
		CanonicalForm string `json:"canonical_form"`
		Hash          string `json:"hash"`
	} `json:"canonical_cases"`
	Session []struct {
		Name        string `json:"name"`
		File        string `json:"file"`
		OK          bool   `json:"ok"`
		Events      int    `json:"events"`
		FirstBadSeq uint64 `json:"first_bad_seq"`
	} `json:"session_cases"`
}

func loadVectors(t *testing.T) canonicalVectors {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(vectorsDir, "canonical.json"))
	if err != nil {
		t.Fatalf("conformance vectors missing (run `go run ./tools/gen-vectors`): %v", err)
	}
	var v canonicalVectors
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatal(err)
	}
	if len(v.Cases) == 0 || len(v.Session) == 0 {
		t.Fatal("empty conformance vectors")
	}
	return v
}

func TestConformanceCanonicalForm(t *testing.T) {
	vectors := loadVectors(t)
	for _, c := range vectors.Cases {
		form, err := c.Event.CanonicalBytes()
		if err != nil {
			t.Errorf("%s: CanonicalBytes: %v", c.Name, err)
			continue
		}
		if string(form) != c.CanonicalForm {
			t.Errorf("%s: canonical form drift\n got: %q\nwant: %q", c.Name, form, c.CanonicalForm)
		}
		hash, err := c.Event.ComputeHash()
		if err != nil {
			t.Errorf("%s: ComputeHash: %v", c.Name, err)
			continue
		}
		if hash != c.Hash {
			t.Errorf("%s: hash drift\n got: %s\nwant: %s", c.Name, hash, c.Hash)
		}
	}
}

func TestConformanceVerification(t *testing.T) {
	vectors := loadVectors(t)
	for _, c := range vectors.Session {
		path := filepath.Join(vectorsDir, c.File)
		res, err := VerifyFile(path)
		if err != nil {
			t.Errorf("%s: VerifyFile: %v", c.Name, err)
			continue
		}
		if res.OK() != c.OK || res.Events != c.Events || res.FirstBadSeq != c.FirstBadSeq {
			t.Errorf("%s: verification drift\n got: ok=%v events=%d first_bad=%d\nwant: ok=%v events=%d first_bad=%d",
				c.Name, res.OK(), res.Events, res.FirstBadSeq, c.OK, c.Events, c.FirstBadSeq)
		}
	}
}
