// Package fuzz is `tapelog fuzz`: the boundary hardening lab. It mutates
// recorded sessions with attack-shaped mutations (tool-name spoofing,
// argument tampering, toxic-flow reordering) and re-evaluates the policy
// on every mutant. The oracle is verdict consistency: a mutation that
// turns a deny into an allow is a boundary bypass — a policy hole that a
// real attacker could use. Findings are reproducible (mutant + rule id)
// and carry indicative MITRE ATLAS tactics (docs/TESTING.md).
package fuzz

import "encoding/json"

// Finding is one boundary bypass discovered by mutation.
type Finding struct {
	Operator   string          `json:"operator"`
	Tool       string          `json:"tool"`
	Mutation   string          `json:"mutation"`             // human description
	Args       json.RawMessage `json:"args,omitempty"`       // mutated args (when relevant)
	WasVerdict string          `json:"was_verdict"`          // verdict before mutation
	NowVerdict string          `json:"now_verdict"`          // verdict after mutation
	RuleID     string          `json:"rule_id"`              // rule that produced the baseline deny
	Detail     string          `json:"detail"`
	ATLAS      string          `json:"atlas"` // indicative ATLAS tactic
}

// Mutation is one candidate transformation of a recorded call.
type Mutation struct {
	Operator string
	Tool     string          // mutated tool name ("" = unchanged)
	Args     json.RawMessage // mutated args (nil = unchanged)
	Desc     string
	ATLAS    string
	Swap     bool // reorder: swap this interaction with the next
}

// Operators are the mutation families (all on by default).
var Operators = []string{
	"tool_case",      // case variants of the tool name
	"tool_space",     // whitespace / zero-width padding
	"tool_homoglyph", // Cyrillic lookalikes
	"tool_traversal", // path-like tool names
	"tool_namespace", // server__tool prefix confusion
	"arg_traversal",  // ../ escapes in string arguments
	"arg_type",       // string args flipped to numbers
	"arg_overflow",   // oversized string args
	"arg_unicode",    // zero-width / homoglyph value evasions
	"arg_boundary",   // empty / null / extreme values
	"arg_encoding",   // base64 / URL-encoded values
	"swap_adjacent",  // toxic-flow reordering
	"swap_rotate",    // non-adjacent reorder (sink before source)
}
