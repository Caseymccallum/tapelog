package web

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Caseymccallum/tapelog/internal/session"
)

const maxRecordingBytes = 16 << 20
const maxRecordingEvents = 50000
const maxCatalogFiles = 200
const maxCatalogBytes = 32 << 20
const maxPreviewBytes = 32 << 10

// Counts describe MCP evidence only. Decisions are not completed-call counts.
type SessionInfo struct {
	File        string   `json:"file"`
	SessionID   string   `json:"session_id,omitempty"`
	Name        string   `json:"name"`
	Harness     string   `json:"harness,omitempty"`
	Servers     []string `json:"servers"`
	Events      int      `json:"events"`
	Calls       int      `json:"calls"`
	Answered    int      `json:"answered"`
	Allows      int      `json:"allows"`
	Denies      int      `json:"denies"`
	Confirms    int      `json:"confirms"`
	Failures    int      `json:"failures"`
	Discovery   int      `json:"discovery"`
	FirstTS     string   `json:"first_ts,omitempty"`
	LastTS      string   `json:"last_ts,omitempty"`
	DurationMS  *float64 `json:"duration_ms"`
	ChainOK     bool     `json:"chain_ok"`
	Integrity   string   `json:"integrity"`
	FirstBadSeq uint64   `json:"first_bad_seq,omitempty"`
	Problem     string   `json:"problem,omitempty"`
	Complete    bool     `json:"complete"`
	EndReason   string   `json:"end_reason,omitempty"`
	Revision    string   `json:"revision,omitempty"`
	Ambiguous   bool     `json:"ambiguous"`
	// Structure is "valid", "invalid" or "unreadable": whether the file is one
	// session log (one identity, one start first, any end last). It says
	// nothing about the hash chain, and Complete is derived from it.
	Structure  string   `json:"structure"`
	SessionIDs []string `json:"session_ids,omitempty"`
	// StructureProblems names the shared checker's own defect codes
	// ("no_events", "mixed_identity at seq 9", …). The verdict word alone cannot
	// tell an empty file from a two-session one, and the CLI text and API must
	// agree on which problem was found.
	StructureProblems []string `json:"structure_problems,omitempty"`
}

type activity struct {
	Seq          uint64   `json:"seq"`
	TS           string   `json:"ts"`
	Tool         string   `json:"tool"`
	Server       string   `json:"server"`
	ServerSource string   `json:"server_source"`
	Status       string   `json:"status"`
	Verdict      string   `json:"verdict"`
	Rule         string   `json:"rule"`
	Reason       string   `json:"reason"`
	DurationMS   *float64 `json:"duration_ms"`
	ResultSeq    uint64   `json:"result_seq,omitempty"`
	DecisionSeqs []uint64 `json:"decision_seqs"`
	ParentSeq    *uint64  `json:"parent_seq,omitempty"`
	Link         string   `json:"link"`
}

type recording struct {
	Info       SessionInfo
	Data       []byte
	Events     []session.Event
	Activities []*activity
}

func boundedRecording(f *os.File, name string, budget ...int64) (*recording, error) {
	readLimit := int64(maxRecordingBytes)
	if len(budget) > 0 && budget[0] < readLimit {
		readLimit = budget[0]
	}
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, fmt.Errorf("not a regular recording")
	}
	if st.Size() > maxRecordingBytes {
		return nil, fmt.Errorf("recording exceeds the 16 MiB workbench limit; use tapelog inspect/verify for this file")
	}
	if st.Size() > readLimit {
		return nil, fmt.Errorf("library scan budget reached; open this recording individually")
	}
	data, err := io.ReadAll(io.LimitReader(f, readLimit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > readLimit {
		return nil, fmt.Errorf("recording grew beyond the current workbench read limit; refresh or open it individually")
	}
	return decodeRecording(data, name), nil
}

func decodeRecording(data []byte, name string) *recording {
	sum := sha256.Sum256(data)
	rec := &recording{Data: data, Info: SessionInfo{File: name, Name: strings.TrimSuffix(name, ".jsonl"), Servers: []string{}, Integrity: "empty", Revision: hex.EncodeToString(sum[:])}}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 64*1024), maxRecordingBytes+1)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		if len(rec.Events) >= maxRecordingEvents {
			rec.Info.Integrity = "limited"
			rec.Info.Problem = "Recording exceeds the 50,000 event workbench limit; use the CLI."
			break
		}
		var e session.Event
		if err := json.Unmarshal(line, &e); err != nil {
			rec.Info.Problem = fmt.Sprintf("Invalid JSON after %d readable events; the file may be corrupt or have a partial final write.", len(rec.Events))
			break
		}
		rec.Events = append(rec.Events, e)
	}
	if rec.Info.Integrity != "limited" {
		result, err := session.Verify(bytes.NewReader(data))
		if err != nil {
			rec.Info.Integrity = "invalid"
			rec.Info.Problem = err.Error()
		} else if !result.OK() {
			rec.Info.Integrity = "invalid"
			rec.Info.FirstBadSeq = result.FirstBadSeq
			rec.Info.Problem = result.Problem
		} else if len(rec.Events) > 0 {
			rec.Info.Integrity = "verified"
			rec.Info.ChainOK = true
		}
	}
	// Structure is independent of the chain: a correctly hashed file can still
	// be two sessions, or have no start, or no end.
	structure, serr := session.InspectStructure(bytes.NewReader(data))
	if serr != nil {
		rec.Info.Structure = "unreadable"
	} else {
		rec.Info.SessionIDs = structure.SessionIDs
		for _, p := range structure.Problems {
			rec.Info.StructureProblems = append(rec.Info.StructureProblems, p.Describe())
		}
		rec.Info.Complete = structure.Complete()
		switch {
		case !structure.Valid():
			rec.Info.Structure = "invalid"
		case !structure.Complete():
			rec.Info.Structure = "incomplete"
		default:
			rec.Info.Structure = "valid"
		}
	}
	deriveRecording(rec)
	return rec
}

