package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Caseymccallum/tapelog/internal/web"
	"github.com/spf13/cobra"
	"net/url"
	"sort"
	"strings"
	"unicode"
)

var errIncompleteBehaviour = errors.New("analysis is incomplete; review coverage and integrity")

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
			fmt.Fprintf(cmd.OutOrStdout(), "Sessions %.0f · calls %.0f · results %.0f · tool errors %.0f · denied calls %.0f · deny decisions %.0f · unanswered %.0f\n", c["sessions"], c["calls"], c["results"], c["failures"], c["denies"], c["deny_decisions"], c["unanswered"])
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
						fmt.Fprintf(cmd.OutOrStdout(), "%s · integrity %v · ended %v · unanswered %v\n  %s\n", safeTerminal(name), row["integrity"], row["complete"], row["unanswered"], workbenchLink(name))
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
	for _, kind := range []string{"summary", "repeats", "failures"} {
		kind := kind
		var dir string
		var asJSON bool
		cmd := &cobra.Command{Use: kind + " [recording.jsonl...]", Short: map[string]string{"summary": "Summarise a recording library", "repeats": "Find repeated recorded activity", "failures": "Group recorded tool failures"}[kind], RunE: func(cmd *cobra.Command, args []string) error {
			r, err := web.BehaviourWorkflow(dir, args, kind)
			if err != nil {
				return err
			}
			return workflowOut(cmd, r, asJSON)
		}}
		cmd.Flags().StringVar(&dir, "dir", "", "directory containing recordings (required)")
		cmd.Flags().BoolVar(&asJSON, "json", false, "emit versioned JSON to stdout")
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
	return root
}
