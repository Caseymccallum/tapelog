package plugin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

const wasmPath = "../../examples/plugins/argguard/argguard.wasm"

func TestTightenLattice(t *testing.T) {
	cases := []struct {
		from, to string
		want     bool
	}{
		{"allow", "confirm", true},
		{"allow", "deny", true},
		{"confirm", "deny", true},
		{"deny", "allow", false},   // loosen: forbidden
		{"deny", "confirm", false}, // loosen: forbidden
		{"confirm", "allow", false},
		{"allow", "allow", false}, // unchanged is not tightening
	}
	for _, c := range cases {
		if got := TightenAllowed(c.from, c.to); got != c.want {
			t.Errorf("TightenAllowed(%s, %s) = %v, want %v", c.from, c.to, got, c.want)
		}
	}
}

func loadExample(t *testing.T) *Plugin {
	t.Helper()
	p, err := Load(context.Background(), wasmPath)
	if err != nil {
		t.Fatalf("load example plugin (build it with examples/plugins/argguard/build.ps1): %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })
	return p
}

func TestExampleVerdictHook(t *testing.T) {
	p := loadExample(t)
	ctx := context.Background()

	// Forbidden marker in args -> deny.
	out, err := p.CallVerdict(ctx, []byte(`{"tool":"read_file","args":{"path":"forbidden.txt"},"verdict":"allow"}`))
	if err != nil {
		t.Fatal(err)
	}
	var resp Response
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Verdict != "deny" || !strings.Contains(resp.Reason, "argguard") {
		t.Fatalf("want argguard deny, got %+v", resp)
	}

	// exec tool with allow -> tighten to confirm.
	out, _ = p.CallVerdict(ctx, []byte(`{"tool":"exec_shell","args":{},"verdict":"allow"}`))
	resp = Response{} // fresh: Unmarshal does not clear old fields
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Verdict != "confirm" {
		t.Fatalf("want confirm tightening, got %+v", resp)
	}

	// Benign call -> unchanged (empty response).
	out, _ = p.CallVerdict(ctx, []byte(`{"tool":"read_file","args":{"path":"/tmp/x"},"verdict":"allow"}`))
	resp = Response{}
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Verdict != "" {
		t.Fatalf("want unchanged, got %+v", resp)
	}
}

func TestExampleRedactHook(t *testing.T) {
	p := loadExample(t)
	if !p.HasRedact() {
		t.Fatal("example plugin should have redact_hook")
	}
	out, err := p.CallRedact(context.Background(), []byte(`{"text":"key ACME-abc123 and more"}`))
	if err != nil {
		t.Fatal(err)
	}
	var resp struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Text != "key [REDACTED] and more" {
		t.Fatalf("redact_hook output: %q", resp.Text)
	}
}

func TestChainTightenEnforcesLattice(t *testing.T) {
	// Even though the example plugin returns "confirm" for exec tools, a
	// request already at "deny" must stay "deny".
	c, err := NewChain(context.Background(), []string{wasmPath})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	req := Request{Tool: "exec_shell", Args: json.RawMessage(`{}`), Verdict: "deny", RuleID: "r", Reason: "already denied"}
	got := c.Tighten(context.Background(), req)
	if got.Verdict != "deny" {
		t.Fatalf("plugin loosened a deny: %+v", got)
	}
}

func TestEmptyChainPassthrough(t *testing.T) {
	var c *Chain
	req := Request{Verdict: "allow", Reason: "r"}
	if got := c.Tighten(context.Background(), req); got.Verdict != "allow" {
		t.Fatalf("nil chain must pass through: %+v", got)
	}
	if got := c.RedactText(context.Background(), "text"); got != "text" {
		t.Fatalf("nil chain must pass through: %q", got)
	}
}
