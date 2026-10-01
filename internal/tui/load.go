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
}

// Summary aggregates a session at a glance.
type Summary struct {
	Events    int
	Calls     int
	Allowed   int
	Confirmed int
	Denied    int
	Drift     int
}

// Load parses a session log into renderable items, summary, and the
// causal story (denied calls with provenance).
func Load(path string) ([]Item, Summary, Story, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, Summary{}, Story{}, err
	}
	defer f.Close()

	var events []session.Event
	var items []Item
	var sum Summary
	blobs := session.NewBlobStore(session.DefaultBlobDir(path))
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var e session.Event
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue
		}
		// Show blob-backed payloads as content (verified), never as the
		// digest placeholder (spec/session-log-v0.md §6.2 rule 2).
		e.Payload = session.ResolvePayload(e.Payload, blobs)
		events = append(events, e)
		item := itemFromEvent(e)
		items = append(items, item)
		sum.Events++
		switch item.Kind {
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
	fmt.Fprintf(&b, "\n%d events · %d calls · %d allowed · %d confirmed · %d denied · %d drift\n",
		sum.Events, sum.Calls, sum.Allowed, sum.Confirmed, sum.Denied, sum.Drift)
	b.WriteString(renderDenied(story))
	return b.String()
}
