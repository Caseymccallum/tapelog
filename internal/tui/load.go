package tui

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/Caseymccallum/tapelog/internal/session"
)

// Kind classifies an event for color rendering.
type Kind int

const (
	KindInfo Kind = iota
	KindCall
	KindResult
	KindAllow
	KindConfirm
	KindDeny
	KindError
)

// Item is one rendered session event.
type Item struct {
	Seq     uint64
	TS      string
	Type    string
	Label   string
	Kind    Kind
	Payload string   // pretty-printed JSON payload
	Extra   []string // causal provenance lines (deny events)
	// Attribution is the client-declared conversation id for this event
	// (empty when none was declared). It is a claim, not a fact, and the
	// viewer labels it as such.
	Attribution string
}

// Summary aggregates a session at a glance.
type Summary struct {
	Events    int
	Calls     int
	Allowed   int
	Confirmed int
	Denied    int
	Drift     int
	Failed    int
	Discovery int
	Started   bool
	Ended     bool
	// Integrity is "verified", "invalid" or "unreadable" (hash chain).
	// Structure is "valid", "invalid", "incomplete" or "unreadable" (one
	// session log, boundary observed). Both are reported, never fatal.
	Integrity string
	Structure string
	// StructureProblems carries the shared checker's own defect codes, so the
	// rendered verdict names the same problem `verify` and the workbench API name
	// instead of a canned sentence that can be false (an empty file is not a
	// "mixed session ids" file).
	StructureProblems []string
	ChainEvents       int
	ChainHead         string
}

// Load parses a session log into renderable items, summary, and the
// causal story (denied calls with provenance).
func Load(path string) ([]Item, Summary, Story, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, Summary{}, Story{}, err
	}

	var events []session.Event
	var items []Item
	var sum Summary
	blobs := session.NewBlobStore(session.DefaultBlobDir(path))

	// Integrity and structure are reported, never fatal: inspect is a forensic
	// viewer and must open tampered or structurally invalid evidence.
	if vr, verr := session.Verify(bytes.NewReader(data)); verr != nil {
		sum.Integrity = "unreadable"
	} else if vr != nil && vr.OK() {
		sum.Integrity = "verified"
		sum.ChainEvents = vr.Events
		sum.ChainHead = vr.LastHash
	} else {
		sum.Integrity = "invalid"
	}
	if sr, serr := session.InspectStructure(bytes.NewReader(data)); serr != nil {
		sum.Structure = "unreadable"
	} else {
		for _, p := range sr.Problems {
			sum.StructureProblems = append(sum.StructureProblems, p.Describe())
		}
		switch {
		case !sr.Valid():
			sum.Structure = "invalid"
		case !sr.Complete():
			sum.Structure = "incomplete"
		default:
			sum.Structure = "valid"
		}
	}

	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	lineNumber := 0
	for sc.Scan() {
		lineNumber++
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var e session.Event
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			return nil, Summary{}, Story{}, fmt.Errorf("cannot render %s: invalid session JSON at line %d; run tapelog verify on this file", path, lineNumber)
		}
		if e.Seq == 0 || e.Type == "" {
			return nil, Summary{}, Story{}, fmt.Errorf("invalid session event at line %d; expected a sequence and event type", lineNumber)
		}
		// Show blob-backed payloads as content (verified), never as the
		// digest placeholder (spec/session-log-v0.md §6.2 rule 2).
		e.Payload = session.ResolvePayload(e.Payload, blobs)
		events = append(events, e)
		item := itemFromEvent(e)
		items = append(items, item)
		sum.Events++
		switch e.Type {
		case session.EventSessionStart:
			sum.Started = true
		case session.EventSessionEnd:
			sum.Ended = true
		case session.EventToolsList:
			sum.Discovery++
		}
		switch item.Kind {
		case KindError:
			sum.Failed++
		case KindCall:
			sum.Calls++
		case KindAllow:
			sum.Allowed++
		case KindConfirm:
			sum.Confirmed++
		case KindDeny:
			sum.Denied++
		}
		if strings.Contains(item.Label, "[DRIFT]") {
			sum.Drift++
		}
	}
	if err := sc.Err(); err != nil {
		return nil, Summary{}, Story{}, err
	}

	// Correlate denied calls with their provenance and attach the block
	// to the matching decision item (detail pane + plain rendering).
	story := buildStory(events)
	for i := range items {
		if items[i].Kind != KindDeny {
			continue
		}
		for _, d := range story.Denied {
			if d.DecSeq != items[i].Seq {
				continue
			}
			items[i].Extra = append(items[i].Extra,
				fmt.Sprintf("call #%d · session %s", d.CallSeq, d.SessionID))
			if d.Tool != "" {
				items[i].Extra = append(items[i].Extra, d.Tool+" "+d.Args)
			}
			for _, pv := range d.Provenance {
				where := fmt.Sprintf("call #%d", pv.CallSeq)
				if pv.CallSeq == 0 {
					where = "call not found in this log"
				}
				if pv.ResultSeq > 0 {
					where += fmt.Sprintf(" (result #%d)", pv.ResultSeq)
				}
				items[i].Extra = append(items[i].Extra,
					fmt.Sprintf("provenance: value from %s — produced at %s", pv.Source, where))
			}
			items[i].Extra = append(items[i].Extra, "causal story: "+causalLine(d))
			break
		}
	}
	return items, sum, story, sc.Err()
}

