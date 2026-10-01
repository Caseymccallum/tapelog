// story.go — causal correlation for `inspect`: joins tools/call,
// policy/decision, and tools/result events by request id, then renders
// DENIED blocks with provenance (docs/ROADMAP.md v0.4: "DENIED blocks with
// event #, session id, contamination provenance; inspect renders the
// causal story of a trajectory").
//
// Provenance comes from the flow-decision reason string
// (internal/policy/flows.go appends "[contaminated by: X]" /
// "[taint sources: X]"); each source name is mapped back to the call
// event that produced it — suffix-matching through the mux <server>__
// namespace so "read_secrets" finds both "read_secrets" (record mode) and
// "a__read_secrets" (mux mode).
package tui

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/Caseymccallum/tapelog/internal/session"
)

// ProvenanceLine names one contamination source and where it was
// produced in the log.
type ProvenanceLine struct {
	Source    string
	CallSeq   uint64 // tools/call event that produced the value
	ResultSeq uint64 // its tools/result event (0 = no result recorded)
}

// DeniedBlock is one denied call with full causal context.
type DeniedBlock struct {
	CallSeq    uint64 // the tools/call event #
	DecSeq     uint64 // the policy/decision event #
	SessionID  string
	Tool       string
	Args       string // compact single-line args
	RuleID     string
	Reason     string
	Provenance []ProvenanceLine
	Bypass     bool   // a result exists despite the deny (invariant break!)
	BypassSeq  uint64 // that result's event #
}

// Story is the causal story of one session.
type Story struct {
	SessionID string
	Denied    []DeniedBlock
}

// provenanceRe matches the explanation suffix policy flow decisions add:
// "[contaminated by: a, b]" or "[taint sources: a, b]".
var provenanceRe = regexp.MustCompile(`\[(contaminated by|taint sources): ([^\]]+)\]`)

// callRef tracks one request across call/decision/result events.
type callRef struct {
	seq       uint64
	tool      string
	args      string
	resultSeq uint64
}

// buildStory correlates a session's events into the causal story. Two
// passes: first index calls/results, then build deny blocks — so a result
// recorded AFTER its deny (an invariant break) is still flagged.
func buildStory(events []session.Event) Story {
	st := Story{}
	byID := map[string]*callRef{}
	type denyAt struct {
		i int
		e session.Event
		p session.PolicyDecisionPayload
	}
	var denies []denyAt

	for i, e := range events {
		if st.SessionID == "" && e.SessionID != "" {
			st.SessionID = e.SessionID
		}
		switch e.Type {
		case session.EventToolCall:
			var p session.ToolCallPayload
			if json.Unmarshal(e.Payload, &p) != nil {
				continue
			}
			byID[string(p.ID)] = &callRef{seq: e.Seq, tool: p.Tool, args: compact(p.Args)}
		case session.EventToolResult:
			var p session.ToolResultPayload
			if json.Unmarshal(e.Payload, &p) != nil {
				continue
			}
			if ref := byID[string(p.ID)]; ref != nil {
				ref.resultSeq = e.Seq
			}
		case session.EventPolicyDecision:
			var p session.PolicyDecisionPayload
			if json.Unmarshal(e.Payload, &p) != nil || p.Verdict != "deny" {
				continue
			}
			denies = append(denies, denyAt{i: i, e: e, p: p})
		}
	}
	for _, d := range denies {
		st.Denied = append(st.Denied, deniedBlock(events, d.i, d.e, d.p, byID))
	}
	return st
}

// deniedBlock assembles one denied call's causal context.
func deniedBlock(events []session.Event, i int, e session.Event,
	p session.PolicyDecisionPayload, byID map[string]*callRef) DeniedBlock {
	block := DeniedBlock{
		DecSeq: e.Seq, SessionID: e.SessionID,
		RuleID: p.RuleID, Reason: p.Reason,
	}
	if ref := byID[string(p.ID)]; ref != nil {
		block.CallSeq = ref.seq
		block.Tool = ref.tool
		block.Args = ref.args
		if ref.resultSeq > 0 {
			// A denied call must never produce a result (invariant
			// no_deny_bypassed) — surface it loudly.
			block.Bypass = true
			block.BypassSeq = ref.resultSeq
		}
	}
	for _, line := range provenanceRe.FindAllStringSubmatch(p.Reason, -1) {
		for _, src := range strings.Split(line[2], ",") {
			src = strings.TrimSpace(src)
			if src == "" {
				continue
			}
			block.Provenance = append(block.Provenance, traceSource(events, i, src))
		}
	}
	return block
}

