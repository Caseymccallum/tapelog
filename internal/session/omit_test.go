package session

import (
	"encoding/json"
	"strings"
	"testing"
)

// --omit-result-text must keep the shape of a result and none of its payload.
func TestOmitResultTextKeepsShapeDropsPayload(t *testing.T) {
	raw := json.RawMessage(`{"content":[{"type":"text","text":"secret payload here","annotations":{"audience":"user"}},` +
		`{"type":"resource","resource":{"uri":"file:///fictional/a.md"}}],"isError":false,"usage":{"tokens":42}}`)
	got := OmitResultText(raw)

	var out struct {
		Content []struct {
			Type        string         `json:"type"`
			Text        string         `json:"text"`
			Annotations map[string]any `json:"annotations"`
			Resource    map[string]any `json:"resource"`
		} `json:"content"`
		IsError bool           `json:"isError"`
		Usage   map[string]any `json:"usage"`
	}
	if err := json.Unmarshal(got, &out); err != nil {
		t.Fatalf("omitted result is not valid JSON: %v (%s)", err, got)
	}
	if strings.Contains(string(got), "secret payload here") {
		t.Fatalf("payload survived omission: %s", got)
	}
	if out.Content[0].Type != "text" || out.Content[1].Type != "resource" {
		t.Fatalf("content item types lost: %s", got)
	}
	if out.Content[0].Text != Omitted {
		t.Fatalf("text = %q, want the omission marker", out.Content[0].Text)
	}
	if out.IsError {
		t.Fatalf("isError flipped")
	}
	if out.Usage["tokens"] != float64(42) {
		t.Fatalf("structural number lost: %s", got)
	}
	if out.Content[1].Resource["uri"] != "file:///fictional/a.md" {
		t.Fatalf("resource uri lost: %s", got)
	}
	// Fail-closed: a label the mode does not recognise as structure is payload.
	if out.Content[0].Annotations["audience"] != Omitted {
		t.Fatalf("unrecognised label was kept: %s", got)
	}
}

// Payload hiding under keys a whitelist would not name, and payload that is not a
// string: fail-closed means every one of these is blanked.
func TestOmitResultTextBlanksUnanticipatedPayload(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		gone string
	}{
		{"unknown key", `{"foo":"SECRETVALUE"}`, "SECRETVALUE"},
		{"nested unknown", `{"structuredContent":{"password":"SECRETVALUE"}}`, "SECRETVALUE"},
		{"object under output", `{"output":{"nested":"SECRETVALUE"}}`, "SECRETVALUE"},
		{"array under body", `{"body":["SECRETVALUE"]}`, "SECRETVALUE"},
		{"number under text", `{"text":123456789}`, "123456789"},
		{"array under text", `{"content":[{"type":"text","text":["SECRETVALUE"]}]}`, "SECRETVALUE"},
		{"bare array document", `[{"note":"SECRETVALUE"}]`, "SECRETVALUE"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := string(OmitResultText(json.RawMessage(tc.raw)))
			if strings.Contains(got, tc.gone) {
				t.Fatalf("payload survived omission: %s", got)
			}
		})
	}
}

// An error result carries its message where a test might forget to look.
func TestOmitResultTextBlanksErrorMessage(t *testing.T) {
	got := string(OmitResultText(json.RawMessage(`{"code":-32000,"message":"upstream failed for SECRETVALUE"}`)))
	if strings.Contains(got, "SECRETVALUE") {
		t.Fatalf("error message survived omission: %s", got)
	}
	if !strings.Contains(got, "-32000") {
		t.Fatalf("error code lost: %s", got)
	}
}

// Payload that does not parse is replaced wholesale: recording bytes we cannot read
// is the failure this mode exists to prevent.
func TestOmitResultTextRefusesUnparseablePayload(t *testing.T) {
	for _, raw := range []string{``, `{"broken":`, `not json at all SECRETVALUE`} {
		got := string(OmitResultText(json.RawMessage(raw)))
		if strings.Contains(got, "SECRETVALUE") || strings.Contains(got, "broken") {
			t.Fatalf("unparseable payload leaked (%q -> %s)", raw, got)
		}
		if !strings.Contains(got, "unparseable") {
			t.Fatalf("unparseable payload not marked (%q -> %s)", raw, got)
		}
	}
}

// A bare string result has no structure to keep, so it is payload.
func TestOmitResultTextBlanksBareString(t *testing.T) {
	got := string(OmitResultText(json.RawMessage(`"just a string with SECRETVALUE"`)))
	if strings.Contains(got, "SECRETVALUE") {
		t.Fatalf("bare string survived: %s", got)
	}
}

// The marker has to be recognisable to the readers that already handle redaction,
// which is why it starts with Redacted rather than inventing a new class of absence.
func TestOmittedMarkerIsRecognisableAsRedacted(t *testing.T) {
	if !strings.Contains(Omitted, Redacted) {
		t.Fatalf("Omitted %q does not contain Redacted %q", Omitted, Redacted)
	}
}
