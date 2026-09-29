package session

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCanonicalJSONSortsKeys(t *testing.T) {
	a, err := CanonicalJSON(json.RawMessage(`{"b":1,"a":{"z":true,"y":null}}`))
	if err != nil {
		t.Fatal(err)
	}
	b, err := CanonicalJSON(json.RawMessage(`{"a":{"y":null,"z":true},"b":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatalf("canonical forms differ:\n%s\n%s", a, b)
	}
	if a != `{"a":{"y":null,"z":true},"b":1}` {
		t.Fatalf("unexpected canonical form: %s", a)
	}
}

func TestCanonicalJSONNoHTMLEscape(t *testing.T) {
	got, err := CanonicalJSON(json.RawMessage(`{"s":"a<b>&c"}`))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, `\u003c`) {
		t.Fatalf("HTML escaping leaked into canonical form: %s", got)
	}
}

func TestCanonicalJSONPreservesNumbers(t *testing.T) {
	got, err := CanonicalJSON(json.RawMessage(`{"n":12345678901234567890}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "12345678901234567890") {
		t.Fatalf("number literal altered: %s", got)
	}
}

func TestRedactStringPatterns(t *testing.T) {
	r := NewRedactor()
	cases := map[string]string{
		"key is AKIA1234567890ABCDEF ok":    "key is [REDACTED] ok",
		"token ghp_" + strings.Repeat("a", 36) + "!": "token [REDACTED]!",
		"Authorization: Bearer " + strings.Repeat("x", 20): "Authorization: [REDACTED]",
		"nothing sensitive here": "nothing sensitive here",
	}
	for in, want := range cases {
		if got := r.RedactString(in); got != want {
			t.Errorf("RedactString(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRedactJSONSensitiveFields(t *testing.T) {
	r := NewRedactor()
	in := json.RawMessage(`{"user":"casey","password":"hunter2","nested":{"api_key":"sk-` + strings.Repeat("a", 40) + `"},"list":["ok"]}`)
	got := string(r.RedactJSON(in))
	for _, secret := range []string{"hunter2", strings.Repeat("a", 40)} {
		if strings.Contains(got, secret) {
			t.Errorf("secret %q survived redaction: %s", secret, got)
		}
	}
	if !strings.Contains(got, "casey") || !strings.Contains(got, "ok") {
		t.Errorf("non-secret data lost: %s", got)
	}
}

func TestRedactJSONDeterministic(t *testing.T) {
	r := NewRedactor()
	in := json.RawMessage(`{"b":"token ghp_` + strings.Repeat("a", 36) + `","a":"x"}`)
	if string(r.RedactJSON(in)) != string(r.RedactJSON(in)) {
		t.Fatal("redaction is not deterministic")
	}
}