// traceSource maps a contamination source name back to the producing
// call (and its result) — the closest preceding call whose tool matches
// the source name, namespace-insensitive.
func traceSource(events []session.Event, before int, source string) ProvenanceLine {
	pv := ProvenanceLine{Source: source}
	for j := before - 1; j >= 0; j-- {
		if events[j].Type != session.EventToolCall {
			continue
		}
		var cp session.ToolCallPayload
		if json.Unmarshal(events[j].Payload, &cp) != nil || !sameTool(cp.Tool, source) {
			continue
		}
		pv.CallSeq = events[j].Seq
		for k := j + 1; k < before; k++ {
			if events[k].Type != session.EventToolResult {
				continue
			}
			var rp session.ToolResultPayload
			if json.Unmarshal(events[k].Payload, &rp) == nil && string(rp.ID) == string(cp.ID) {
				pv.ResultSeq = events[k].Seq
				break
			}
		}
		break
	}
	return pv
}

// sameTool matches a recorded tool name to a policy source name,
// tolerating the mux <server>__ namespace on either side.
func sameTool(tool, source string) bool {
	if tool == source {
		return true
	}
	if _, t, ok := cutNS(tool); ok && t == source {
		return true
	}
	if _, s, ok := cutNS(source); ok && s == tool {
		return true
	}
	_, t1, ok1 := cutNS(tool)
	_, s1, ok2 := cutNS(source)
	return ok1 && ok2 && t1 == s1
}

// cutNS splits a mux namespaced name at the first "__".
func cutNS(name string) (server, tool string, ok bool) {
	i := strings.Index(name, "__")
	if i <= 0 || i+2 >= len(name) {
		return "", "", false
	}
	return name[:i], name[i+2:], true
}

// cleanReason strips the provenance bracket from a decision reason
// (provenance gets its own rendered lines).
func cleanReason(reason string) string {
	return strings.TrimSpace(provenanceRe.ReplaceAllString(reason, ""))
}

// causalLine is the one-line causal story of a denied call.
func causalLine(d DeniedBlock) string {
	call := fmt.Sprintf("call #%d", d.CallSeq)
	if d.CallSeq == 0 {
		call = "call"
	}
	s := fmt.Sprintf("%s → deny (%s)", call, d.RuleID)
	if len(d.Provenance) > 0 {
		srcs := make([]string, 0, len(d.Provenance))
		for _, pv := range d.Provenance {
			srcs = append(srcs, pv.Source)
		}
		s += fmt.Sprintf(" ← %s", strings.Join(srcs, ", "))
	}
	if d.Bypass {
		s += fmt.Sprintf(" ⚠ RESULT #%d RECORDED DESPITE DENY (no_deny_bypassed)", d.BypassSeq)
	} else {
		s += " → never executed"
	}
	return s
}

// renderDenied renders the DENIED section: one block per denied call with
// event #, session id, provenance, and the causal story line.
func renderDenied(st Story) string {
	if len(st.Denied) == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\n── denied calls (%d) ──\n", len(st.Denied))
	for _, d := range st.Denied {
		fmt.Fprintf(&b, "#%d  %s  %s %s\n", d.CallSeq, d.SessionID, d.Tool, d.Args)
		fmt.Fprintf(&b, "    denied by %s — %s\n", d.RuleID, cleanReason(d.Reason))
		for _, pv := range d.Provenance {
			where := fmt.Sprintf("call #%d", pv.CallSeq)
			if pv.CallSeq == 0 {
				where = "call not found in this log"
			}
			if pv.ResultSeq > 0 {
				where += fmt.Sprintf(" (result #%d)", pv.ResultSeq)
			}
			fmt.Fprintf(&b, "    provenance: value from %s — produced at %s\n", pv.Source, where)
		}
		fmt.Fprintf(&b, "    causal story: %s\n", causalLine(d))
	}
	return b.String()
}
