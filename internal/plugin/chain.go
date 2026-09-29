package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// Chain applies loaded plugins in order. Safe for concurrent use.
type Chain struct {
	plugins []*Plugin
	warn    io.Writer // non-fatal plugin warnings (default os.Stderr)
}

// NewChain loads every plugin path (order = application order).
func NewChain(ctx context.Context, paths []string) (*Chain, error) {
	c := &Chain{warn: os.Stderr}
	for _, path := range paths {
		p, err := Load(ctx, path)
		if err != nil {
			c.Close()
			return nil, err
		}
		c.plugins = append(c.plugins, p)
	}
	return c, nil
}

// Close tears down all plugins.
func (c *Chain) Close() {
	for _, p := range c.plugins {
		_ = p.Close()
	}
}

// Empty reports whether the chain has no plugins.
func (c *Chain) Empty() bool { return c == nil || len(c.plugins) == 0 }

// Tighten runs every verdict_hook in order and returns the final
// (possibly tightened) verdict/reason. ENFORCEMENT CONTRACT: even if a
// plugin lies, the returned verdict is never weaker than the input —
// the lattice check lives here, in the host.
func (c *Chain) Tighten(ctx context.Context, req Request) Request {
	if c.Empty() {
		return req
	}
	for _, p := range c.plugins {
		in, err := json.Marshal(req)
		if err != nil {
			continue
		}
		out, err := p.CallVerdict(ctx, in)
		if err != nil {
			// Fail closed: a broken enforcement plugin denies.
			fmt.Fprintf(c.warn, "tapelog: plugin %s error (%v) — failing closed\n", p.Name(), err)
			req.Verdict = "deny"
			req.Reason += fmt.Sprintf(" (plugin %s error: failing closed)", p.Name())
			continue
		}
		var resp Response
		if err := json.Unmarshal(out, &resp); err != nil {
			fmt.Fprintf(c.warn, "tapelog: plugin %s returned invalid JSON — failing closed\n", p.Name())
			req.Verdict = "deny"
			req.Reason += fmt.Sprintf(" (plugin %s invalid output: failing closed)", p.Name())
			continue
		}
		if resp.Verdict != "" && resp.Verdict != req.Verdict {
			if !TightenAllowed(req.Verdict, resp.Verdict) {
				fmt.Fprintf(c.warn, "tapelog: plugin %s tried to loosen %s -> %s — ignored\n",
					p.Name(), req.Verdict, resp.Verdict)
			} else {
				req.Verdict = resp.Verdict
				reason := resp.Reason
				if reason == "" {
					reason = req.Reason
				}
				req.Reason = fmt.Sprintf("%s (tightened by plugin %s)", reason, p.Name())
			}
		} else if resp.Reason != "" && resp.Reason != req.Reason {
			req.Reason = fmt.Sprintf("%s (plugin %s)", resp.Reason, p.Name())
		}
	}
	return req
}

// RedactText runs every redact_hook in order over plain text. Plugin
// failures leave the text unchanged (host redaction still applies).
func (c *Chain) RedactText(ctx context.Context, text string) string {
	if c.Empty() {
		return text
	}
	current := text
	for _, p := range c.plugins {
		if !p.HasRedact() {
			continue
		}
		in, err := json.Marshal(map[string]string{"text": current})
		if err != nil {
			continue
		}
		out, err := p.CallRedact(ctx, in)
		if err != nil {
			fmt.Fprintf(c.warn, "tapelog: plugin %s redact_hook error (%v) — text unchanged\n", p.Name(), err)
			continue
		}
		var resp struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(out, &resp); err != nil {
			continue
		}
		current = resp.Text
	}
	return current
}
