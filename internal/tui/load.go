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
	Payload string // pretty-printed JSON payload
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

// Load parses a session log into renderable items + summary.
func Load(path string) ([]Item, Summary, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, Summary{}, err
	}
	defer f.Close()

	var items []Item
	var sum Summary
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
	return items, sum, sc.Err()
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

// Plain renders a non-interactive listing (used by --plain and CI).
func Plain(items []Item, sum Summary) string {
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
	return b.String()
}
