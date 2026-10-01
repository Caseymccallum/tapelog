// Package schemafire is tapelog's inbound schema firewall: tool-call
// arguments are validated against the JSON Schema the MCP server itself
// advertised in the tool's `inputSchema`. "Allowing a tool name isn't
// enough — risk hides in the payload" (docs/POLICY.md). Validation is
// fail-open for tools with no (or broken) schemas — there is nothing
// trustworthy to validate against — and fail-closed for arguments that
// provably don't match a known schema.
package schemafire

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// Validator compiles and applies per-tool input schemas.
type Validator struct {
	mu      sync.Mutex
	schemas map[string]*jsonschema.Schema
	broken  map[string]string // tool -> why its schema could not compile
	strict  bool              // deny calls to broken-schema tools instead of skipping
}

// New creates an empty Validator (fail-open for broken schemas).
func New() *Validator {
	return &Validator{schemas: map[string]*jsonschema.Schema{}, broken: map[string]string{}}
}

// SetStrict enables strict mode: a tool whose advertised inputSchema
// cannot be compiled is treated as hostile — calls to it are denied with
// rule_id "schema-firewall" instead of silently skipping validation
// (default fail-open is documented in docs/POLICY.md).
func (v *Validator) SetStrict(on bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.strict = on
}

// Set registers the inputSchema for a tool (extracted from its MCP
// descriptor). A server-provided schema that fails to compile is recorded
// as broken and skipped (fail-open by default; strict mode turns broken
// schemas into denials — see SetStrict).
func (v *Validator) Set(tool string, schemaJSON json.RawMessage) error {
	if len(schemaJSON) == 0 {
		return nil
	}
	compiler := jsonschema.NewCompiler()
	// The schema document is referenced by an opaque URN; its own $schema
	// keyword selects the draft.
	url := "urn:tapelog:inputschema:" + tool
	doc, err := jsonschema.UnmarshalJSON(strings.NewReader(string(schemaJSON)))
	if err != nil {
		return v.markBroken(tool, fmt.Errorf("schemafire: tool %q: %w", tool, err))
	}
	if err := compiler.AddResource(url, doc); err != nil {
		return v.markBroken(tool, fmt.Errorf("schemafire: tool %q: %w", tool, err))
	}
	sch, err := compiler.Compile(url)
	if err != nil {
		return v.markBroken(tool, fmt.Errorf("schemafire: tool %q: %w", tool, err))
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	v.schemas[tool] = sch
	delete(v.broken, tool)
	return nil
}

// markBroken records an uncompilable advertised schema and returns the
// error for logging.
func (v *Validator) markBroken(tool string, err error) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.broken[tool] = err.Error()
	return err
}

// Check validates one tool call's arguments. Tools without a registered
// schema pass (nothing to validate against). Tools whose advertised schema
// failed to compile pass unless strict mode is on.
func (v *Validator) Check(tool string, args json.RawMessage) (ok bool, reason string) {
	v.mu.Lock()
	sch, known := v.schemas[tool]
	broken := v.broken[tool]
	strict := v.strict
	v.mu.Unlock()
	if !known {
		if strict && broken != "" {
			return false, fmt.Sprintf("tool advertised an uncompilable inputSchema and strict schemas are on: %s", compact(broken))
		}
		return true, ""
	}

	var value any
	if len(args) == 0 {
		value = map[string]any{}
	} else if err := json.Unmarshal(args, &value); err != nil {
		return false, fmt.Sprintf("arguments are not valid JSON: %v", err)
	}
	if err := sch.Validate(value); err != nil {
		return false, fmt.Sprintf("arguments violate the tool's inputSchema: %s", compact(err.Error()))
	}
	return true, ""
}

// compact shortens schema errors for one-line audit reasons.
func compact(s string) string {
	s = strings.ReplaceAll(s, "\n", "; ")
	if len(s) > 300 {
		s = s[:300] + "…"
	}
	return s
}
