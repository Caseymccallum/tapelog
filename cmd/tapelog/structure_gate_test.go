package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Caseymccallum/tapelog/internal/session"
)

// sgWrite writes a correctly hash-chained log with seq = position+1.
func sgWrite(t *testing.T, path string, evs ...session.Event) {
	t.Helper()
	for i := range evs {
		evs[i].Seq = uint64(i + 1)
	}
	sgWriteRaw(t, path, evs...)
}

// sgWriteRaw chains events with the Seq values already set on them.
func sgWriteRaw(t *testing.T, path string, evs ...session.Event) {
	t.Helper()
	prev := ""
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for i := range evs {
		e := evs[i]
		e.Version = session.SchemaVersion
		e.PrevHash = prev
		h, err := e.ComputeHash()
		if err != nil {
			t.Fatal(err)
		}
		e.Hash = h
		prev = h
		raw, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write(append(raw, '\n')); err != nil {
			t.Fatal(err)
		}
	}
}

func sgStart(sid string) session.Event {
	return session.Event{Type: session.EventSessionStart, SessionID: sid,
		Payload: json.RawMessage(`{"harness":"t","policy_id":"observe"}`)}
}

func sgEnd(sid string) session.Event {
	return session.Event{Type: session.EventSessionEnd, SessionID: sid,
		Payload: json.RawMessage(`{"reason":"client disconnect"}`)}
}

func sgCall(sid string) session.Event {
	return session.Event{Type: session.EventToolCall, SessionID: sid,
		Payload: json.RawMessage(`{"id":1,"tool":"read_file","args":{}}`)}
}

func sgFixtures(t *testing.T) (valid, mixed, open, tampered string) {
	t.Helper()
	dir := t.TempDir()
	valid = filepath.Join(dir, "valid.jsonl")
	sgWrite(t, valid, sgStart("s"), sgCall("s"), sgEnd("s"))

	mixed = filepath.Join(dir, "mixed.jsonl")
	sgWrite(t, mixed, sgStart("a"), sgCall("a"), sgStart("b"), sgEnd("b"))

	open = filepath.Join(dir, "open.jsonl")
	sgWrite(t, open, sgStart("s"), sgCall("s"))

	tampered = filepath.Join(dir, "tampered.jsonl")
	data, err := os.ReadFile(valid)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tampered, bytes.Replace(data, []byte(`"read_file"`), []byte(`"read_filf"`), 1), 0o600); err != nil {
		t.Fatal(err)
	}
	return valid, mixed, open, tampered
}

// TestInspectCheckExitCodes pins the inspect --check verdict mapping: an
// unparseable file is a documented exit 1, but for a parseable file the exit
// code must name the failed property (3 integrity, 4 structure invalid,
// 5 incomplete).
func TestInspectCheckExitCodes(t *testing.T) {
	valid, mixed, open, tampered := sgFixtures(t)
	tests := []struct {
		name string
		path string
		want error
	}{
		{"valid file exits 0", valid, nil},
		{"mixed identity exits 4", mixed, errStructureFailed},
		{"open recording exits 5", open, errIncompleteStructure},
		{"broken chain exits 3", tampered, errIntegrityFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newInspectCmd()
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			cmd.SetArgs([]string{"--check", "--plain", tt.path})
			err := cmd.Execute()
			if tt.want == nil {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
		})
	}
}

// TestVerifyRequireStructureExitCode pins the verify --require-structure gate:
// a chain-intact file that is not one complete valid session exits 3.
func TestVerifyRequireStructureExitCode(t *testing.T) {
	valid, mixed, open, _ := sgFixtures(t)
	tests := []struct {
		name string
		path string
		want error
	}{
		{"valid file exits 0", valid, nil},
		{"mixed identity exits 3", mixed, errStructureRequired},
		{"open recording exits 3", open, errStructureRequired},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newVerifyCmd()
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			cmd.SetArgs([]string{"--require-structure", tt.path})
			err := cmd.Execute()
			if tt.want == nil {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
		})
	}
}

// TestVerifyRequireStructureCatchesSeqZeroIdentity is the F1/F3 regression at
// the CLI: an empty identity at seq 0 must exit 3, not 0.
func TestVerifyRequireStructureCatchesSeqZeroIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "seq0.jsonl")
	sgWriteRaw(t, path,
		session.Event{Type: session.EventSessionStart, SessionID: "", Seq: 0,
			Payload: json.RawMessage(`{"harness":"t","policy_id":"observe"}`)},
		session.Event{Type: session.EventToolCall, SessionID: "sess-1", Seq: 1,
			Payload: json.RawMessage(`{"id":1,"tool":"read_file","args":{}}`)},
		session.Event{Type: session.EventSessionEnd, SessionID: "sess-1", Seq: 2,
			Payload: json.RawMessage(`{"reason":"client disconnect"}`)},
	)
	cmd := newVerifyCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--require-structure", path})
	err := cmd.Execute()
	if !errors.Is(err, errChainFailed) {
		t.Fatalf("a seq-0 event is a chain defect; the chain gate must fire first, got err=%v", err)
	}
}