func duration(a, b string) *float64 {
	start, e1 := time.Parse(time.RFC3339Nano, a)
	end, e2 := time.Parse(time.RFC3339Nano, b)
	if e1 != nil || e2 != nil || end.Before(start) {
		return nil
	}
	ms := float64(end.Sub(start)) / float64(time.Millisecond)
	return &ms
}

func recordedTimeBefore(a, b string, newest bool) bool {
	ta, ea := time.Parse(time.RFC3339Nano, a)
	tb, eb := time.Parse(time.RFC3339Nano, b)
	if ea != nil {
		return false
	}
	if eb != nil {
		return true
	}
	if newest {
		return ta.After(tb)
	}
	return ta.Before(tb)
}

func idKey(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var b bytes.Buffer
	if json.Compact(&b, raw) != nil {
		return ""
	}
	return b.String()
}

func deriveRecording(rec *recording) {
	info := &rec.Info
	bySeq := map[uint64]*activity{}
	byID := map[string][]*activity{}
	servers := map[string]bool{}
	for _, e := range rec.Events {
		info.Events++
		if info.SessionID == "" {
			info.SessionID = e.SessionID
		}
		if e.TS != "" {
			if info.FirstTS == "" {
				info.FirstTS = e.TS
			}
			info.LastTS = e.TS
		}
		var p struct {
			ID         json.RawMessage `json:"id"`
			Tool       string          `json:"tool"`
			Server     string          `json:"server"`
			ServerName string          `json:"server_name"`
			Harness    string          `json:"harness"`
			Task       string          `json:"task"`
			Verdict    string          `json:"verdict"`
			Rule       string          `json:"rule_id"`
			Reason     string          `json:"reason"`
			IsError    bool            `json:"is_error"`
			Parent     *uint64         `json:"parent_seq"`
		}
		_ = json.Unmarshal(e.Payload, &p)
		switch e.Type {
		case session.EventSessionStart:
			info.Harness = p.Harness
			if p.Task != "" {
				info.Name = p.Task
			} else if p.Harness != "" {
				info.Name = p.Harness + " session"
			}
		case session.EventSessionEnd:
			info.EndReason = p.Reason
		case session.EventToolsList:
			info.Discovery++
		case session.EventToolCall:
			a := &activity{Seq: e.Seq, TS: e.TS, Tool: p.Tool, Server: "Not recorded", Status: "unanswered", DecisionSeqs: []uint64{}, ParentSeq: p.Parent, Link: "No related decision or result recorded"}
			if a.Tool == "" {
				a.Tool = "Unnamed tool"
				info.Ambiguous = true
			}
			if p.Server == "" {
				p.Server = p.ServerName
			}
			if p.Server != "" {
				a.Server = p.Server
				a.ServerSource = "Recorded server field"
			} else if i := strings.Index(p.Tool, "__"); i > 0 {
				a.Server = p.Tool[:i]
				a.ServerSource = "Recorded tool namespace"
			}
			if a.Server != "Not recorded" {
				servers[a.Server] = true
			}
			bySeq[e.Seq] = a
			key := idKey(p.ID)
			if key == "" {
				info.Ambiguous = true
			} else {
				byID[key] = append(byID[key], a)
				if len(byID[key]) > 1 {
					info.Ambiguous = true
				}
			}
			rec.Activities = append(rec.Activities, a)
			info.Calls++
		case session.EventPolicyDecision, session.EventToolResult:
			if e.Type == session.EventPolicyDecision {
				switch p.Verdict {
				case "allow":
					info.Allows++
				case "deny":
					info.Denies++
				case "confirm":
					info.Confirms++
				}
			}
			var a *activity
			link := "Recorded parent_seq"
			if p.Parent != nil {
				a = bySeq[*p.Parent]
			} else if candidates := byID[idKey(p.ID)]; len(candidates) == 1 {
				a = candidates[0]
				link = "Unique preceding request ID (legacy)"
			}
			if a == nil {
				info.Ambiguous = true
				continue
			}
			a.Link = link
			if e.Type == session.EventPolicyDecision {
				a.Verdict = p.Verdict
				a.Rule = p.Rule
				a.Reason = p.Reason
				a.DecisionSeqs = append(a.DecisionSeqs, e.Seq)
				if a.ResultSeq == 0 {
					switch p.Verdict {
					case "deny":
						a.Status = "denied"
					case "confirm":
						a.Status = "confirmation"
					}
				}
			} else {
				if a.ResultSeq != 0 {
					info.Ambiguous = true
					continue
				}
				a.ResultSeq = e.Seq
				a.Status = "success"
				info.Answered++
				if p.IsError {
					a.Status = "failed"
					info.Failures++
				}
				a.DurationMS = duration(a.TS, e.TS)
			}
		}
	}
	for name := range servers {
		info.Servers = append(info.Servers, name)
	}
	sort.Strings(info.Servers)
	info.DurationMS = duration(info.FirstTS, info.LastTS)
}

