package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Caseymccallum/tapelog/internal/web"
	"github.com/spf13/cobra"
	"io"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

var errIncompleteBehaviour = errors.New("analysis is incomplete; review coverage and integrity")

// behaviourIDPattern is the recording boundary's identifier shape, applied again
// at the command line so a flag cannot name something that is not an id.
var behaviourIDPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)

func safeTerminal(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return '�'
		}
		return r
	}, s)
}
func workbenchLink(name string) string {
	return "http://127.0.0.1:8923/#session=" + url.PathEscape(name)
}

// conversationsOut renders the conversation groups. Counts only: no score, no
// invented completion, and the unattributed remainder is shown as one.
func conversationsOut(cmd *cobra.Command, r *web.WorkflowResult, only string) error {
	m, _ := r.Result.(map[string]any)
	groups, _ := m["conversations"].([]any)
	out := cmd.OutOrStdout()
	fmt.Fprint(out, "Behaviour conversations · ", r.Status)
	fmt.Fprintln(out)
	nums := func(row map[string]any, k string) int {
		f, _ := row[k].(float64)
		return int(f)
	}
	shown := 0
	for _, g := range groups {
		row, ok := g.(map[string]any)
		if !ok {
			continue
		}
		id, _ := row["id"].(string)
		if only != "" && id != only {
			continue
		}
		shown++
		attributed, _ := row["attributed"].(bool)
		label := id
		if !attributed {
			label = id + "  (client declared no conversation)"
		}
		tools := ""
		if ts, ok := row["tools"].([]any); ok {
			for i, v := range ts {
				if s, ok := v.(string); ok {
					if i > 0 {
						tools += ", "
					}
					// A tool name is client-controlled text: strip terminal
					// control sequences before printing it.
					tools += safeTerminal(s)
				}
			}
		}
		fmt.Fprint(out, "  ", label)
		fmt.Fprintln(out)
		fmt.Fprintf(out, "    %s · %s · %s · %s · %s · %s\n",
			plural(nums(row, "calls"), "call"),
			plural(nums(row, "results"), "result"),
			plural(nums(row, "unanswered"), "unanswered call"),
			plural(nums(row, "denials"), "denial"),
			plural(nums(row, "failures"), "failure"),
			plural(nums(row, "ambiguous_calls"), "ambiguous call"))
		if tools != "" {
			fmt.Fprintf(out, "    tools: %s\n", tools)
		}
		// The timeline is what the card shows; printing only counts left a
		// terminal reader with strictly less than a browser reader (M13 F10).
		printTimeline(out, row, 20, "    ")
	}
	if shown == 0 {
		fmt.Fprintln(out, "  no conversation matched")
		return nil
	}
	if rec, ok := m["recurrence"].([]any); ok && len(rec) > 0 {
		fmt.Fprintln(out, "  recurrence across conversations:")
		for _, v := range rec {
			row, ok := v.(map[string]any)
			if !ok {
				continue
			}
			tool, _ := row["tool"].(string)
			fmt.Fprintf(out, "    %s in %d conversations (%d calls)", tool, nums(row, "conversations"), nums(row, "calls"))
			fmt.Fprintln(out)
		}
	}
	fmt.Fprintln(out, "  Attribution is client-declared: it groups activity and is not a session boundary or proof of completion.")
	return nil
}

// printTimeline renders a bounded timeline from the versioned JSON payload. The
// text view names the limit instead of silently printing a prefix.
func printTimeline(out io.Writer, row map[string]any, max int, indent string) {
	entries, ok := row["timeline"].([]any)
	if !ok || len(entries) == 0 {
		return
	}
	fmt.Fprintf(out, "%stimeline:\n", indent)
	shown := 0
	for _, v := range entries {
		if shown >= max {
			break
		}
		e, ok := v.(map[string]any)
		if !ok {
			continue
		}
		shown++
		fmt.Fprintf(out, "%s  #%d  %s  %s  %s\n", indent, uintOf(e["seq"]), safeTerminal(stringOf(e["tool"])), statusWord(e), safeTerminal(whenOf(e["ts"])))
	}
	if len(entries) > shown {
		fmt.Fprintf(out, "%s  … %d more call(s); use `tapelog behaviour conversation` for the full timeline.\n", indent, len(entries)-shown)
	}
}

