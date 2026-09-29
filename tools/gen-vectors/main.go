// Command gen-vectors regenerates the conformance test vectors in
// spec/test-vectors/ from the reference implementation. Never edit the
// vectors by hand — run `go run ./tools/gen-vectors` instead.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cassette-ai/cassette/internal/session"
)

const vectorsDir = "spec/test-vectors"

// canonicalCase pins payload -> canonical form -> hash behavior.
type canonicalCase struct {
	Name          string        `json:"name"`
	Event         session.Event `json:"event"`
	CanonicalForm string        `json:"canonical_form"`
	Hash          string        `json:"hash"`
}

// sessionCase pins a whole-file verification expectation.
type sessionCase struct {
	Name        string `json:"name"`
	File        string `json:"file"`
	OK          bool   `json:"ok"`
	Events      int    `json:"events"`
	FirstBadSeq uint64 `json:"first_bad_seq,omitempty"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "gen-vectors:", err)
		os.Exit(1)
	}
}

func run() error {
	if err := os.MkdirAll(vectorsDir, 0o755); err != nil {
		return err
	}

	base := session.Event{
		Version:   0,
		Seq:       1,
		TS:        "2026-01-02T03:04:05.000Z",
		Type:      "tools/call",
		SessionID: "vector-session",
		PrevHash:  "",
	}

	cases := []struct {
		name    string
		payload string
		prev    string
	}{
		{"empty-payload", `{}`, ""},
		{"unsorted-keys", `{"b":1,"a":{"z":true,"y":null,"x":"v"}}`, ""},
		{"big-int-preserved", `{"n":12345678901234567890}`, ""},
		{"html-chars-unescaped", `{"s":"a<b>&c"}`, ""},
		{"unicode", `{"s":"héllo ☃ 🎧"}`, ""},
		{"arrays-and-nulls", `{"arr":[1,"two",{"k":null}],"nil":null}`, ""},
		{"prev-hash-linkage", `{"tool":"read_file"}`, "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"},
	}

	var vectors struct {
		Comment string          `json:"$comment"`
		Cases   []canonicalCase `json:"canonical_cases"`
		Session []sessionCase   `json:"session_cases"`
	}
	vectors.Comment = "Conformance vectors for the Agent Session Log Format v0. Regenerate: go run ./tools/gen-vectors"

	for _, c := range cases {
		e := base
		e.Payload = json.RawMessage(c.payload)
		e.PrevHash = c.prev
		form, err := e.CanonicalBytes()
		if err != nil {
			return fmt.Errorf("%s: %w", c.name, err)
		}
		hash, err := e.ComputeHash()
		if err != nil {
			return fmt.Errorf("%s: %w", c.name, err)
		}
		vectors.Cases = append(vectors.Cases, canonicalCase{
			Name: c.name, Event: e, CanonicalForm: string(form), Hash: hash,
		})
	}

	// Session-level vectors: an intact session plus tampered variants.
	intact := filepath.Join(vectorsDir, "session-intact.jsonl")
	if err := writeSession(intact); err != nil {
		return err
	}
	tampered := filepath.Join(vectorsDir, "session-tampered.jsonl")
	if err := derive(intact, tampered, tamperModify); err != nil {
		return err
	}
	deleted := filepath.Join(vectorsDir, "session-deleted.jsonl")
	if err := derive(intact, deleted, tamperDelete); err != nil {
		return err
	}

	for _, sc := range []struct{ name, file string }{
		{"intact", intact}, {"tampered-payload", tampered}, {"deleted-event", deleted},
	} {
		res, err := session.VerifyFile(sc.file)
		if err != nil {
			return err
		}
		vectors.Session = append(vectors.Session, sessionCase{
			Name: sc.name, File: filepath.Base(sc.file),
			OK: res.OK(), Events: res.Events, FirstBadSeq: res.FirstBadSeq,
		})
	}

	out, err := json.MarshalIndent(vectors, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(vectorsDir, "canonical.json"), append(out, '\n'), 0o644)
}

// writeSession writes a three-event reference session.
func writeSession(path string) error {
	w, err := session.NewWriter(path, "vector-session")
	if err != nil {
		return err
	}
	defer w.Close()
	w.SetClock(func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) })

	if _, err := w.Append(session.EventSessionStart, session.SessionStartPayload{Harness: "vector", PolicyID: "observe"}); err != nil {
		return err
	}
	if _, err := w.Append(session.EventToolCall, session.ToolCallPayload{
		ID: json.RawMessage(`1`), Tool: "read_file", Args: json.RawMessage(`{"path":"/tmp/a"}`),
	}); err != nil {
		return err
	}
	if _, err := w.Append(session.EventPolicyDecision, session.PolicyDecisionPayload{
		ID: json.RawMessage(`1`), Verdict: "allow", RuleID: "rule-1", Reason: "vector",
	}); err != nil {
		return err
	}
	return nil
}

type tamperKind int

const (
	tamperModify tamperKind = iota
	tamperDelete
)

// derive writes a tampered variant of the intact session.
func derive(src, dst string, kind tamperKind) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	switch kind {
	case tamperModify:
		lines[1] = strings.Replace(lines[1], "/tmp/a", "/etc/shadow", 1)
	case tamperDelete:
		lines = append(lines[:1], lines[2:]...)
	}
	return os.WriteFile(dst, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}

