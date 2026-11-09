package session

import (
	"encoding/json"
	"strings"
	"testing"
)

// Independent review found that the sensitive-name list only matched the exact
// spellings it listed, so a passphrase or a camelCase key was written into the
// log verbatim while its snake_case sibling was masked. These are the four
// spellings that leaked; the list is now matched against a separator-free,
// lowercased key.
func TestRedactJSONSensitiveFieldSpellings(t *testing.T) {
	values := map[string]string{
		"passphrase":  "correct-horse-battery-staple",
		"privateKey":  "-----BEGIN PRIVATE KEY-----body-----END PRIVATE KEY-----",
		"accessKey":   "AKIAIOSFODNN7EXAMPLE",
		"session_key": "sk-live-0123456789abcdef0123456789abcdef",
		"Private-Key": "hyphenated",
		"API_KEY":     "shouty",
	}
	for key, value := range values {
		body, err := json.Marshal(map[string]string{key: value})
		if err != nil {
			t.Fatal(err)
		}
		out := NewRedactor().RedactJSON(body)
		if strings.Contains(string(out), value) {
			t.Fatalf("%s: value survived redaction: %s", key, out)
		}
		if !strings.Contains(string(out), Redacted) {
			t.Fatalf("%s: no redaction marker in %s", key, out)
		}
	}
}

// The normalisation must not turn ordinary keys into secrets: over-redaction
// would silently destroy evidence a user came to read.
func TestRedactJSONLeavesOrdinaryKeysReadable(t *testing.T) {
	for _, key := range []string{"path", "author", "session_id", "hostname", "content", "reason"} {
		body, err := json.Marshal(map[string]string{key: "readable value"})
		if err != nil {
			t.Fatal(err)
		}
		out := NewRedactor().RedactJSON(body)
		if !strings.Contains(string(out), "readable value") {
			t.Fatalf("%s: ordinary key was redacted: %s", key, out)
		}
	}
}