// statusWord is the one-word evidence state of a call. A missing result is
// never printed as a success or a failure.
func statusWord(e map[string]any) string {
	if stringOf(e["verdict"]) == "deny" {
		return "denied"
	}
	if b, _ := e["is_error"].(bool); b {
		return "error"
	}
	if b, _ := e["ambiguous"].(bool); b {
		return "result owner unknown"
	}
	if b, _ := e["answered"].(bool); !b {
		return "no recorded result"
	}
	return "answered"
}

func stringOf(v any) string {
	s, _ := v.(string)
	return s
}

func uintOf(v any) uint64 {
	f, _ := v.(float64)
	if f < 0 {
		return 0
	}
	return uint64(f)
}

func whenOf(v any) string {
	s, _ := v.(string)
	if s == "" {
		return "no recorded time"
	}
	return s
}

// conversationOut renders one conversation: every counted fact, the evidence
// limits, and its ordered timeline page.
func conversationOut(cmd *cobra.Command, r *web.WorkflowResult) error {
	m, _ := r.Result.(map[string]any)
	out := cmd.OutOrStdout()
	fmt.Fprint(out, "Conversation ", safeTerminal(stringOf(m["conversation"])), " · ", r.Status)
	fmt.Fprintln(out)
	summary, _ := m["summary"].(map[string]any)
	if summary == nil {
		fmt.Fprintln(out, "  no summary available")
		return nil
	}
	fmt.Fprintf(out, "  %s · %s · %s · %s · %s · %s\n",
		plural(intOf(summary["calls"]), "call"),
		plural(intOf(summary["results"]), "result"),
		plural(intOf(summary["unanswered"]), "unanswered call"),
		plural(intOf(summary["denials"]), "denial"),
		plural(intOf(summary["failures"]), "failure"),
		plural(intOf(summary["ambiguous_calls"]), "ambiguous call"))
	if tc, ok := summary["tool_calls"].([]any); ok && len(tc) > 0 {
		parts := make([]string, 0, len(tc))
		for _, v := range tc {
			row, ok := v.(map[string]any)
			if !ok {
				continue
			}
			parts = append(parts, safeTerminal(stringOf(row["tool"]))+" ("+strconv.Itoa(intOf(row["calls"]))+")")
		}
		fmt.Fprintf(out, "  tools: %s\n", strings.Join(parts, ", "))
	}
	if files, ok := summary["files"].([]any); ok && len(files) > 0 {
		names := make([]string, 0, len(files))
		for _, v := range files {
			names = append(names, safeTerminal(stringOf(v)))
		}
		fmt.Fprintf(out, "  recordings: %s\n", strings.Join(names, ", "))
	}
	for _, note := range stringList(m["evidence"]) {
		fmt.Fprintf(out, "  ! %s\n", note)
	}
	printTimeline(out, m, 500, "  ")
	fmt.Fprintf(out, "  showing %d of %d call(s) from offset %d\n", timelineLen(m), intOf(m["total"]), intOf(m["offset"]))
	fmt.Fprintln(out, "  Attribution is client-declared: it groups activity and is not a session boundary or proof of completion.")
	return nil
}

// compareConversationsOut prints the observed differences and, separately, the
// reasons the comparison cannot be read as a judgement.
func compareConversationsOut(cmd *cobra.Command, r *web.WorkflowResult) error {
	m, _ := r.Result.(map[string]any)
	cmp, _ := m["comparison"].(map[string]any)
	out := cmd.OutOrStdout()
	fmt.Fprint(out, "Conversation comparison · ", r.Status)
	fmt.Fprintln(out)
	if cmp == nil {
		fmt.Fprintln(out, "  no comparison available")
		return nil
	}
	if ok, _ := cmp["comparable"].(bool); !ok {
		fmt.Fprintf(out, "  not comparable: %s\n", safeTerminal(stringOf(cmp["explanation"])))
		return nil
	}
	a, _ := cmp["a"].(map[string]any)
	b, _ := cmp["b"].(map[string]any)
	fmt.Fprintf(out, "  A = %s   B = %s\n", safeTerminal(stringOf(a["id"])), safeTerminal(stringOf(b["id"])))
	fmt.Fprintf(out, "  %-28s %8s %8s %8s\n", "", "A", "B", "change")
	if metrics, ok := cmp["metrics"].([]any); ok {
		for _, v := range metrics {
			row, ok := v.(map[string]any)
			if !ok {
				continue
			}
			fmt.Fprintf(out, "  %-28s %8d %8d %+8d\n", stringOf(row["metric"]), intOf(row["a"]), intOf(row["b"]), intOf(row["change"]))
		}
	}
	if findings := stringList(cmp["findings"]); len(findings) > 0 {
		fmt.Fprintln(out, "  observed differences:")
		for _, f := range findings {
			fmt.Fprintf(out, "    · %s\n", f)
		}
	}
	if caveats := stringList(cmp["caveats"]); len(caveats) > 0 {
		fmt.Fprintln(out, "  limits of this comparison:")
		for _, c := range caveats {
			fmt.Fprintf(out, "    · %s\n", c)
		}
	}
	return nil
}

