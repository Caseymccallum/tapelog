package report

import (
	"strings"
	"testing"
)

func TestWriteJUnit(t *testing.T) {
	results := []Result{
		{Name: "ok scenario", File: "pass.yaml", Asserts: 2},
		{Name: "bad scenario", File: "fail.yaml", Asserts: 3, Failures: []Failure{
			{Check: "taint", Detail: "send_http after read_secrets"},
		}},
	}
	var sb strings.Builder
	if err := WriteJUnit(&sb, results); err != nil {
		t.Fatal(err)
	}
	out := sb.String()
	for _, want := range []string{
		`<?xml version="1.0" encoding="UTF-8"?>`,
		`tests="2"`, `failures="1"`,
		`name="ok scenario"`, `name="bad scenario"`,
		`message="taint"`, `send_http after read_secrets`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("junit output missing %q:\n%s", want, out)
		}
	}
	// Passing scenario must not carry a <failure>.
	if strings.Contains(out[strings.Index(out, `name="ok scenario"`):strings.Index(out, `name="bad scenario"`)], "<failure") {
		t.Fatalf("passing scenario must not contain <failure>:\n%s", out)
	}
}

func TestGitHubAnnotations(t *testing.T) {
	results := []Result{
		{Name: "bad", File: "fail.yaml", Failures: []Failure{
			{Check: "order", Detail: "call 2 before\ncall 1 (100% done)"},
		}},
		{Name: "fine", File: "pass.yaml"},
	}
	lines := GitHubAnnotations(results)
	if len(lines) != 1 {
		t.Fatalf("want 1 annotation, got %v", lines)
	}
	got := lines[0]
	for _, want := range []string{"::error ", "file=fail.yaml", "title=tapelog test", "%0A", "%25"} {
		if !strings.Contains(got, want) {
			t.Fatalf("annotation missing %q: %s", want, got)
		}
	}
	if strings.Contains(got, "\n") {
		t.Fatalf("annotation must be single-line: %q", got)
	}
}
