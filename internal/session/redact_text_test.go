package session

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestRedactionMasksJSONCarriedAsResultText pins the M15.1 change. An MCP result
// usually carries its payload as text — {"content":[{"type":"text","text":"…"}]} —
// so a sensitive key inside that string used to be recorded in plain text even
// though the argument copy of the same value was masked. A whole JSON document in
// a result now gets the same key-based treatment, at any nesting depth, including
// escaped JSON inside JSON.
func TestRedactionMasksJSONCarriedAsResultText(t *testing.T) {
	cases := []struct {
		name   string
		text   string // the value of content[0].text
		secret string
	}{
		{"object", `{"api_key":"TEXTKEY0001"}`, "TEXTKEY0001"},
		{"object with space", `{"password": "TEXTKEY0002"}`, "TEXTKEY0002"},
		{"camelCase key", `{"clientSecret":"TEXTKEY0003"}`, "TEXTKEY0003"},
		{"nested object", `{"outer":{"inner":{"accessKey":"TEXTKEY0004"}}}`, "TEXTKEY0004"},
		{"object in array", `{"rows":[{"id":1},{"session_key":"TEXTKEY0005"}]}`, "TEXTKEY0005"},
		{"array document", `[{"token":"TEXTKEY0006"}]`, "TEXTKEY0006"},
		{"escaped json in json", `{"payload":"{\"api_key\":\"TEXTKEY0007\"}"}`, "TEXTKEY0007"},
		{"double escaped", `{"a":"{\"b\":\"{\\\"passphrase\\\":\\\"TEXTKEY0008\\\"}\"}"}`, "TEXTKEY0008"},
		{"padded with whitespace", "\n  {\"api_key\":\"TEXTKEY0009\"}  \n", "TEXTKEY0009"},
		// Reviewer cases (M15.1): a credential pattern elsewhere in the document must not
		// short-circuit key masking; a document carried as a JSON *string* must be reached;
		// a byte-order mark must not defeat the parse.
		{"pattern elsewhere does not suppress keys", `{"note":"AKIAIOSFODNN7EXAMPLE","password": "TEXTKEY0010"}`, "TEXTKEY0010"},
		{"document carried as a json string", `"{\"password\": \"TEXTKEY0011\"}"`, "TEXTKEY0011"},
		{"leading BOM", "\ufeff{\"password\":\"TEXTKEY0012\"}", "TEXTKEY0012"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(map[string]any{
				"content": []map[string]any{{"type": "text", "text": tc.text}},
			})
			if err != nil {
				t.Fatal(err)
			}
			got := redactValue(t, string(raw))
			if strings.Contains(got, tc.secret) {
				t.Fatalf("%s survived result-text redaction: %s", tc.secret, got)
			}
			if !strings.Contains(got, Redacted) {
				t.Fatalf("expected a %s marker, got %s", Redacted, got)
			}
		})
	}
}

// TestRedactionLeavesInnocuousValuesReadable is the counterweight: redaction that
// eats ordinary data destroys the evidence a user came to read.
func TestRedactionLeavesInnocuousValuesReadable(t *testing.T) {
	text := `{"path":"/fictional/alpha.md","size":42,"title":"Quarterly report","ok":true}`
	raw, _ := json.Marshal(map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
	})
	got := redactValue(t, string(raw))
	for _, want := range []string{"/fictional/alpha.md", "Quarterly report", "42"} {
		if !strings.Contains(got, want) {
			t.Fatalf("innocuous value %q was lost: %s", want, got)
		}
	}
	if strings.Contains(got, Redacted) {
		t.Fatalf("nothing sensitive was present, but a marker appeared: %s", got)
	}
	// Fidelity: when nothing matched, the text is stored exactly as received — not
	// re-serialised, not re-ordered. A redactor that tidies everything it touches
	// silently rewrites evidence.
	var envelope struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal([]byte(got), &envelope); err != nil {
		t.Fatalf("output is not valid JSON: %v (%s)", err, got)
	}
	if envelope.Content[0].Text != text {
		t.Fatalf("untouched text was rewritten: got %q want %q", envelope.Content[0].Text, text)
	}
}

// Redaction must be deterministic: the same input twice produces the same bytes,
// or replay matching and the hash chain stop meaning anything.
func TestRedactionTextIsDeterministic(t *testing.T) {
	text := `{"b":2,"api_key":"DETERMINISM1","a":{"z":"ok"}}`
	first := NewRedactor().RedactText(text)
	for i := 0; i < 8; i++ {
		if got := NewRedactor().RedactText(text); got != first {
			t.Fatalf("run %d differs:\n%s\n%s", i, first, got)
		}
	}
}
