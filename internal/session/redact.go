package session

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
)

// Redacted is the replacement marker written in place of secret material.
const Redacted = "[REDACTED]"

// sensitiveFieldFragments mark JSON object keys whose values must be redacted.
var sensitiveFieldFragments = []string{
	"password", "passwd", "secret", "token", "api_key", "apikey",
	"credential", "authorization", "private_key", "access_key", "secret_key",
}

// defaultSecretPatterns match common credential shapes in free text.
// Best-effort by design — see docs/THREAT_MODEL.md ("log confidentiality").
var defaultSecretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`),                                  // AWS access key ID
	regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{36,}\b`),                        // GitHub tokens
	regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{22,}\b`),                      // GitHub fine-grained PAT
	regexp.MustCompile(`\bxox[baprs]-[A-Za-z0-9-]{10,}\b`),                      // Slack tokens
	regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._~+/=-]{16,}`),                // Authorization: Bearer
	regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]*?-----END [A-Z ]*PRIVATE KEY-----`), // PEM keys
	regexp.MustCompile(`\bsk-ant-[A-Za-z0-9_-]{20,}\b`),                         // Anthropic API keys
	regexp.MustCompile(`\bsk-[A-Za-z0-9]{32,}\b`),                               // OpenAI-style keys
	regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\b`), // JWTs
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
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			if isSensitiveField(k) {
				out[k] = Redacted
				continue
			}
			out[k] = r.walk(val)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, elem := range t {
			out[i] = r.walk(elem)
		}
		return out
	case string:
		return r.RedactString(t)
	default:
		return v
	}
}

// isSensitiveField reports whether a JSON key name suggests secret material.
func isSensitiveField(key string) bool {
	k := strings.ToLower(key)
	for _, frag := range sensitiveFieldFragments {
		if strings.Contains(k, frag) {
			return true
		}
	}
	return false
}
