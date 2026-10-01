# Tapelog

**Record every tool call. Replay any session. Test agent behaviour deterministically.**
`rr` for tool-calling agents — a tamper-evident flight recorder and a hard policy boundary for MCP tool traffic.

> **Status:** latest release **v0.2.0** · current main **v0.4.0-dev**
> (the build version in [`internal/buildinfo`](internal/buildinfo/buildinfo.go) is the single source of truth).
> **Stable:** `record` · `mux` · `policy` · `replay` · `verify` · `test` · `fuzz` · `inspect` · `export`.
> **Experimental:** value-level taint · signed checkpoints · the MCP compat lab.
> [CHANGELOG](CHANGELOG.md) · [roadmap](docs/ROADMAP.md) · [threat model](docs/THREAT_MODEL.md)

## The 30-second version

An agent makes a dangerous call → **tapelog blocks it** → the whole
trajectory is recorded and hash-chained → **replay** it as a hermetic
server in CI → change the policy → **what-if** shows exactly which
verdicts would flip. All of that in one runnable artifact:

```powershell
# record → deny → verify → 9 trajectory assertions → what-if → replay → tamper:
powershell -File examples/trajectory-demo/demo.ps1
```

The money line: *behavioral tests pass on a tampered log; the hash chain
doesn't.*

New here? The [five-minute quickstart](docs/QUICKSTART.md) gets you from
zero to a recorded, replayed session — and then to the denial that
proves the boundary.

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

## Install

```bash
# prebuilt binaries (Windows/macOS/Linux) — GitHub Releases, signed + SBOM:
#   https://github.com/Caseymccallum/tapelog/releases

go install github.com/Caseymccallum/tapelog/cmd/tapelog@latest   # or from source
```

## Connect your agent (one config change)

tapelog sits between your MCP client and your MCP servers — you change
**one config entry**. Example for Roo Code / `.roo/mcp.json` (other
clients: [docs/CLIENT-SETUP.md](docs/CLIENT-SETUP.md)):

```json
{
  "mcpServers": {
    "filesystem-logged": {
      "command": "C:\\path\\to\\tapelog.exe",
      "args": ["record", "--log", "sessions/roo.jsonl",
               "--approval-listen", "127.0.0.1:8923",
               "--", "cmd", "/c", "npx", "-y",
               "@modelcontextprotocol/server-filesystem", "."]
    }
  }
}
```

The agent sees its tools as usual; every call now flows through policy +
a hash-chained log. First run without `--policy` = **no policy rules**
(default-allow, everything recorded) — note this is not "nothing
happens": the boundary's other protections stay active (schema firewall,
limits, drift pinning, injection scan, redaction). Note:
under a client, `confirm` verdicts go to the approval queue (web UI on
`127.0.0.1:8923`) — the interactive prompt only works in a terminal.

## What you get

Agents can read files, run commands, and call the network on our behalf — and today, when something goes wrong, we debug by printf-ing JSON blobs. Tapelog combines tamper-evident recording, policy enforcement, deterministic replay, and trajectory regression testing into one local developer workflow for MCP traffic:

