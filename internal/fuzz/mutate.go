package fuzz

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Caseymccallum/tapelog/internal/policy"
	"github.com/Caseymccallum/tapelog/internal/replay"
)

// mutate produces the attack-shaped mutations for one interaction.
func mutate(it replay.Interaction, enabled map[string]bool) []Mutation {
	var out []Mutation
	add := func(op, tool string, args json.RawMessage, desc, atlas string) {
		if enabled[op] {
			out = append(out, Mutation{Operator: op, Tool: tool, Args: args, Desc: desc, ATLAS: atlas})
		}
	}
	name := it.Tool

	if enabled["tool_case"] {
		add("tool_case", strings.ToUpper(name), nil, "upper-case name", "Defense Evasion")
		if len(name) > 0 {
			add("tool_case", strings.ToUpper(name[:1])+name[1:], nil, "capitalized name", "Defense Evasion")
		}
	}
	if enabled["tool_space"] {
		add("tool_space", name+" ", nil, "trailing space", "Defense Evasion")
		add("tool_space", name+"\u200b", nil, "zero-width suffix", "Defense Evasion")
	}
	if enabled["tool_homoglyph"] {
		hg := strings.NewReplacer("e", "е", "a", "а", "o", "о", "p", "р", "c", "с").Replace(name)
		if hg != name {
			add("tool_homoglyph", hg, nil, "Cyrillic lookalikes", "Defense Evasion")
		}
	}
	if enabled["tool_traversal"] {
		add("tool_traversal", "../"+name, nil, "traversal prefix", "Defense Evasion")
		add("tool_traversal", name+"/..", nil, "traversal suffix", "Defense Evasion")
	}
	if enabled["tool_namespace"] {
		add("tool_namespace", "x__"+name, nil, "namespaced as x__"+name, "Defense Evasion")
		if i := strings.Index(name, "__"); i > 0 {
			add("tool_namespace", name[i+2:], nil, "namespace stripped", "Defense Evasion")
		}
	}

	// Argument tampering: rewrite every string value in the args object.
	var obj map[string]any
	if err := json.Unmarshal(it.Args, &obj); err == nil {
		for key, val := range obj {
			s, isStr := val.(string)
			if !isStr {
				continue
			}
			if enabled["arg_traversal"] {
				for _, m := range map[string]string{
					"../" + s:        "traversal prefix",
					"/x/../" + s:     "indirect traversal",
					s + "/../../etc": "traversal suffix",
				} {
					if mutated, err := withValue(obj, key, m); err == nil {
						add("arg_traversal", "", mutated, key+": "+m, "Execution")
					}
				}
			}
			if enabled["arg_type"] {
				if mutated, err := withValue(obj, key, 12345); err == nil {
					add("arg_type", "", mutated, key+": string -> number", "Defense Evasion")
				}
			}
			if enabled["arg_overflow"] {
				if mutated, err := withValue(obj, key, s+strings.Repeat("A", 8192)); err == nil {
					add("arg_overflow", "", mutated, key+": +8KB filler", "Impact")
				}
			}
			if enabled["arg_unicode"] {
				for _, m := range map[string]string{
					insertZeroWidth(s): "zero-width split",
					strings.NewReplacer("e", "е", "a", "а", "o", "о").Replace(s): "homoglyph value",
				} {
					if m == s {
						continue
					}
					if mutated, err := withValue(obj, key, m); err == nil {
						add("arg_unicode", "", mutated, key+": "+clip(m), "Defense Evasion")
					}
				}
			}
			if enabled["arg_boundary"] {
				for _, b := range []string{"", "0", s + "\x00", s + "\nDROP"} {
					if mutated, err := withValue(obj, key, b); err == nil {
						add("arg_boundary", "", mutated, key+": string boundary", "Defense Evasion")
					}
				}
			}
			if enabled["arg_encoding"] {
				if mutated, err := withValue(obj, key, b64(s)); err == nil {
					add("arg_encoding", "", mutated, key+": base64", "Defense Evasion")
				}
				if mutated, err := withValue(obj, key, urlEncode(s)); err == nil {
					add("arg_encoding", "", mutated, key+": url-encoded", "Defense Evasion")
				}
			}
		}
	}
	return out
}

// withValue copies obj with one key set to value and re-marshals it.
func withValue(obj map[string]any, key string, value any) (json.RawMessage, error) {
	cp := make(map[string]any, len(obj))
	for k, v := range obj {
		cp[k] = v
	}
	cp[key] = value
	return json.Marshal(cp)
}

// verdicts walks the sequence through the policy pipeline in order
// (flows + taint first, per-call rules otherwise) — what-if semantics.
func verdicts(seq []replay.Interaction, ev policy.Evaluator) []string {
	var taint policy.TaintState
	fc, hasFlows := ev.(policy.FlowChecker)
	out := make([]string, len(seq))
	for i, it := range seq {
		if hasFlows {
			if dec, applied := fc.CheckFlow(&taint, it.Tool); applied {
				out[i] = string(dec.Verdict)
				if dec.Verdict == policy.VerdictAllow {
					taint.Record(it.Tool)
				}
				continue
			}
		}
		dec := ev.Evaluate(policy.Request{Tool: it.Tool, Args: it.Args})
		out[i] = string(dec.Verdict)
		if dec.Verdict == policy.VerdictAllow {
			taint.Record(it.Tool)
		}
	}
	return out
}

// ruleFor finds the deny rule id for a tool under the evaluator's policy.
func ruleFor(ev policy.Evaluator, tool string, args json.RawMessage) string {
	dec := ev.Evaluate(policy.Request{Tool: tool, Args: args})
	if dec.Verdict == policy.VerdictDeny {
		return dec.RuleID
	}
	return ""
}

// mkFinding builds a Finding describing a bypass.
func mkFinding(m Mutation, it replay.Interaction, was, now, ruleID string) Finding {
	desc := m.Desc
	if m.Tool != "" {
		desc = fmt.Sprintf("%s -> %q", desc, m.Tool)
	}
	return Finding{
		Operator: m.Operator, Tool: it.Tool, Mutation: desc, Args: m.Args,
		WasVerdict: was, NowVerdict: now, RuleID: ruleID,
		Detail: fmt.Sprintf("%s flipped deny -> %s", it.Tool, now),
		ATLAS:  m.ATLAS,
	}
}
