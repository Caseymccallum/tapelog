package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/Caseymccallum/tapelog/internal/replay"
	"github.com/Caseymccallum/tapelog/internal/scenario"
	"github.com/Caseymccallum/tapelog/internal/session"
	"gopkg.in/yaml.v3"
)

func apiError(w http.ResponseWriter, code int, err error) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}
func getOnly(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodGet {
		apiError(w, 405, fmt.Errorf("GET only"))
		return false
	}
	return true
}

// postOnly keeps the Studio's write-free endpoints honest about their verbs.
func postOnly(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodPost {
		apiError(w, 405, fmt.Errorf("POST only"))
		return false
	}
	return true
}
func pageBounds(r *http.Request, total int) (int, int) {
	offset := queryInt(r, "offset")
	limit := queryInt(r, "limit")
	if limit == 0 || limit > 100 {
		limit = 100
	}
	if offset > total {
		offset = total
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return offset, end
}

func (s *server) libraryAPI(w http.ResponseWriter, r *http.Request) {
	if !getOnly(w, r) {
		return
	}
	items, capped, err := catalog(s)
	if err != nil {
		apiError(w, 400, err)
		return
	}
	q := strings.ToLower(r.URL.Query().Get("q"))
	status := r.URL.Query().Get("status")
	filtered := make([]SessionInfo, 0, len(items))
	for _, item := range items {
		if q != "" && !strings.Contains(strings.ToLower(item.Name+" "+item.File+" "+item.SessionID+" "+item.Harness+" "+strings.Join(item.Servers, " ")), q) {
			continue
		}
		if status == "denied" && item.Denies == 0 || status == "failed" && item.Failures == 0 || status == "incomplete" && item.Complete || status == "integrity" && item.Integrity == "verified" {
			continue
		}
		filtered = append(filtered, item)
	}
	switch r.URL.Query().Get("sort") {
	case "activity":
		sort.SliceStable(filtered, func(i, j int) bool { return filtered[i].Calls > filtered[j].Calls })
	case "status":
		sort.SliceStable(filtered, func(i, j int) bool {
			return filtered[i].Denies+filtered[i].Failures > filtered[j].Denies+filtered[j].Failures
		})
	case "oldest":
		sort.SliceStable(filtered, func(i, j int) bool { return recordedTimeBefore(filtered[i].FirstTS, filtered[j].FirstTS, false) })
	}
	start, end := pageBounds(r, len(filtered))
	directory := s.dir
	if directory == "" && s.logPath != "" {
		directory = filepath.Dir(s.logPath)
	}
	if directory != "" {
		if absolute, err := filepath.Abs(directory); err == nil {
			directory = absolute
		}
	}
	writeJSON(w, map[string]any{"sessions": filtered[start:end], "total": len(filtered), "catalog_total": len(items), "capped": capped, "directory": directory, "queue_enabled": s.queue != nil, "single_session": s.dir == "", "limit_bytes": maxRecordingBytes, "limit_events": maxRecordingEvents})
}

func (s *server) activitiesAPI(w http.ResponseWriter, r *http.Request) {
	if !getOnly(w, r) {
		return
	}
	rec, err := s.recording(r.URL.Query().Get("file"))
	if err != nil {
		apiError(w, 400, err)
		return
	}
	q := strings.ToLower(r.URL.Query().Get("q"))
	status := r.URL.Query().Get("status")
	server := r.URL.Query().Get("server")
	items := make([]*activity, 0)
	for _, a := range rec.Activities {
		if q != "" && !strings.Contains(strings.ToLower(a.Tool+" "+a.Rule+" "+a.Reason), q) {
			continue
		}
		if status != "" && a.Status != status && a.Verdict != status && !(status == "denied" && a.Verdict == "deny") && !(status == "confirmation" && a.Verdict == "confirm") {
			continue
		}
		if server != "" && a.Server != server {
			continue
		}
		items = append(items, a)
	}
	start, end := pageBounds(r, len(items))
	findings := []map[string]any{}
	var first *activity
	for i, a := range rec.Activities {
		if a.Status == "failed" && (first == nil || a.ResultSeq < first.ResultSeq) {
			first = a
		}
		if a.Verdict == "deny" && len(findings) < 5 {
			findings = append(findings, map[string]any{"seq": a.Seq, "index": i, "tool": shortText(a.Tool, 512), "label": "Recorded deny decision"})
		}
	}
	if first != nil {
		for i, a := range rec.Activities {
			if a == first {
				findings = append([]map[string]any{{"seq": a.Seq, "index": i, "tool": shortText(a.Tool, 512), "label": fmt.Sprintf("First recorded error result #%d", a.ResultSeq)}}, findings...)
				break
			}
		}
	}
	visible := make([]*activity, 0, end-start)
	for _, a := range items[start:end] {
		visible = append(visible, safeActivity(a))
	}
	writeJSON(w, map[string]any{"session": rec.Info, "activities": visible, "total": len(items), "offset": start, "findings": findings})
}

type eventView struct {
	Seq       uint64            `json:"seq"`
	TS        string            `json:"ts"`
	Type      session.EventType `json:"type"`
	Raw       string            `json:"raw"`
	Truncated bool              `json:"truncated"`
}

func viewEvent(e session.Event) eventView {
	data, _ := json.MarshalIndent(e, "", "  ")
	view := eventView{Seq: e.Seq, TS: e.TS, Type: e.Type, Raw: string(data)}
	if len(data) > maxPreviewBytes {
		view.Raw = strings.ToValidUTF8(string(data[:maxPreviewBytes]), "�") + "\n… preview limited to 32 KiB; inspect the original locally."
		view.Truncated = true
	}
	return view
}

func (s *server) eventsAPI(w http.ResponseWriter, r *http.Request) {
	if !getOnly(w, r) {
		return
	}
	rec, err := s.recording(r.URL.Query().Get("file"))
	if err != nil {
		apiError(w, 400, err)
		return
	}
	start, end := pageBounds(r, len(rec.Events))
	views := []eventView{}
	for _, e := range rec.Events[start:end] {
		if r.URL.Query().Get("summary") == "1" {
			views = append(views, eventView{Seq: e.Seq, TS: e.TS, Type: e.Type})
		} else {
			views = append(views, viewEvent(e))
		}
	}
	writeJSON(w, map[string]any{"events": views, "total": len(rec.Events), "session": rec.Info})
}

func (s *server) eventAPI(w http.ResponseWriter, r *http.Request) {
	if !getOnly(w, r) {
		return
	}
	rec, err := s.recording(r.URL.Query().Get("file"))
	if err != nil {
		apiError(w, 400, err)
		return
	}
	seq, _ := strconv.ParseUint(r.URL.Query().Get("seq"), 10, 64)
	var a *activity
	for _, item := range rec.Activities {
		if item.Seq == seq {
			a = item
			break
		}
	}
	wanted := map[uint64]bool{seq: true}
	if a != nil {
		wanted[a.ResultSeq] = true
		for _, n := range a.DecisionSeqs {
			wanted[n] = true
		}
	}
	related := []eventView{}
	evidence := map[string]any{"decisions": []map[string]any{}}
	decisions := []map[string]any{}
	bounded := func(raw json.RawMessage) string {
		if len(raw) > 4096 {
			return strings.ToValidUTF8(string(raw[:4096]), "�") + "… [preview limited]"
		}
		return string(raw)
	}
	precedingEvents := []session.Event{}
	for _, e := range rec.Events {
		if wanted[e.Seq] && len(related) < 20 {
			related = append(related, viewEvent(e))
			switch e.Type {
			case session.EventToolCall:
				var p session.ToolCallPayload
				if json.Unmarshal(e.Payload, &p) == nil {
					evidence["input"] = bounded(p.Args)
				}
			case session.EventToolResult:
				var p session.ToolResultPayload
				if json.Unmarshal(e.Payload, &p) == nil {
					evidence["result"] = bounded(p.Result)
					evidence["is_error"] = p.IsError
				}
			case session.EventPolicyDecision:
				var p session.PolicyDecisionPayload
				if json.Unmarshal(e.Payload, &p) == nil {
					decisions = append(decisions, map[string]any{"seq": e.Seq, "verdict": shortText(p.Verdict, 256), "rule": shortText(p.RuleID, 256), "reason": shortText(p.Reason, 2048)})
				}
			}
		}
		if e.Seq < seq {
			precedingEvents = append(precedingEvents, e)
			if len(precedingEvents) > 3 {
				precedingEvents = precedingEvents[1:]
			}
		}
	}
	preceding := []eventView{}
	for _, e := range precedingEvents {
		preceding = append(preceding, viewEvent(e))
	}
	if len(related) == 0 {
		apiError(w, 404, fmt.Errorf("event not found in this snapshot"))
		return
	}
	evidence["decisions"] = decisions
	writeJSON(w, map[string]any{"activity": safeActivity(a), "evidence": evidence, "events": related, "preceding": preceding, "session": rec.Info, "related_limited": len(wanted) > 20})
}

// Engines consume the exact in-memory snapshot that the UI inspected, never a
// second path read or an untrusted blob reference. Legacy duplicate IDs are
// refused rather than silently paired by the CLI loader's ID map.
func workbenchTape(rec *recording) (*replay.Tape, error) {
	if !rec.Info.ChainOK || !rec.Info.Complete || rec.Info.Calls == 0 {
		return nil, fmt.Errorf("choose a non-empty, completed recording with a verified chain")
	}
	if rec.Info.Ambiguous {
		return nil, fmt.Errorf("request relationships are ambiguous; use the event inspector instead")
	}
	if len(rec.Events) > 10000 {
		return nil, fmt.Errorf("comparison and test creation are limited to 10,000 events; use the CLI for larger sessions")
	}
	tape, err := replay.LoadInline(bytes.NewReader(rec.Data))
	if err != nil {
		return nil, err
	}
	// The existing engine is ID-based. Refuse any parent link that would produce
	// a different pairing; the activity timeline can still show that evidence.
	bySeq := make(map[uint64]session.Event, len(rec.Events))
	for _, e := range rec.Events {
		bySeq[e.Seq] = e
	}
	for _, e := range rec.Events {
		if e.Type != session.EventPolicyDecision && e.Type != session.EventToolResult {
			continue
		}
		var p struct {
			ID     json.RawMessage `json:"id"`
			Parent *uint64         `json:"parent_seq"`
		}
		_ = json.Unmarshal(e.Payload, &p)
		if p.Parent == nil {
			continue
		}
		if call, ok := bySeq[*p.Parent]; ok {
			var cp session.ToolCallPayload
			_ = json.Unmarshal(call.Payload, &cp)
			if idKey(cp.ID) != idKey(p.ID) {
				return nil, fmt.Errorf("parent and request ID disagree; engine comparison would be ambiguous")
			}
		}
	}
	return tape, nil
}

func (s *server) compareAPI(w http.ResponseWriter, r *http.Request) {
	if !getOnly(w, r) {
		return
	}
	if s.dir == "" {
		apiError(w, 400, fmt.Errorf("comparison needs directory mode"))
		return
	}
	a, err := s.recording(r.URL.Query().Get("a"))
	if err != nil {
		apiError(w, 400, err)
		return
	}
	b, err := s.recording(r.URL.Query().Get("b"))
	if err != nil {
		apiError(w, 400, err)
		return
	}
	at, err := workbenchTape(a)
	if err != nil {
		apiError(w, 422, err)
		return
	}
	bt, err := workbenchTape(b)
	if err != nil {
		apiError(w, 422, err)
		return
	}
	writeJSON(w, map[string]any{"a": a.Info, "b": b.Info, "diff": replay.Diff(at, bt), "investigation": compareAttempts(at, bt), "policy_history": comparePolicyHistory(a, b, at, bt), "scope": "Boundary attempts matched by tool and canonical argument hash, then occurrence order. Repeated identical calls are ambiguous. Policy comparison uses the last recorded decision per call. Differences do not automatically indicate regressions."})
}

func buildScenario(rec *recording, seq uint64, kind, fixture string, seqs ...uint64) (*scenario.Scenario, error) {
	if len(fixture) == 0 || len(fixture) > 1024 || strings.ContainsAny(fixture, "\x00\r\n") {
		return nil, fmt.Errorf("enter the fixture path relative to where you will save the test")
	}
	var selected *activity
	for _, a := range rec.Activities {
		if a.Seq == seq {
			selected = a
			break
		}
	}
	if selected == nil {
		return nil, fmt.Errorf("select a recorded tool call")
	}
	if strings.ContainsAny(selected.Tool, "*?") {
		return nil, fmt.Errorf("tool name contains assertion glob characters; build this assertion manually")
	}
	check := scenario.Check{}
	switch kind {
	case "denied":
		if selected.Verdict != "deny" {
			return nil, fmt.Errorf("this call has no recorded deny verdict")
		}
		check.Denied = selected.Tool
	case "attempt_ceiling":
		check.Attempted = &scenario.AttemptSpec{Tool: selected.Tool}
	case "no_attempts":
		zero := 0
		check.Attempted = &scenario.AttemptSpec{Tool: selected.Tool, Times: &zero}
	case "invariant":
		check.Invariant = "no_deny_bypassed"
	case "sequence":
		if len(seqs) < 2 || len(seqs) > 10 {
			return nil, fmt.Errorf("select 2 to 10 answered calls")
		}
		used := map[uint64]bool{}
		for _, n := range seqs {
			if used[n] {
				return nil, fmt.Errorf("select each call only once")
			}
			used[n] = true
			found := false
			for _, a := range rec.Activities {
				if a.Seq == n {
					if a.ResultSeq == 0 || strings.ContainsAny(a.Tool, "*?") {
						return nil, fmt.Errorf("sequence requires distinct answered calls with literal tool names")
					}
					check.Sequence = append(check.Sequence, a.Tool)
					found = true
					break
				}
			}
			if !found {
				return nil, fmt.Errorf("sequence event not found")
			}
		}
	case "attempted":
		check.Attempted = &scenario.AttemptSpec{Tool: selected.Tool}
	case "answered":
		if selected.ResultSeq == 0 {
			return nil, fmt.Errorf("this call has no recorded result")
		}
		check.Called = &scenario.CalledSpec{Tool: selected.Tool}
	case "never_answered":
		for _, a := range rec.Activities {
			if a.Tool == selected.Tool && a.ResultSeq != 0 {
				return nil, fmt.Errorf("this tool has recorded results in this session")
			}
		}
		check.NeverCalled = selected.Tool
	default:
		return nil, fmt.Errorf("choose denied, attempted, answered or never_answered")
	}
	return &scenario.Scenario{Version: 1, Name: "Recorded boundary regression", Fixture: fixture, Assert: []scenario.Check{check}}, nil
}

func (s *server) testAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		apiError(w, 405, fmt.Errorf("POST only"))
		return
	}
	var req struct {
		File     string   `json:"file"`
		Revision string   `json:"revision"`
		Seq      uint64   `json:"seq"`
		Kind     string   `json:"kind"`
		Fixture  string   `json:"fixture"`
		Run      bool     `json:"run"`
		Seqs     []uint64 `json:"seqs"`
		Ceiling  *int     `json:"ceiling"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		apiError(w, 400, fmt.Errorf("invalid test request (maximum 4 KiB)"))
		return
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		apiError(w, 400, fmt.Errorf("request must contain one JSON object"))
		return
	}
	rec, err := s.recording(req.File)
	if err != nil {
		apiError(w, 400, err)
		return
	}
	if req.Revision != rec.Info.Revision {
		apiError(w, 409, fmt.Errorf("recording changed; refresh and review this call before generating a test"))
		return
	}
	tape, err := workbenchTape(rec)
	if err != nil {
		apiError(w, 422, err)
		return
	}
	sc, err := buildScenario(rec, req.Seq, req.Kind, req.Fixture, req.Seqs...)
	if err != nil {
		apiError(w, 422, err)
		return
	}
	if req.Kind == "attempt_ceiling" {
		if req.Ceiling == nil || *req.Ceiling < 0 || *req.Ceiling > 100000 {
			apiError(w, 422, fmt.Errorf("enter an attempt ceiling from 0 to 100000"))
			return
		}
		sc.Assert[0].Attempted.Max = req.Ceiling
	}
	data, err := yaml.Marshal(sc)
	if err != nil {
		apiError(w, 500, err)
		return
	}
	resp := map[string]any{"yaml": string(data), "scope": "Assertions apply to tool names across the cassette, not a request ID. Zero attempts checks only this recorded boundary; never_called checks answered calls only. Sequence is an ordered subsequence of answered tools. The invariant checks recorded denials and results. Attempt ceiling counts boundary attempts, including unanswered calls. The engine cannot assert that a specific error does not recur. No arguments or results are copied, no live tool runs, and no file is written by the server."}
	if req.Run {
		fails := scenario.Run(sc, tape, nil)
		resp["passed"] = len(fails) == 0
		resp["failures"] = fails
	}
	writeJSON(w, resp)
}
