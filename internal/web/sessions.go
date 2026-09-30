package web

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/Caseymccallum/tapelog/internal/session"
)

// SessionInfo summarizes one session log in a directory listing.
type SessionInfo struct {
	File        string `json:"file"`
	SessionID   string `json:"session_id,omitempty"`
	Events      int    `json:"events"`
	Denies      int    `json:"denies"`
	FirstTS     string `json:"first_ts,omitempty"`
	LastTS      string `json:"last_ts,omitempty"`
	ChainOK     bool   `json:"chain_ok"`
	FirstBadSeq uint64 `json:"first_bad_seq,omitempty"` // 0 = intact
	Problem     string `json:"problem,omitempty"`
}

// listSessions summarizes up to 200 *.jsonl logs in dir (newest name
// first — session ids are timestamped). Empty dir field yields nil.
func listSessions(dir string) []SessionInfo {
	if dir == "" {
		return nil
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if err != nil {
		return nil
	}
	sortStrings(files)
	if len(files) > 200 {
		files = files[len(files)-200:]
	}
	out := make([]SessionInfo, 0, len(files))
	for i := len(files) - 1; i >= 0; i-- {
		out = append(out, summarize(files[i]))
	}
	return out
}

// summarize scans one log for its header and counts, and verifies its
// hash chain — tampered logs must be visible, not silently rendered.
func summarize(path string) SessionInfo {
	info := SessionInfo{File: filepath.Base(path)}
	if res, err := session.VerifyFile(path); err == nil {
		info.ChainOK = res.OK()
		info.FirstBadSeq = res.FirstBadSeq
		info.Problem = res.Problem
	} else {
		info.Problem = err.Error()
	}
	f, err := os.Open(path)
	if err != nil {
		return info
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		var e struct {
			TS    string `json:"ts"`
			Type  string `json:"type"`
			PID   string `json:"session_id"`
			Pload struct {
				Verdict string `json:"verdict"`
			} `json:"payload"`
		}
		if json.Unmarshal(sc.Bytes(), &e) != nil {
			continue
		}
		info.Events++
		if e.PID != "" && info.SessionID == "" {
			info.SessionID = e.PID
		}
		if e.TS != "" {
			if info.FirstTS == "" {
				info.FirstTS = e.TS
			}
			info.LastTS = e.TS
		}
		if e.Type == "policy/decision" && e.Pload.Verdict == "deny" {
			info.Denies++
		}
	}
	return info
}

// sortStrings sorts ascending (small-n insertion sort).
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