// itemFromEvent turns a log event into a list item.
func itemFromEvent(e session.Event) Item {
	item := Item{Seq: e.Seq, TS: e.TS, Type: string(e.Type), Kind: KindInfo, Payload: pretty(e.Payload)}
	switch e.Type {
	case session.EventSessionStart:
		var p session.SessionStartPayload
		_ = json.Unmarshal(e.Payload, &p)
		item.Label = fmt.Sprintf("harness=%s policy=%s", p.Harness, p.PolicyID)
	case session.EventToolsList:
		var p session.ToolsListPayload
		_ = json.Unmarshal(e.Payload, &p)
		item.Label = fmt.Sprintf("%d tools", len(p.Tools))
	case session.EventToolCall:
		var p session.ToolCallPayload
		_ = json.Unmarshal(e.Payload, &p)
		item.Kind = KindCall
		item.Label = fmt.Sprintf("%s %s", p.Tool, compact(p.Args))
		if p.ConversationID != "" {
			// Client-declared attribution: shown as correlation, never as
			// a session boundary.
			item.Attribution = p.ConversationID
			item.Label += fmt.Sprintf(" [conversation %s]", p.ConversationID)
		}
		if p.DescriptorDrift {
			item.Label += " [DRIFT]"
		}
	case session.EventPolicyDecision:
		var p session.PolicyDecisionPayload
		_ = json.Unmarshal(e.Payload, &p)
		item.Label = fmt.Sprintf("%s (%s) — %s", p.Verdict, p.RuleID, p.Reason)
		switch p.Verdict {
		case "allow":
			item.Kind = KindAllow
		case "confirm":
			item.Kind = KindConfirm
		default:
			item.Kind = KindDeny
		}
	case session.EventToolResult:
		var p session.ToolResultPayload
		_ = json.Unmarshal(e.Payload, &p)
		item.Kind = KindResult
		if p.IsError {
			item.Kind = KindError
			item.Label = "error result"
		} else {
			item.Label = "ok"
		}
	case session.EventSessionEnd:
		var p session.SessionEndPayload
		_ = json.Unmarshal(e.Payload, &p)
		item.Label = p.Reason
	}
	return item
}

// pretty formats a JSON payload for the detail pane.
func pretty(raw json.RawMessage) string {
	var buf bytes.Buffer
	if err := json.Indent(&buf, raw, "", "  "); err != nil {
		return string(raw)
	}
	return buf.String()
}

// compact renders args as single-line JSON, truncated for the list view.
func compact(raw json.RawMessage) string {
	s := string(raw)
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 64 {
		s = s[:61] + "..."
	}
	return s
}

// Plain renders a non-interactive listing (used by --plain and CI),
// followed by the DENIED section (causal blocks with provenance).
func Plain(items []Item, sum Summary, story Story) string {
	var b strings.Builder
	b.WriteString(Overview(sum))
	attributed := 0
	for _, it := range items {
		if it.Attribution != "" {
			attributed++
		}
	}
	if attributed > 0 {
		b.WriteString("Attribution: client-declared conversation ids (a claim, not authenticated; not a session boundary)\n")
	}
	for _, it := range items {
		marker := " "
		switch it.Kind {
		case KindDeny, KindError:
			marker = "!"
		case KindConfirm:
			marker = "?"
		case KindAllow:
			marker = "+"
		case KindCall:
			marker = ">"
		}
		fmt.Fprintf(&b, "%s %3d  %-15s %s\n", marker, it.Seq, it.Type, it.Label)
	}

	b.WriteString(renderDenied(story))
	return b.String()
}

