# Tapelog

**The flight recorder and deterministic replay for AI agents.**
`rr` for tool-calling agents — record every MCP tool call, enforce policy at the boundary, and replay any session.

> **Status: v0.1 under active construction.** Working name (`tapelog`); final name TBD before publication — see [docs/ROADMAP.md](docs/ROADMAP.md).

## Why

Agents can read files, run commands, and call the network on our behalf — and today, when something goes wrong, we debug by printf-ing JSON blobs. Enterprise "agent governance" platforms exist (see [RESEARCH.md](RESEARCH.md) for the full landscape), but nobody ships the developer-grade basics:

1. **Record everything** — a tamper-evident, hash-chained session log of every tool call, result, and policy verdict.
2. **Enforce a hard boundary** — declarative allow/deny/confirm policy on tool calls with *explainable* deny reasons (a fully prompt-injected agent must not exceed its delegated authority).
3. **Replay anything** — VCR-style deterministic re-execution of sessions for debugging, regression tests, and policy what-if analysis.

## How it works

```
agent harness (Claude Code, Codex CLI, any MCP client)
      │ MCP (stdio / HTTP)
      ▼
┌──────────────────────────┐
│  tapelog (this tool)    │  1. intercept tools/call
│  ┌────────────────────┐  │  2. policy verdict: allow / confirm / deny (+ reason)
│  │ policy engine      │  │  3. hash-chained session log (redacted)
│  │ (Cedar / YAML)     │  │  4. forward to the real MCP server
│  └────────────────────┘  │
└──────────────────────────┘
      │ MCP
      ▼
real MCP servers (filesystem, git, fetch, ...)
```

See [ARCHITECTURE.md](ARCHITECTURE.md) and [docs/THREAT_MODEL.md](docs/THREAT_MODEL.md).

## Quick start

```bash
go install github.com/tapelog-dev/tapelog/cmd/tapelog@latest   # once published

# Record a session while proxying a real MCP server:
tapelog record --policy policy.yaml --log session.jsonl -- npx -y @modelcontextprotocol/server-filesystem .

# One boundary across MANY servers (stdio + HTTP), one session log:
tapelog mux --config mux.yaml --policy policy.yaml --log session.jsonl

# Verify the log is untampered:
tapelog verify session.jsonl

# Replay the session as a hermetic MCP server (agent regression tests, CI):
tapelog replay session.jsonl --strict

# Compare two sessions (e.g. replay vs. live, or before/after a change):
tapelog diff session-a.jsonl session-b.jsonl

# Inspect a session (interactive TUI, or --plain for CI):
tapelog inspect session.jsonl

# Export the session as an OpenTelemetry trace (OTLP or stdout):
tapelog export otel session.jsonl --endpoint http://localhost:4318/v1/traces

# Test a policy against sample tool calls:
tapelog policy test --policy policy.yaml --calls samples.jsonl

# Policy regression test: replay verdicts against a candidate policy
# (exits non-zero if any verdict would change):
tapelog policy whatif --policy candidate-policy.yaml session.jsonl
```

## Documentation

| Document | What |
|---|---|
| [ARCHITECTURE.md](ARCHITECTURE.md) | Components, data flow, trust boundaries |
| [docs/THREAT_MODEL.md](docs/THREAT_MODEL.md) | What we defend against — and what we honestly don't |
| [docs/POLICY.md](docs/POLICY.md) | Policy language reference (rules, scoped grants, Cedar `where`) |
| [spec/](spec/) | **The Agent Session Log Format spec** — normative rules, JSON Schema, conformance test vectors |
| [docs/ROADMAP.md](docs/ROADMAP.md) | Week-by-week build plan |
| [docs/launch/SHOW-HN.md](docs/launch/SHOW-HN.md) | Launch kit: post draft, checklist, talking points |
| [adapters/](adapters/) | TypeScript & Python config adapters (thin, dependency-free) |
| [examples/rogue-agent/](examples/rogue-agent/) | The 60-second attack-story demo |
| [RESEARCH.md](RESEARCH.md) / [STACK.md](STACK.md) | Market research & stack decisions |
| [docs/adr/](docs/adr/) | Architecture decision records |
| [CONTRIBUTING.md](CONTRIBUTING.md) | How to contribute |
| [GOVERNANCE.md](GOVERNANCE.md) / [SECURITY.md](SECURITY.md) | Project governance & vulnerability reporting |

## Design principles

- **Local-first.** No hosted service, no phone-home. Your traces stay on your machine.
- **Record everything, enforce what you can prove, replay the rest.** No security theater.
- **Untrusted-model assumption.** The model may be fully prompt-injected; the boundary must still hold.
- **Interoperable by default.** MCP spec `2026-07-28`, OpenTelemetry GenAI semantic conventions, language-neutral log schema.
- **Reuse over reinvention.** See the [bill of materials](STACK.md).

## License

Apache-2.0 — see [LICENSE](LICENSE).