1. **Record everything** — a tamper-evident, hash-chained session log of every tool call, result, and policy verdict.
2. **Enforce a hard boundary** — declarative allow/deny/confirm policy with *explainable* deny reasons, an **inbound schema firewall** (arguments must satisfy the server's own `inputSchema`), **session budgets / rate limits / payload caps**, **prompt-injection scanning of results**, cross-tool taint rules, and tool-poisoning drift detection (a fully prompt-injected agent must not exceed its delegated authority).
3. **Replay anything** — VCR-style deterministic re-execution of sessions for debugging, regression tests, and policy what-if analysis.
4. **Test and harden it** — behavioral regression over recorded trajectories (`tapelog test`), policy-boundary fuzzing (`tapelog fuzz`), and batteries-included policy packs (`packs/`) — the CI layer the eval wave doesn't have.

## Security model

The model driving the agent may be **fully prompt-injected**; the
boundary must hold anyway — a constrained agent cannot exceed the
authority policy delegates, and everything that crosses is recorded.
Tamper evidence is first-class: the hash chain catches edits, `verify
--expect` catches wholesale rewrites, and signed checkpoints prove *who*
attested the chain head and *when*. What we honestly do **not** claim
(sandbox escape, harness built-in tools, value-taint precision limits)
is written down in [docs/THREAT_MODEL.md](docs/THREAT_MODEL.md) — trust
boundaries matter, so the limits are part of the docs, not a footnote.

## Documentation

| Document | What |
|---|---|
| [docs/QUICKSTART.md](docs/QUICKSTART.md) | **Five minutes to proof** — zero to a recorded, replayed session (and the denial that proves the boundary) |
| [ARCHITECTURE.md](ARCHITECTURE.md) | Components, data flow, trust boundaries |
| [docs/THREAT_MODEL.md](docs/THREAT_MODEL.md) | What we defend against — and what we honestly don't |
| [docs/POLICY.md](docs/POLICY.md) | Policy language reference (rules, scoped grants, Cedar `where`) |
| [docs/CLIENT-SETUP.md](docs/CLIENT-SETUP.md) | **Put it in front of your agent** — Roo Code, VS Code, Claude, any stdio client |
| [docs/TESTING.md](docs/TESTING.md) | **Agent regression testing** — scenario DSL over cassettes (`tapelog test`) |
| [docs/CHECKPOINTS.md](docs/CHECKPOINTS.md) | **Signed checkpoints** — who attested the chain head, and when (transparency-witnessed) |
| [docs/PLUGINS.md](docs/PLUGINS.md) | **WASM plugins** — sandboxed verdict/redact hooks (`--plugin`, wazero) |
| [spec/](spec/) | **The Agent Session Log Format spec** — normative rules, JSON Schema, conformance test vectors |
| [docs/ROADMAP.md](docs/ROADMAP.md) | Week-by-week build plan |
| [docs/launch/](docs/launch/) | Launch kit: post draft, checklist, and the hands-on [testing guide](docs/launch/TESTING-GUIDE.md) |
| [adapters/](adapters/) | TypeScript & Python config adapters (thin, dependency-free) |
| [examples/rogue-agent/](examples/rogue-agent/) | The 60-second attack-story demo |
| [examples/trajectory-demo/](examples/trajectory-demo/) | The canonical demo: record → deny → verify → assertions → replay → tamper |
| [RESEARCH.md](RESEARCH.md) / [STACK.md](STACK.md) / [docs/RESEARCH-EVOLUTION.md](docs/RESEARCH-EVOLUTION.md) | Market research, stack decisions & eval-method notes |
| [docs/adr/](docs/adr/) | Architecture decision records |
| [CONTRIBUTING.md](CONTRIBUTING.md) | How to contribute |
| [GOVERNANCE.md](GOVERNANCE.md) / [SECURITY.md](SECURITY.md) | Project governance & vulnerability reporting |

## Command tour

Everything the CLI does, in copy-paste form (details in the docs above):

```bash
# assumes tapelog is installed (see Install above)

# Record a session while proxying a real MCP server:
tapelog record --policy policy.yaml --log session.jsonl -- npx -y @modelcontextprotocol/server-filesystem .

# One boundary across MANY servers (stdio + HTTP), one session log:
tapelog mux --config mux.yaml --policy policy.yaml --log session.jsonl

# Verify the log is untampered (record the printed chain head outside the
# log — CI output, a ticket — and it doubles as a rewrite/truncation anchor):
tapelog verify session.jsonl
tapelog verify session.jsonl --expect <chain-head-from-the-run>

# Replay the session as a hermetic MCP server (agent regression tests, CI):
tapelog replay session.jsonl --strict

# Compare two sessions (e.g. replay vs. live, or before/after a change):
tapelog diff session-a.jsonl session-b.jsonl

# Inspect a session (interactive TUI, or --plain for CI — deny blocks get
# event #/provenance ("value from read_secrets — produced at call #1")):
tapelog inspect session.jsonl

# Sign the session state and witness it in a transparency log (proves
# WHO attested the head and WHEN — see docs/CHECKPOINTS.md):
tapelog checkpoint session.jsonl --signer ssh --key ~/.ssh/id_ed25519
tapelog verify session.jsonl --checkpoint session.jsonl.checkpoint.json

# Export the session as an OpenTelemetry trace (OTLP or stdout):
tapelog export otel session.jsonl --endpoint http://localhost:4318/v1/traces

# Test a policy against sample tool calls:
tapelog policy test --policy policy.yaml --calls samples.jsonl

# Policy regression test: replay verdicts against a candidate policy
# (exits non-zero if any verdict would change):
tapelog policy whatif --policy candidate-policy.yaml session.jsonl

# Fuzz the boundary: attack-shaped mutations of recorded calls; a mutation
# that escapes a deny is a reproducible policy hole (exits non-zero on one):
tapelog fuzz --policy policy.yaml session.jsonl

# Preflight your setup (policy, plugins, platform, log writability):
tapelog doctor

# Park confirm verdicts for remote review (quarantine queue) — a web
# dashboard (approvals + live log) opens on the same socket:
tapelog record --policy policy.yaml --log session.jsonl \
  --approval-listen 127.0.0.1:8923 -- npx -y @modelcontextprotocol/server-filesystem .
tapelog queue list              # or: open http://127.0.0.1:8923
tapelog queue allow 1 --note "reviewed"

# Shell completion:
tapelog completion bash > /etc/bash_completion.d/tapelog

# Behavioral regression over recorded sessions (agent CI):
tapelog test agent-tests/ --plain   # assertions on the tool-call trajectory

# Browse all your recorded sessions in a browser (read-only):
tapelog web --dir ./sessions
```

## Design principles

- **Local-first.** No hosted service, no phone-home. Your traces stay on your machine.
- **Record everything, enforce what you can prove, replay the rest.** No security theater.
- **Untrusted-model assumption.** The model may be fully prompt-injected; the boundary must still hold.
- **The five-minute test.** A stranger gets from zero to a recorded, replayed session in five minutes ([docs/QUICKSTART.md](docs/QUICKSTART.md)). Every change is judged against that — if it costs time-to-value, it has to earn it.
- **Interoperable by default.** MCP spec `2026-07-28`, OpenTelemetry GenAI semantic conventions, language-neutral log schema.
- **Reuse over reinvention.** See the [bill of materials](STACK.md).

## License

Apache-2.0 — see [LICENSE](LICENSE).