func timelineLen(m map[string]any) int {
	entries, ok := m["timeline"].([]any)
	if !ok {
		return 0
	}
	return len(entries)
}

func intOf(v any) int {
	f, _ := v.(float64)
	return int(f)
}

func stringList(v any) []string {
	list, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		if s, ok := item.(string); ok {
			out = append(out, safeTerminal(s))
		}
	}
	return out
}

func workflowOut(cmd *cobra.Command, r *web.WorkflowResult, jsonMode bool) error {
	m := r.Result.(map[string]any)
	links := map[string]string{}
	if trend, ok := m["trend"].([]any); ok {
		for _, v := range trend {
			if row, ok := v.(map[string]any); ok {
				if name, ok := row["file"].(string); ok {
					links[name] = workbenchLink(name)
				}
			}
		}
	}
	if r.Analysis == "compare" {
		if a, ok := m["reference"].(map[string]any); ok {
			if b, ok := m["current"].(map[string]any); ok {
				an, _ := a["file"].(string)
				bn, _ := b["file"].(string)
				if an != "" && bn != "" {
					links["comparison"] = "http://127.0.0.1:8923/#behaviour&a=" + url.QueryEscape(an) + "&b=" + url.QueryEscape(bn)
				}
			}
		}
	}
	m["workbench_links"] = links
	if jsonMode {
		e := json.NewEncoder(cmd.OutOrStdout())
		e.SetIndent("", "  ")
		if err := e.Encode(r); err != nil {
			return err
		}
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "Behaviour %s · %s\n", r.Analysis, r.Status)
		if c, ok := m["counts"].(map[string]any); ok {
			fmt.Fprintf(cmd.OutOrStdout(), "Conversations %.0f · sessions %.0f · calls %.0f · results %.0f · tool errors %.0f · denied calls %.0f · deny decisions %.0f · unanswered %.0f\n", c["conversations"], c["sessions"], c["calls"], c["results"], c["failures"], c["denies"], c["deny_decisions"], c["unanswered"])
		}
		for _, key := range []string{"failures", "repeats", "new_failures", "resolved_failures", "new_denials", "resolved_denials", "new_repeats", "resolved_repeats", "changed_repeats"} {
			if list, ok := m[key].([]any); ok && len(list) > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "\n%s (%d)\n", strings.ReplaceAll(key, "_", " "), len(list))
				for _, v := range list {
					item, ok := v.(map[string]any)
					if !ok {
						continue
					}
					tool := item["tool"]
					if tool == nil {
						tool = item["tools"]
					}
					fmt.Fprintf(cmd.OutOrStdout(), "  %s", safeTerminal(fmt.Sprint(tool)))
					for _, field := range []string{"id", "kind", "confidence", "rule", "occurrences", "sessions", "reference", "current", "reference_occurrences", "current_occurrences"} {
						if x, ok := item[field]; ok {
							fmt.Fprintf(cmd.OutOrStdout(), " · %s %s", field, safeTerminal(fmt.Sprint(x)))
						}
					}
					fmt.Fprintln(cmd.OutOrStdout())
				}
			}
		}
		if lib, ok := m["library"].(map[string]any); ok {
			fmt.Fprintf(cmd.OutOrStdout(), "\nCoverage: %v analysed / %v discovered; capped %v, truncated %v\n", lib["analysed"], lib["files"], lib["capped"], lib["truncated"])
			if skipped, ok := lib["skipped"].([]any); ok {
				for _, x := range skipped {
					b, _ := json.Marshal(x)
					fmt.Fprintln(cmd.OutOrStdout(), "  Skipped:", safeTerminal(string(b)))
				}
			}
		}
		if r.Analysis == "compare" {
			for _, key := range []string{"reference", "current"} {
				if row, ok := m[key].(map[string]any); ok {
					name, _ := row["file"].(string)
					fmt.Fprintf(cmd.OutOrStdout(), "%s: %s\n", key, safeTerminal(name))
				}
			}
			fmt.Fprintln(cmd.OutOrStdout(), links["comparison"])
		}
		if rates, ok := m["rates"].([]any); ok {
			fmt.Fprintln(cmd.OutOrStdout(), "\nRates (reference → candidate; denominators in parentheses):")
			for _, x := range rates {
				v := x.(map[string]any)
				if v["unit"] == "per_minute" {
					fmt.Fprintf(cmd.OutOrStdout(), "  %s: %v (%v calls / %v ms) → %v (%v calls / %v ms)\n", safeTerminal(fmt.Sprint(v["metric"])), v["reference"], v["reference_n"], v["reference_duration_ms"], v["current"], v["current_n"], v["current_duration_ms"])
				} else {
					fmt.Fprintf(cmd.OutOrStdout(), "  %s: %v (%v) → %v (%v)\n", safeTerminal(fmt.Sprint(v["metric"])), v["reference"], v["reference_n"], v["current"], v["current_n"])
				}
			}
		}
		if calls, ok := m["call_comparison"].(map[string]any); ok {
			fmt.Fprintf(cmd.OutOrStdout(), "\nCall comparison: %v changes; repeated-key ambiguity %v; sequence differs %v\n", calls["change_count"], calls["ambiguous_duplicates"], calls["sequence_differs"])
			if v, ok := calls["reason"]; ok {
				fmt.Fprintln(cmd.OutOrStdout(), safeTerminal(fmt.Sprint(v)))
			}
			if changes, ok := calls["changes"].([]string); ok {
				for _, x := range changes {
					fmt.Fprintln(cmd.OutOrStdout(), "  "+safeTerminal(x))
				}
			}
		}
		if tools, ok := m["tool_usage"].(map[string]any); ok {
			fmt.Fprintln(cmd.OutOrStdout(), "\nTool usage (reference → candidate):")
			names := make([]string, 0, len(tools))
			for name := range tools {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				v := tools[name].(map[string]any)
				fmt.Fprintf(cmd.OutOrStdout(), "  %s: %v → %v\n", safeTerminal(name), v["reference"], v["candidate"])
			}
		}
		if trend, ok := m["trend"].([]any); ok && len(trend) > 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "\nInvestigate (start tapelog web --dir with the same directory separately):")
			for _, v := range trend {
				if row, ok := v.(map[string]any); ok {
					if name, ok := row["file"].(string); ok {
						fmt.Fprintf(cmd.OutOrStdout(), "%s · integrity %v · complete %v · unanswered %v\n  %s\n", safeTerminal(name), row["integrity"], row["complete"], row["unanswered"], workbenchLink(name))
					}
				}
			}
		}
		fmt.Fprintln(cmd.OutOrStdout(), "Recorded boundary evidence only. Differences and repeats are observations, not automatic regressions. Error message text is omitted from this output.")
	}
	if r.Status != "complete" {
		return errIncompleteBehaviour
	}
	return nil
}
func newBehaviourCmd() *cobra.Command {
	root := &cobra.Command{Use: "behaviour", Short: "Analyse recorded agent behaviour without executing tools", Long: "Analyse local recordings using the workbench's behaviour detectors. Results describe the recorded MCP boundary only."}
	for _, kind := range []string{"summary", "repeats", "failures", "conversations"} {
		kind := kind
		var dir string
		var asJSON bool
		var convFilter string
		cmd := &cobra.Command{Use: kind + " [recording.jsonl...]", Short: map[string]string{"summary": "Summarise a recording library", "repeats": "Find repeated recorded activity", "failures": "Group recorded tool failures", "conversations": "Group recorded activity by client-declared conversation"}[kind], RunE: func(cmd *cobra.Command, args []string) error {
			r, err := web.BehaviourWorkflow(dir, args, kind)
			if err != nil {
				return err
			}
			if kind == "conversations" && convFilter != "" {
				// The filter used to apply to the text view only, and an invalid
				// or reserved id selected the unattributed remainder as though it
				// were a conversation (M14 review F4, F9). Both are refused rather
				// than half-applied.
				if !behaviourIDPattern.MatchString(convFilter) || convFilter == "(unattributed)" {
					return fmt.Errorf("--conversation must be a plain client-declared id (1-128 bytes of A-Za-z0-9._:-); the unattributed remainder is not a conversation")
				}
				if asJSON {
					return fmt.Errorf("--conversation narrows the text view only; use `tapelog behaviour conversation --dir … --conversation %s --json` for one conversation's versioned payload", convFilter)
				}
				return conversationsOut(cmd, r, convFilter)
			}
			return workflowOut(cmd, r, asJSON)
		}}
		cmd.Flags().StringVar(&dir, "dir", "", "directory containing recordings (required)")
		cmd.Flags().BoolVar(&asJSON, "json", false, "emit versioned JSON to stdout")
		if kind == "conversations" {
			cmd.Flags().StringVar(&convFilter, "conversation", "", "show only this client-declared conversation id")
		}
		root.AddCommand(cmd)
	}
	var dir, ref, cur string
	var asJSON bool
	compare := &cobra.Command{Use: "compare", Short: "Compare a candidate with an explicit reference", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		r, err := web.BehaviourWorkflowCompare(dir, ref, cur)
		if err != nil {
			return err
		}
		return workflowOut(cmd, r, asJSON)
	}}
	compare.Flags().StringVar(&dir, "dir", "", "directory containing both recordings (required)")
	compare.Flags().StringVar(&ref, "reference", "", "reference recording filename")
	compare.Flags().StringVar(&cur, "candidate", "", "candidate recording filename")
	compare.Flags().BoolVar(&asJSON, "json", false, "emit versioned JSON to stdout")
	root.AddCommand(compare)

	// M14: the conversation as the unit of investigation. Both commands read the
	// same analysis the workspace does, so the terminal and the card cannot
	// disagree about counts, order or who a call belongs to.
	var convDir, convID string
	var convJSON bool
	var convOffset, convLimit int
	conversation := &cobra.Command{
		Use:   "conversation [recording.jsonl...]",
		Short: "Inspect one client-declared conversation, including its full ordered timeline",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := web.ConversationWorkflow(convDir, args, convID, convOffset, convLimit)
			if err != nil {
				return err
			}
			if convJSON {
				return workflowOut(cmd, r, true)
			}
			return conversationOut(cmd, r)
		},
	}
	conversation.Flags().StringVar(&convDir, "dir", "", "directory containing recordings (required)")
	conversation.Flags().StringVar(&convID, "conversation", "", "client-declared conversation id (required)")
	conversation.Flags().IntVar(&convOffset, "offset", 0, "timeline entries to skip")
	conversation.Flags().IntVar(&convLimit, "limit", 100, "timeline entries to show (max 500)")
	conversation.Flags().BoolVar(&convJSON, "json", false, "emit versioned JSON to stdout")
	root.AddCommand(conversation)

	var cmpDir, cmpA, cmpB string
	var cmpJSON bool
	compareConversations := &cobra.Command{
		Use:   "compare-conversations [recording.jsonl...]",
		Short: "Compare two client-declared conversations on recorded counts",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := web.CompareConversationsWorkflow(cmpDir, args, cmpA, cmpB)
			if err != nil {
				return err
			}
			if cmpJSON {
				return workflowOut(cmd, r, true)
			}
			return compareConversationsOut(cmd, r)
		},
	}
	compareConversations.Flags().StringVar(&cmpDir, "dir", "", "directory containing recordings (required)")
	compareConversations.Flags().StringVar(&cmpA, "a", "", "first conversation id")
	compareConversations.Flags().StringVar(&cmpB, "b", "", "second conversation id")
	compareConversations.Flags().BoolVar(&cmpJSON, "json", false, "emit versioned JSON to stdout")
	root.AddCommand(compareConversations)
	return root
}

// plural renders one count with its noun, so output never reads "1 denials".
func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(n) + " " + noun + "s"
}
