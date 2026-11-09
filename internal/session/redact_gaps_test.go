package session

import (
	"encoding/json"
	"strings"
	"testing"
)

// redactValue is a helper that runs the real redactor over a JSON payload.
func redactValue(t *testing.T, raw string) string {
	t.Helper()
	out := NewRedactor().RedactJSON(json.RawMessage(raw))
	return string(out)
}

// This file holds what redaction protects and — just as deliberately — what it
// still does not. The leftovers are asserted so that closing one fails a test and
// forces the documentation to move in the same change.

// TestRedactionMasksKeysPatternsAndNesting covers what redaction actually protects:
// values under sensitive field names, credential shapes in free text, nesting and
// arrays. All of this happens before hashing, so replay matching stays stable.
func TestRedactionMasksKeysPatternsAndNesting(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string // must be absent from the output
	}{
		{"sensitive key", `{"api_key":"plainvalue123"}`, []string{"plainvalue123"}},
		{"nested sensitive key", `{"outer":{"password":"nestedvalue456"}}`, []string{"nestedvalue456"}},
		{"array of objects", `{"list":[{"token":"arrayvalue789"}]}`, []string{"arrayvalue789"}},
		{"openai-shaped string", `{"note":"sk-abcdefghijklmnopqrstuvwxyz0123456789"}`, []string{"sk-abcdefghijklmnopqrstuvwxyz0123456789"}},
		{"aws key in a message", `{"message":"failed with AKIAIOSFODNN7EXAMPLE"}`, []string{"AKIAIOSFODNN7EXAMPLE"}},
		{"bearer header", `{"header":"Authorization: Bearer abcdefghijklmnopqrstuvwx"}`, []string{"abcdefghijklmnopqrstuvwx"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := redactValue(t, tc.in)
			for _, secret := range tc.want {
				if strings.Contains(got, secret) {
					t.Fatalf("%s survived redaction: %s", secret, got)
				}
			}
			if !strings.Contains(got, Redacted) {
				t.Fatalf("expected a %s marker, got %s", Redacted, got)
			}
		})
	}
}

// TestRedactionLeftoversAreDocumented asserts the limits that remain, so a future
// change that closes them fails here and forces the docs to move with it. Each case
// is one of the stated gaps; see docs/SECURITY-AND-LIMITATIONS.md.
func TestRedactionLeftoversAreDocumented(t *testing.T) {
	inResultText := func(text string) string {
		raw, _ := json.Marshal(map[string]any{
			"content": []map[string]any{{"type": "text", "text": text}},
		})
		return redactValue(t, string(raw))
	}

	t.Run("plain prose with a bare secret still leaks", func(t *testing.T) {
		got := inResultText("the recovery phrase is PROSE0001 and nothing else")
		if !strings.Contains(got, "PROSE0001") {
			t.Fatalf("prose redaction changed; update the docs: %s", got)
		}
	})

	t.Run("malformed JSON is not repaired", func(t *testing.T) {
		got := inResultText(`{"api_key": "MALFORMED0002", broken`)
		if !strings.Contains(got, "MALFORMED0002") {
			t.Fatalf("malformed-JSON behaviour changed; update the docs: %s", got)
		}
	})

	t.Run("JSON prefix with trailing prose is not partially parsed", func(t *testing.T) {
		got := inResultText(`{"api_key": "TRAILING0003"} and then some prose`)
		if !strings.Contains(got, "TRAILING0003") {
			t.Fatalf("partial-document behaviour changed; update the docs: %s", got)
		}
	})

	t.Run("text above the size bound is pattern-only", func(t *testing.T) {
		big := `{"padding":"` + strings.Repeat("z", maxEmbeddedJSONBytes) + `","api_key":"OVERSIZE0004"}`
		got := inResultText(big)
		if !strings.Contains(got, "OVERSIZE0004") {
			t.Fatalf("oversize behaviour changed; update the docs (and consider raising the bound)")
		}
	})

	t.Run("past the string-nesting bound the document is left alone", func(t *testing.T) {
		// White-box: escaping grows exponentially with layer count, so the bound is
		// checked directly rather than by building a 28-layer string. Structural
		// nesting inside one document is not counted by this bound — encoding/json
		// bounds that, and counting it would silently stop redacting deep arguments.
		r := NewRedactor()
		if _, ok := r.redactEmbeddedJSON(`{"password":"DEPTHS0001"}`, maxJSONDepth+1); ok {
			t.Fatalf("depth bound not enforced: redacted past maxJSONDepth")
		}
		if got, ok := r.redactEmbeddedJSON(`{"password":"DEPTHS0002"}`, 0); !ok || strings.Contains(got, "DEPTHS0002") {
			t.Fatalf("depth 0 must redact: ok=%v got=%s", ok, got)
		}
	})

	t.Run("a secret used as a property name is preserved", func(t *testing.T) {
		got := inResultText(`{"SK-KEYNAMESECRET123":"value"}`)
		if !strings.Contains(got, "SK-KEYNAMESECRET123") {
			t.Fatalf("key names are documented as preserved; behaviour changed: %s", got)
		}
	})
}
