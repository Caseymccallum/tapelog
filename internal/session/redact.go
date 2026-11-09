package session

import (
	"bytes"
	"encoding/json"
	"io"
	"regexp"
	"strings"
)

// Redacted is the replacement marker written in place of secret material.
const Redacted = "[REDACTED]"

// Omitted is recorded in place of a result body when the recorder runs with
// --omit-result-text. It deliberately starts with Redacted so every existing
// "this content was withheld" path (the Regression Lab's redacted_content code,
// the workbench banner) treats an omitted body the same way — the point of the
// mode is that the bytes never reach the disk, not that a new class of absence
// has to be understood by every reader.
const Omitted = Redacted + " (result text omitted at record time)"

// Bounds for JSON-inside-text redaction. An MCP tool result is usually a string
// (content[].text), so a document the server returned as text gets the same
// key-based treatment as structured arguments — but only within these limits,
// and the behaviour past them is stated rather than surprising:
//
//   - text longer than maxEmbeddedJSONBytes is left to the pattern pass only
//   - a document nested deeper than maxJSONDepth is not descended into further
//   - text that is not a whole, well-formed JSON document is left to the
//     pattern pass only (no attempt to fix or partially parse it)
//
// Past a limit the value is still recorded, still pattern-redacted, and still
// readable — a bounded gap that is documented, not a silent one.
const (
	maxEmbeddedJSONBytes = 256 * 1024
	maxJSONDepth         = 24
)

// sensitiveFieldFragments mark JSON object keys whose values must be redacted.
// They are matched against a separator-free, lowercased key (see
// normaliseFieldName), so "private_key", "privateKey" and "private-key" are one
// entry here. Spellings that name the same secret under a different word —
// passphrase, session_key — are listed explicitly rather than guessed at.
var sensitiveFieldFragments = []string{
	"password", "passwd", "passphrase", "secret", "token", "apikey",
	"credential", "authorization", "privatekey", "accesskey", "secretkey",
	"sessionkey", "authtoken",
}

// defaultSecretPatterns match common credential shapes in free text.
// Best-effort by design — see docs/THREAT_MODEL.md ("log confidentiality").
var defaultSecretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`),                                                       // AWS access key ID
	regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{36,}\b`),                                             // GitHub tokens
	regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{22,}\b`),                                           // GitHub fine-grained PAT
	regexp.MustCompile(`\bxox[baprs]-[A-Za-z0-9-]{10,}\b`),                                           // Slack tokens
	regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._~+/=-]{16,}`),                                     // Authorization: Bearer
	regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]*?-----END [A-Z ]*PRIVATE KEY-----`), // PEM keys
	regexp.MustCompile(`\bsk-ant-[A-Za-z0-9_-]{20,}\b`),                                              // Anthropic API keys
	regexp.MustCompile(`\bsk-[A-Za-z0-9]{32,}\b`),                                                    // OpenAI-style keys
	regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\b`),          // JWTs
}

// Redactor masks secret material deterministically: the same input always
// produces the same output, which keeps replay matching stable.
type Redactor struct {
	patterns []*regexp.Regexp
	textHook func(string) string // optional extra redaction (e.g. plugins)
}

// NewRedactor returns a Redactor with the built-in secret patterns.
func NewRedactor() *Redactor {
	return &Redactor{patterns: defaultSecretPatterns}
}

// RegisterTextRedactor installs an additional redaction pass (applied
// after the built-in patterns). Used by the plugin chain; one hook only.
func (r *Redactor) RegisterTextRedactor(fn func(string) string) {
	r.textHook = fn
}

// RedactString replaces every pattern match with Redacted, then applies
// the registered extra redactor.
func (r *Redactor) RedactString(s string) string {
	out := s
	for _, p := range r.patterns {
		out = p.ReplaceAllString(out, Redacted)
	}
	if r.textHook != nil {
		out = r.textHook(out)
	}
	return out
}

// RedactJSON redacts a JSON value and returns it re-serialized canonically
// (sorted keys), so redacted payloads hash deterministically.
//   - values of sensitive object keys become "[REDACTED]"
//   - all strings pass through RedactString
func (r *Redactor) RedactJSON(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage("{}")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		// Not valid JSON: fall back to string-level redaction of the raw
		// bytes and store as a properly-escaped JSON string (raw text may
		// contain quotes — hand-quoting would produce invalid JSON and
		// break the very event meant to record the evidence).
		b, _ := json.Marshal(r.RedactString(string(raw)))
		return json.RawMessage(b)
	}
	cleaned := r.walk(v)
	b, err := marshalCanonical(cleaned)
	if err != nil {
		out, _ := json.Marshal(Redacted)
		return json.RawMessage(out)
	}
	return b
}

