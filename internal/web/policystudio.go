package web

// Policy Studio: a read-only, evidence-based view of Tapelog's existing
// policy engine. It adds no evaluation logic of its own — every decision
// shown here comes from internal/policy and internal/replay — and it has no
// file-writing endpoint. A candidate policy exists only as text in a response
// until an operator explicitly saves it in their own browser.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"

	"github.com/Caseymccallum/tapelog/internal/policy"
	"github.com/Caseymccallum/tapelog/internal/replay"
	"github.com/Caseymccallum/tapelog/internal/session"
	"github.com/Caseymccallum/tapelog/packs"
)

const (
	maxActivePolicyBytes = 512 << 10 // 512 KiB of YAML is far beyond any real policy
	maxPolicyRequestBody = 32 << 10  // a Studio form, not an editor buffer
	maxCandidateResponse = 64 << 10  // refuse to hand back an unbounded document
)

// policySource is one policy the Studio can show. Source is never sent as
// JSON; the UI asks for inspect, which bounds and digests the text.
type policySource struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"` // active | pack
	Name        string `json:"name"`
	Path        string `json:"path,omitempty"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Digest      string `json:"digest"`
	Bytes       int    `json:"bytes"`
	Error       string `json:"error,omitempty"`

	source string
}

func digestOf(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// readPolicyFile reads a bounded, regular policy file. The path comes from
// the operator's own --policy flag, not from a request.
func readPolicyFile(path string) ([]byte, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, fmt.Errorf("policy path is not a regular file")
	}
	if st.Size() > maxActivePolicyBytes {
		return nil, fmt.Errorf("policy file exceeds the %d KiB Studio limit; inspect it with the CLI", maxActivePolicyBytes>>10)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxActivePolicyBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxActivePolicyBytes {
		return nil, fmt.Errorf("policy file exceeds the %d KiB Studio limit", maxActivePolicyBytes>>10)
	}
	return data, nil
}

// policySources lists the configured policy and the embedded packs. A broken
// active policy is reported, not hidden: an operator needs to see that the
// file they pointed at does not compile.
func (s *server) policySources() []policySource {
	out := []policySource{}
	if s.policyPath != "" {
		src := policySource{ID: "active", Kind: "active", Name: "Workspace policy", Path: s.policyPath}
		data, err := readPolicyFile(s.policyPath)
		if err != nil {
			src.Error = err.Error()
		} else {
			src.source = string(data)
			src.Bytes = len(data)
			src.Digest = digestOf(data)
			if _, err := policy.Parse(data, s.policyPath); err != nil {
				src.Error = err.Error()
			}
		}
		out = append(out, src)
	}
	for _, p := range packs.All() {
		src := policySource{ID: "pack:" + p.Name, Kind: "pack", Name: p.Name, Title: p.Title, Description: p.Description, source: p.Source, Bytes: p.Bytes, Digest: digestOf([]byte(p.Source))}
		if _, err := policy.Parse([]byte(p.Source), p.Name); err != nil {
			src.Error = err.Error()
		}
		out = append(out, src)
	}
	return out
}

// policySourceByID resolves a source id from a request against the same list
// that was advertised, so a request cannot name an arbitrary file.
func (s *server) policySourceByID(id string) (policySource, bool) {
	for _, src := range s.policySources() {
		if src.ID == id {
			return src, true
		}
	}
	return policySource{}, false
}

// validation is the engine's verdict on a candidate document.
type validation struct {
	OK      bool   `json:"ok"`
	Error   string `json:"error,omitempty"`
	Version int    `json:"version,omitempty"`
	Rules   int    `json:"rules"`
	Flows   int    `json:"flows"`
}

// analyse parses and describes a policy document through the real engine.
func analyse(data []byte, name string) (*policy.Policy, *policy.Analysis, validation) {
	p, err := policy.Parse(data, name)
	if err != nil {
		return nil, nil, validation{OK: false, Error: err.Error()}
	}
	a := policy.Analyze(p, data)
	return p, a, validation{OK: true, Version: p.Version, Rules: len(p.Rules), Flows: len(p.Flows)}
}

func writeAPI(w http.ResponseWriter, v any) {
	// The charset is explicit: a policy candidate contains the source text of
	// the policy it extends, which may be any UTF-8 the operator wrote. Clients
	// that default to Latin-1 without it (Windows PowerShell's
	// Invoke-RestMethod, for one) would otherwise mangle that text.
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(true)
	_ = enc.Encode(v)
}

// decodePolicyRequest decodes exactly one bounded JSON object and refuses
// unknown fields, so a typo cannot silently change a policy request.
func decodePolicyRequest(w http.ResponseWriter, r *http.Request, dst any) error {
	if r.Method != http.MethodPost {
		return fmt.Errorf("POST only")
	}
	body := http.MaxBytesReader(w, r.Body, maxPolicyRequestBody)
	dec := json.NewDecoder(body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			return fmt.Errorf("request is larger than the %d KiB Studio limit", maxPolicyRequestBody>>10)
		}
		return fmt.Errorf("invalid Studio request: %w", err)
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("request must contain one JSON object")
	}
	return nil
}

func sortedStrings(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

func trimmed(s string, n int) string { return shortText(strings.TrimSpace(s), n) }

// policiesAPI advertises the policies the Studio can show. It never returns
// policy text — inspect does that, bounded and digested.
func (s *server) policiesAPI(w http.ResponseWriter, r *http.Request) {
	if !getOnly(w, r) {
		return
	}
	sources := s.policySources()
	packsOut := make([]policySource, 0, len(sources))
	active := (*policySource)(nil)
	for i := range sources {
		if sources[i].Kind == "active" {
			v := sources[i]
			active = &v
			continue
		}
		packsOut = append(packsOut, sources[i])
	}
	notes := []string{
		"The workbench is read-only: no endpoint writes, replaces or activates a policy file.",
		"A candidate is text in this page until you save it yourself in your browser.",
		"Explanations describe what the engine does. Conditions it cannot simplify are shown as Cedar source.",
	}
	if active == nil {
		notes = append(notes, "No policy file is configured for this workbench. Start it with `tapelog web --dir <recordings> --policy <policy.yaml>` to explain the rules in use. The boundary's other protections still apply without a policy.")
	}
	writeAPI(w, map[string]any{
		"active":        active,
		"packs":         packsOut,
		"notes":         notes,
		"read_only":     true,
		"policy_engine": "internal/policy (YAML rules compiled by the Cedar engine)",
	})
}

// inspectAPI explains one policy document, with its original source.
func (s *server) inspectAPI(w http.ResponseWriter, r *http.Request) {
	if !getOnly(w, r) {
		return
	}
	id := r.URL.Query().Get("source")
	src, ok := s.policySourceByID(id)
	if !ok {
		apiError(w, 404, fmt.Errorf("choose one of the listed policies"))
		return
	}
	resp := map[string]any{
		"id":     src.ID,
		"kind":   src.Kind,
		"name":   src.Name,
		"path":   src.Path,
		"digest": src.Digest,
		"bytes":  src.Bytes,
		"title":  src.Title,
	}
	if src.Error != "" {
		resp["valid"] = false
		resp["error"] = src.Error
		resp["yaml"] = src.source
		writeAPI(w, resp)
		return
	}
	_, analysis, v := analyse([]byte(src.source), src.Name)
	resp["valid"] = v.OK
	resp["validation"] = v
	resp["analysis"] = analysis
	resp["yaml"] = src.source
	resp["scope"] = "This is the whole policy. Rules are evaluated top to bottom and the first rule that is in scope and matches wins; if no rule applies the default decides. Command-flow rules are evaluated before per-call rules."
	writeAPI(w, resp)
}

// policyRequest is the Studio's builder form.
type policyRequest struct {
	Base     string              `json:"base"` // "new" | "pack:<name>" | "active"
	Default  string              `json:"default"`
	Rule     policy.RuleDraft    `json:"rule"`
	Limits   *policy.LimitsDraft `json:"limits,omitempty"`
	File     string              `json:"file"`
	Revision string              `json:"revision"`
	Task     string              `json:"task"`
}

// buildCandidate turns a form into candidate text plus what the UI needs to
// explain it. It never writes anything and never touches the active policy.
func (s *server) buildCandidate(req policyRequest) (candidate []byte, baseID string, extended bool, notes []string, err error) {
	explicitID := strings.TrimSpace(req.Rule.ID) != ""
	if err := policy.ValidateRuleDraft(&req.Rule); err != nil {
		return nil, "", false, nil, err
	}
	base := strings.TrimSpace(req.Base)
	if base == "" || base == "new" {
		def := strings.TrimSpace(req.Default)
		if def == "" {
			def = "deny"
		}
		var lim policy.LimitsDraft
		if req.Limits != nil {
			lim = *req.Limits
		}
		src, err := policy.NewCandidateWithLimits(def, req.Rule, lim)
		if err != nil {
			return nil, "", false, nil, err
		}
		notes = append(notes,
			fmt.Sprintf("This is a new policy document with default: %s. It contains only the rule you built; it does not inherit any rule from another policy.", def),
			"With default deny, every call this rule does not match is blocked.")
		if lim.Set() {
			notes = append(notes, "Session limits are enforced by the boundary around each call, not by rule evaluation, so they never appear as a rule decision.")
		}
		return src, "new", false, notes, nil
	}
	if req.Limits != nil && req.Limits.Set() {
		return nil, "", false, nil, fmt.Errorf("limits can only be set on a new candidate; extending a policy keeps its own limits unchanged — edit the source to change them")
	}

	src, ok := s.policySourceByID(base)
	if !ok {
		return nil, "", false, nil, fmt.Errorf("choose a policy to extend, or start a new candidate")
	}
	if src.Kind == "active" {
		notes = append(notes, "You are extending a copy of the workspace policy. The file on disk is only read, never modified.")
	}
	if src.Error != "" {
		return nil, "", false, nil, fmt.Errorf("%s does not compile, so it cannot be extended safely: %s", src.Name, src.Error)
	}
	existing, analysis, _ := analyse([]byte(src.source), src.Name)
	if existing == nil {
		return nil, "", false, nil, fmt.Errorf("%s could not be read", src.Name)
	}
	taken := map[string]bool{}
	for _, id := range analysis.RuleIDs {
		taken[id] = true
	}
	if taken[req.Rule.ID] {
		if explicitID {
			return nil, "", false, nil, fmt.Errorf("%s already has a rule with id %q; choose a different id", src.Name, req.Rule.ID)
		}
		req.Rule.ID = policy.UniqueID(req.Rule.ID, taken)
	}
	out, err := policy.InsertRule([]byte(src.source), req.Rule)
	if err != nil {
		return nil, "", false, nil, err
	}
	updated, updatedAnalysis, v := analyse(out, src.Name)
	if updated == nil {
		return nil, "", false, nil, fmt.Errorf("the extended policy does not compile: %s", v.Error)
	}
	if len(updated.Rules) != len(existing.Rules)+1 {
		return nil, "", false, nil, fmt.Errorf("the extension produced %d rules, expected %d; nothing was offered", len(updated.Rules), len(existing.Rules)+1)
	}
	if updated.Rules[0].ID != req.Rule.ID {
		return nil, "", false, nil, fmt.Errorf("the new rule is not the first rule evaluated; refusing to offer this candidate")
	}
	if len(updatedAnalysis.Flows) != len(analysis.Flows) {
		return nil, "", false, nil, fmt.Errorf("the extension changed the flow rules; refusing to offer this candidate")
	}
	notes = append(notes,
		fmt.Sprintf("The new rule is inserted first in %s, so it is evaluated before every existing rule. Everything else in that document is byte-for-byte unchanged.", src.Name),
		fmt.Sprintf("Existing rules keep their order: %s.", strings.Join(updatedAnalysis.RuleIDs[1:], ", ")))
	return out, base, true, notes, nil
}

// previewAPI builds a candidate and, when a recording is supplied, simulates
// it with the same engine as `tapelog policy whatif`.
func (s *server) previewAPI(w http.ResponseWriter, r *http.Request) {
	if !postOnly(w, r) {
		return
	}
	var req policyRequest
	if err := decodePolicyRequest(w, r, &req); err != nil {
		apiError(w, 400, err)
		return
	}
	candidate, baseID, extended, notes, err := s.buildCandidate(req)
	if err != nil {
		apiError(w, 422, err)
		return
	}
	if len(candidate) > maxCandidateResponse {
		apiError(w, 422, fmt.Errorf("the generated candidate is larger than the Studio will hand back"))
		return
	}
	p, analysis, v := analyse(candidate, "candidate")
	resp := map[string]any{
		"candidate":  string(candidate),
		"digest":     digestOf(candidate),
		"valid":      v.OK,
		"validation": v,
		"analysis":   analysis,
		"base":       baseID,
		"extended":   extended,
		"rule_id":    req.Rule.ID,
		"notes":      notes,
		"warnings":   candidateWarnings(analysis, extended, baseID),
		"reviewed": map[string]any{
			"rule_id":     req.Rule.ID,
			"action":      req.Rule.Action,
			"tools":       req.Rule.Tools,
			"conditions":  len(req.Rule.Conditions),
			"tasks":       len(req.Rule.Tasks),
			"expires":     req.Rule.Expires,
			"candidate_sha256": digestOf(candidate),
		},
		"scope": "Review this text before saving. The candidate is not active and the workbench has not written it anywhere. Simulating it re-evaluates recorded calls only; it cannot prove how unseen calls will behave.",
	}
	if req.File != "" {
		extra := s.attachSimulation(resp, req.File, req.Revision, p, req.Task)
		resp["warnings"] = append(resp["warnings"].([]string), extra...)
	}
	writeAPI(w, resp)
}

// candidateWarnings calls out the risky shape of a candidate when it differs
// from the policy it extends.
func candidateWarnings(a *policy.Analysis, extended bool, baseID string) []string {
	if a == nil {
		return nil
	}
	out := append([]string{}, a.Warnings...)
	for _, r := range a.Rules {
		if r.Broad {
			out = append(out, fmt.Sprintf("Rule %q is broad: %s.", r.ID, strings.Join(r.BroadWhy, "; ")))
		}
		for _, c := range r.Conditions {
			if c.Problem != "" {
				out = append(out, fmt.Sprintf("Rule %q has a condition that cannot match what it appears to target: %s", r.ID, c.Problem))
			}
		}
	}
	if extended && baseID == "new" {
		out = append(out, "This candidate is a separate document, not a modification of an existing policy.")
	}
	return out
}

// attachSimulation re-evaluates the recording against the candidate with the
// same engine as `tapelog policy whatif`, and returns warnings for it.
func (s *server) attachSimulation(resp map[string]any, file, revision string, p *policy.Policy, task string) []string {
	simulate := func(err error) []string {
		resp["simulation_error"] = err.Error()
		return nil
	}
	rec, err := s.recording(file)
	if err != nil {
		return simulate(err)
	}
	if revision != "" && revision != rec.Info.Revision {
		resp["simulation_stale"] = true
		return simulate(fmt.Errorf("the recording changed since this call was selected; refresh and preview again"))
	}
	tape, err := workbenchTape(rec)
	if err != nil {
		return simulate(err)
	}
	if p == nil {
		return simulate(fmt.Errorf("the candidate did not compile, so it cannot be simulated"))
	}
	sim := replay.Simulate(tape, p, task)
	resp["simulation"] = sim
	resp["simulation_scope"] = "Re-evaluated with the boundary's own evaluation pipeline: value-level taint rebuilt from recorded results, then session flows, then ordered rules. Recorded (redacted) arguments are used, so verdicts reproduce exactly. No tool call is executed."
	resp["simulation_limits"] = "This shows what the candidate would have decided about activity that was already recorded. It does not prove how future, unseen calls will behave, and it cannot see traffic that bypassed this boundary."

	warnings := []string{}
	permitted, released := 0, 0
	for _, c := range sim.Calls {
		switch {
		case c.RecordedVerdict == "deny" && c.CandidateVerdict != "deny":
			permitted++
		case c.RecordedVerdict == "confirm" && c.CandidateVerdict == "allow":
			released++
		}
	}
	if permitted > 0 {
		warnings = append(warnings, fmt.Sprintf("This candidate would permit %d call(s) that the recording shows as denied.", permitted))
	}
	if released > 0 {
		warnings = append(warnings, fmt.Sprintf("This candidate would skip human approval for %d call(s) that were recorded as needing it.", released))
	}
	if sim.NewlyDenied > 0 {
		warnings = append(warnings, fmt.Sprintf("This candidate would block %d call(s) that were recorded as allowed. Review them before adopting it.", sim.NewlyDenied))
	}
	return warnings
}

// simulateRequest compares one existing policy (or a candidate text) with a
// recording, without building a rule.
type simulateRequest struct {
	Source    string `json:"source"`
	Candidate string `json:"candidate"`
	File      string `json:"file"`
	Revision  string `json:"revision"`
	Task      string `json:"task"`
}

func (s *server) simulateAPI(w http.ResponseWriter, r *http.Request) {
	if !postOnly(w, r) {
		return
	}
	var req simulateRequest
	if err := decodePolicyRequest(w, r, &req); err != nil {
		apiError(w, 400, err)
		return
	}
	if strings.TrimSpace(req.File) == "" {
		apiError(w, 422, fmt.Errorf("choose a recording to evaluate"))
		return
	}
	var (
		data   []byte
		label  string
		source string
	)
	switch {
	case strings.TrimSpace(req.Candidate) != "":
		data = []byte(req.Candidate)
		label = "candidate"
		source = "candidate"
	case strings.TrimSpace(req.Source) != "":
		src, ok := s.policySourceByID(req.Source)
		if !ok {
			apiError(w, 404, fmt.Errorf("choose one of the listed policies"))
			return
		}
		if src.Error != "" {
			apiError(w, 422, fmt.Errorf("%s does not compile: %s", src.Name, src.Error))
			return
		}
		data = []byte(src.source)
		label = src.Name
		source = src.ID
	default:
		apiError(w, 422, fmt.Errorf("choose a policy to evaluate"))
		return
	}
	p, analysis, v := analyse(data, label)
	if !v.OK {
		apiError(w, 422, fmt.Errorf("this policy does not compile: %s", v.Error))
		return
	}
	resp := map[string]any{
		"source":   source,
		"name":     label,
		"digest":   digestOf(data),
		"analysis": analysis,
		"warnings": candidateWarnings(analysis, false, source),
	}
	extra := s.attachSimulation(resp, req.File, req.Revision, p, req.Task)
	resp["warnings"] = append(resp["warnings"].([]string), extra...)
	writeAPI(w, resp)
}

type fromEventRequest struct {
	File     string `json:"file"`
	Revision string `json:"revision"`
	Seq      uint64 `json:"seq"`
}

// fromEventAPI prepopulates a proposal from one recorded call. It copies only
// what the recording supports: the tool name, the recorded decision and the
// argument keys. It never proposes a glob, and it never applies anything.
func (s *server) fromEventAPI(w http.ResponseWriter, r *http.Request) {
	if !postOnly(w, r) {
		return
	}
	var req fromEventRequest
	if err := decodePolicyRequest(w, r, &req); err != nil {
		apiError(w, 400, err)
		return
	}
	rec, err := s.recording(req.File)
	if err != nil {
		apiError(w, 400, err)
		return
	}
	if req.Revision != "" && req.Revision != rec.Info.Revision {
		apiError(w, 409, fmt.Errorf("recording changed; refresh and review this call before building a proposal"))
		return
	}
	var selected *activity
	for _, a := range rec.Activities {
		if a.Seq == req.Seq {
			selected = a
			break
		}
	}
	if selected == nil {
		apiError(w, 404, fmt.Errorf("select a recorded tool call"))
		return
	}
	if strings.ContainsAny(selected.Tool, "*?") {
		apiError(w, 422, fmt.Errorf("the recorded tool name contains assertion wildcards; write this rule in the policy source"))
		return
	}
	if strings.TrimSpace(selected.Tool) == "" || len(selected.Tool) > 128 || strings.ContainsFunc(selected.Tool, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		apiError(w, 422, fmt.Errorf("the recorded tool name is not usable as a literal rule target"))
		return
	}

	notes := []string{
		"This proposal comes from a single recorded call. One call shows what happened once, not what the agent intended or will do next.",
		"A rule matches the tool name, so it governs every future call to " + selected.Tool + " — not only calls that look like this one.",
		"Nothing here is applied. Review the rule, simulate it, and save it yourself.",
	}
	if selected.Verdict == "deny" {
		notes = append(notes, "This call already has a recorded deny decision, so a new deny rule would not change this recording.")
	}
	if selected.ResultSeq != 0 {
		notes = append(notes, "This call also has a recorded result. Adding a rule does not change or remove recorded evidence.")
	}

	argKeys := []map[string]any{}
	if raw, ok := recordedArgs(rec, selected.Seq); ok {
		argKeys = argumentKeys(raw)
	}
	for _, k := range argKeys {
		if k["redacted"] == true {
			notes = append(notes, "Recorded arguments are redacted. A condition built from a redacted value may not match the real value at run time.")
			break
		}
	}
	if len(argKeys) == 0 {
		notes = append(notes, "No usable argument keys were recorded for this call, so only a tool-level rule can be proposed here.")
	}

	writeAPI(w, map[string]any{
		"tool":             selected.Tool,
		"seq":              selected.Seq,
		"status":           selected.Status,
		"server":           selected.Server,
		"recorded_verdict": selected.Verdict,
		"recorded_rule":    selected.Rule,
		"recorded_reason":  trimmed(selected.Reason, 2048),
		"arg_keys":         argKeys,
		"notes":            notes,
		"suggested_action": suggestedAction(selected.Verdict),
		"revision":         rec.Info.Revision,
		"scope":            "The recorded decision was produced by whatever policy was active during that session. It is evidence about that call, not about the policy you are drafting.",
	})
}

// suggestedAction maps a recorded verdict to a starting point, never a
// decision: the operator still chooses the outcome.
func suggestedAction(verdict string) string {
	switch verdict {
	case "deny":
		return "deny"
	case "confirm":
		return "confirm"
	}
	return "confirm"
}

// recordedArgs returns the recorded (redacted) tool arguments for one event.
func recordedArgs(rec *recording, seq uint64) (json.RawMessage, bool) {
	for _, e := range rec.Events {
		if e.Seq != seq || e.Type != session.EventToolCall {
			continue
		}
		var p session.ToolCallPayload
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return nil, false
		}
		return p.Args, len(p.Args) > 0
	}
	return nil, false
}

// argumentKeys lists top-level argument keys the visual editor can match on.
// Only keys that are valid Cedar identifiers are offered.
func argumentKeys(raw json.RawMessage) []map[string]any {
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return []map[string]any{}
	}
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]map[string]any, 0, len(keys))
	for _, k := range keys {
		if len(out) >= 24 {
			break
		}
		entry := map[string]any{"key": k, "matchable": policy.MatchableArgumentKey(k)}
		switch v := obj[k].(type) {
		case string:
			entry["kind"] = "string"
			entry["value_preview"] = trimmed(v, 160)
			entry["redacted"] = strings.Contains(v, "REDACTED")
		case float64:
			entry["kind"] = "number"
			entry["value_preview"] = trimmed(json.Number(fmt.Sprintf("%v", v)).String(), 40)
			entry["redacted"] = false
		case bool:
			entry["kind"] = "bool"
			entry["value_preview"] = fmt.Sprintf("%v", v)
			entry["redacted"] = false
		default:
			entry["kind"] = "other"
			entry["value_preview"] = ""
			entry["redacted"] = false
			entry["matchable"] = false
		}
		out = append(out, entry)
	}
	return out
}