// os.Root also protects the open against a symlink swap after Lstat.
func (s *server) openRecording(name string) (*os.File, error) {
	if s.dir == "" {
		if s.logPath == "" {
			return nil, fmt.Errorf("no recording selected")
		}
		return os.Open(s.logPath)
	}
	if _, ok := safeJoin(s.dir, name); !ok {
		return nil, fmt.Errorf("invalid recording name")
	}
	root, err := os.OpenRoot(s.dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	st, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, fmt.Errorf("recordings must be regular files, not links")
	}
	return root.Open(name)
}

func (s *server) recording(name string, budget ...int64) (*recording, error) {
	f, err := s.openRecording(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if s.dir == "" {
		name = filepath.Base(s.logPath)
	}
	return boundedRecording(f, name, budget...)
}

func summarize(path string) SessionInfo {
	f, err := os.Open(path)
	if err != nil {
		return SessionInfo{File: filepath.Base(path), Integrity: "unavailable", Structure: "unreadable", Problem: "Recording cannot be opened"}
	}
	defer f.Close()
	rec, err := boundedRecording(f, filepath.Base(path))
	if err != nil {
		return SessionInfo{File: filepath.Base(path), Integrity: "limited", Structure: "unreadable", Problem: err.Error()}
	}
	return rec.Info
}

func listSessions(dir string) []SessionInfo { items, _, _ := catalog(&server{dir: dir}); return items }

// libraryFile is one candidate recording in a directory listing. Size and
// modification time identify the snapshot without reading the file, which is
// what the behaviour index uses to decide whether its cached analysis is
// still current.
type libraryFile struct {
	name     string
	size     int64
	modified time.Time
}

// listRecordings returns the newest *.jsonl files in dir (bounded by
// maxCatalogFiles) and whether the listing itself was truncated. Nothing is
// opened: callers apply their own read budget.
func listRecordings(dir string) ([]libraryFile, bool, error) {
	f, err := os.Open(dir)
	if err != nil {
		return nil, false, err
	}
	defer f.Close()
	entries, err := f.ReadDir(10000)
	if err != nil && err != io.EOF {
		return nil, false, err
	}
	var files []libraryFile
	for _, e := range entries {
		if !e.Type().IsRegular() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		st, err := e.Info()
		if err == nil && st.Mode().IsRegular() {
			files = append(files, libraryFile{e.Name(), st.Size(), st.ModTime()})
		}
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].modified.Equal(files[j].modified) {
			return files[i].name > files[j].name
		}
		return files[i].modified.After(files[j].modified)
	})
	capped := len(entries) == 10000 || len(files) > maxCatalogFiles
	if len(files) > maxCatalogFiles {
		files = files[:maxCatalogFiles]
	}
	return files, capped, nil
}

func catalog(s *server) ([]SessionInfo, bool, error) {
	if s.dir == "" {
		if s.logPath == "" {
			return []SessionInfo{}, false, nil
		}
		rec, err := s.recording("")
		if err != nil {
			return nil, false, err
		}
		return []SessionInfo{rec.Info}, false, nil
	}
	files, capped, err := listRecordings(s.dir)
	if err != nil {
		return nil, false, err
	}
	out := make([]SessionInfo, 0, len(files))
	budget := int64(maxCatalogBytes)
	for _, file := range files {
		if file.size > budget {
			out = append(out, SessionInfo{File: file.name, Name: strings.TrimSuffix(file.name, ".jsonl"), Integrity: "not_checked", Structure: "unreadable", Problem: "Library scan budget reached; open this session to inspect it."})
			continue
		}
		rec, err := s.recording(file.name, budget)
		if err != nil {
			out = append(out, SessionInfo{File: file.name, Name: file.name, Integrity: "unavailable", Structure: "unreadable", Problem: err.Error()})
		} else {
			budget -= int64(len(rec.Data))
			out = append(out, rec.Info)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return recordedTimeBefore(out[i].FirstTS, out[j].FirstTS, true) })
	return out, capped, nil
}