// Overview labels observations without treating decisions as successful calls.
func Overview(sum Summary) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Session overview: %d events · %d mediated calls · %d failed results · %d discovery events\n", sum.Events, sum.Calls, sum.Failed, sum.Discovery)
	fmt.Fprintf(&b, "Policy decisions: %d allow · %d confirm · %d deny (not completed-call counts) · %d drift\n", sum.Allowed, sum.Confirmed, sum.Denied, sum.Drift)
	b.WriteString("Scope: only traffic crossing Tapelog's MCP boundary; built-in agent tools are unobserved.\n")
	fmt.Fprintf(&b, "Integrity: %s\n", integrityVerdict(sum))
	fmt.Fprintf(&b, "Structure: %s\n", structureVerdict(sum))
	fmt.Fprintf(&b, "Completeness: %s\n", completenessVerdict(sum))
	b.WriteString("Eligibility: not checked by inspect; run tapelog lab.\n")
	if sum.Events == 0 {
		b.WriteString("No session events found. Check the selected log and whether recording started.\n")
	} else if sum.Calls == 0 && sum.Discovery > 0 {
		b.WriteString("Only tool discovery observed; no mediated tool calls captured. Ask the client to call a named MCP tool. Agent activity outside MCP remains unobserved.\n")
	} else if sum.Calls == 0 {
		b.WriteString("No mediated tool calls captured. This does not mean the agent was inactive; check the client connection and selected MCP tool.\n")
	}
	if sum.Events > 0 && !sum.Ended {
		b.WriteString("No session/end recorded: recording may still be running or may have been interrupted; completeness is unknown.\n")
	}
	b.WriteString("Next: tapelog verify <log>; tapelog replay <log> --strict serves recorded results only and waits for client input. Denied/unanswered calls are replay misses. Browse: tapelog web --dir <log-directory>.\n\n")
	return b.String()
}

// integrityVerdict renders the hash-chain verdict for the overview. An empty
// file has no chain, so it is reported as EMPTY rather than VERIFIED — the
// same verdict the web API and the lab give it.
func integrityVerdict(sum Summary) string {
	switch sum.Integrity {
	case "verified":
		if sum.ChainEvents == 0 {
			return "EMPTY (no events; there is no chain)"
		}
		return fmt.Sprintf("VERIFIED (%d events, chain intact)", sum.ChainEvents)
	case "invalid":
		return "INVALID (hash chain broken — run tapelog verify <log>)"
	default:
		return "UNREADABLE"
	}
}

// structureVerdict renders the structural verdict for the overview. A file
// that is not one session log is INVALID; a file that is one but has no end is
// INCOMPLETE, never invalid.
func structureVerdict(sum Summary) string {
	switch sum.Structure {
	case "valid":
		return "VALID (one session id, session/start first, session/end last)"
	case "invalid":
		return fmt.Sprintf("INVALID (not one session log — %s)", structureDetail(sum, "mixed session ids, missing or repeated boundary"))
	case "incomplete":
		return fmt.Sprintf("INCOMPLETE (no session/end recorded — %s)", structureDetail(sum, "no_end"))
	default:
		return "UNREADABLE"
	}
}

// structureDetail renders the actual defect codes the shared checker reported.
// The fallback is only reachable for a verdict with no attached problem, which
// the checker does not currently produce.
func structureDetail(sum Summary, fallback string) string {
	if len(sum.StructureProblems) == 0 {
		return fallback
	}
	return strings.Join(sum.StructureProblems, ", ")
}

// completenessVerdict renders whether the recording's own boundary was seen.
func completenessVerdict(sum Summary) string {
	if sum.Structure == "valid" {
		return "closed (session/start … session/end)"
	}
	if sum.Events > 0 && !sum.Ended {
		return "open (no session/end recorded)"
	}
	return "unknown"
}