// walk recursively redacts a decoded JSON value.
func (r *Redactor) walk(v any) any {
	out, _ := r.walkDepth(v, 0)
	return out
}

// walkDepth redacts a decoded JSON value and reports whether anything changed.
// A string value gets the pattern pass, then — if it is itself a whole JSON
// document within bounds — the key-based pass, recursively. Reporting "changed"
// is what lets RedactText leave untouched text byte-identical instead of
// re-serialising every JSON-looking result.
func (r *Redactor) walkDepth(v any, depth int) (any, bool) {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		changed := false
		for k, val := range t {
			if isSensitiveField(k) {
				out[k] = Redacted
				changed = true
				continue
			}
			nv, c := r.walkDepth(val, depth)
			out[k] = nv
			changed = changed || c
		}
		return out, changed
	case []any:
		out := make([]any, len(t))
		changed := false
		for i, elem := range t {
			nv, c := r.walkDepth(elem, depth)
			out[i] = nv
			changed = changed || c
		}
		return out, changed
	case string:
		// Embedded JSON first: if the pattern pass ran first and matched anything,
		// returning there would skip key-based masking for the rest of the document,
		// so `{"note":"AKIA…","password":"x"}` would keep the password. The parse
		// applies the pattern pass to every leaf string anyway.
		if doc, ok := r.redactEmbeddedJSON(t, depth); ok {
			return doc, true
		}
		if red := r.RedactString(t); red != t {
			return red, true
		}
		return t, false
	default:
		return v, false
	}
}

// RedactText redacts a free-text value: known credential shapes first, then —
// when the whole value is one well-formed JSON document within the size and
// depth bounds — the same key-based rules the argument path uses. Text that is
// not a whole document (prose, malformed JSON, a JSON fragment) is only pattern
// redacted: Tapelog does not claim to find arbitrary secrets in prose.
func (r *Redactor) RedactText(s string) string {
	if doc, ok := r.redactEmbeddedJSON(s, 0); ok {
		return doc
	}
	return r.RedactString(s)
}

// redactEmbeddedJSON parses text as a JSON document and applies the key-based
// rules to it, returning (text, true) only when something was actually redacted.
// Every refusal path returns ("", false): the caller then keeps the original
// bytes, which keeps the recording faithful instead of merely tidier.
func (r *Redactor) redactEmbeddedJSON(s string, depth int) (string, bool) {
	if depth > maxJSONDepth || len(s) > maxEmbeddedJSONBytes {
		return "", false
	}
	trimmed := strings.TrimSpace(s)
	// A byte-order mark is legal to ignore and turns up when a server echoes a file
	// verbatim; without stripping it the first-character test below rejects a
	// perfectly good document.
	trimmed = strings.TrimPrefix(trimmed, "\ufeff")
	if len(trimmed) < 2 {
		return "", false
	}
	// A JSON *string* document is a document encoded as a string — the wire shape of
	// {"text":"{\"password\":\"x\"}"} once the outer escape is removed. Handle it by
	// recursing into the decoded string and re-encoding, so the result stays a string.
	if trimmed[0] != '{' && trimmed[0] != '[' && trimmed[0] != '"' {
		return "", false
	}
	dec := json.NewDecoder(strings.NewReader(trimmed))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return "", false
	}
	// Trailing content means it is not one document; a prefix is not evidence
	// of a whole, so do not partially redact it.
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return "", false
	}
	if inner, isString := v.(string); isString {
		doc, ok := r.redactEmbeddedJSON(inner, depth+1)
		if !ok {
			return "", false
		}
		b, err := json.Marshal(doc)
		if err != nil {
			return "", false
		}
		return string(b), true
	}
	cleaned, changed := r.walkDepth(v, depth+1)
	if !changed {
		return "", false
	}
	b, err := marshalCanonical(cleaned)
	if err != nil {
		return "", false
	}
	return string(b), true
}

// normaliseFieldName lowercases a key and removes separators, so the fragment
// list does not have to enumerate every casing and spelling of the same name:
// camelCase, snake_case and kebab-case all collapse to one form.
func normaliseFieldName(key string) string {
	k := strings.ToLower(key)
	k = strings.ReplaceAll(k, "_", "")
	k = strings.ReplaceAll(k, "-", "")
	k = strings.ReplaceAll(k, ".", "")
	return k
}

// isSensitiveField reports whether a JSON key name suggests secret material.
func isSensitiveField(key string) bool {
	k := normaliseFieldName(key)
	for _, frag := range sensitiveFieldFragments {
		if strings.Contains(k, frag) {
			return true
		}
	}
	return false
}
