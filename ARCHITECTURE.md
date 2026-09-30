# Architecture

**Tapelog** is a local-first boundary layer between AI agent harnesses and MCP tool servers. It records every tool interaction into a tamper-evident session log, evaluates declarative policy before side effects occur, and replays sessions deterministically (`tapelog replay`).

## Components

```
┌───────────────┐   MCP stdio/HTTP    ┌─────────────────────────────────┐
│ agent harness │◄───────────────────►│            tapelog             │
└───────────────┘                     │  ┌──────────┐   ┌───────────┐  │
                                      │  │ proxy    │──►│ policy    │  │
                                      │  │ (jsonrpc)│   │ evaluator │  │
                                      │  └────┬─────┘   └───────────┘  │
                                      │       │                         │
                                      │  ┌────▼─────┐   ┌───────────┐  │
                                      │  │ session  │──►│ redactor  │  │
                                      │  │ logger   │   └───────────┘  │
                                      │  └────┬─────┘                  │
                                      └───────┼─────────────────────────┘
                                              ▼
                                   session.jsonl (hash-chained)
```

| Package | Responsibility |
|---|---|
| `cmd/tapelog` | CLI (cobra): `record`, `mux`, `verify`, `replay`, `diff`, `inspect`, `export`, `policy`, `test`, `fuzz`, `doctor`, `web`, `queue`, `completion`, `version` |
| `internal/jsonrpc` | JSON-RPC 2.0 envelope parsing/serialization for the MCP wire format |
| `internal/proxy` | Transparent bidirectional MCP relay; interception hooks; deny short-circuit |
| `internal/mediator` | Per-session decision pipeline: drift pinning, schema firewall, limits, flows/taint, injection scan, confirm routing, recording |
| `internal/policy` | `Evaluator` interface, YAML policy loader, glob matching, explainable decisions |
| `internal/session` | Event types, hash-chained JSONL writer/verifier, redaction |
| `internal/replay` | Deterministic re-execution of a recorded session as a hermetic MCP server |
| `internal/approval` | Parked-approval queue (remote human-in-the-loop) |
| `internal/web` | Embedded review dashboard (approvals + live session log + chain verdicts) |
| `spec/` | The session log format: normative spec + JSON Schema + conformance vectors |

## Data flow

1. Harness sends a JSON-RPC message to `tapelog` (acting as an MCP server).
2. If `method == tools/call`, the proxy extracts `params.name` + `params.arguments` and asks the **policy evaluator** for a verdict *before any side effect*:
   - `allow` → forward to the real MCP server (acting as MCP client).
   - `deny` → synthesize a JSON-RPC error response (`code: -32010`, structured data with policy id + reason); nothing is forwarded.
   - `confirm` → pause for a human: terminal prompt, or the remote approval queue (`--approval-listen`); `--auto-confirm` records the call as allowed; with none available, fail closed (deny).
3. Every message in both directions is redacted and appended to the **session log** as a hash-chained event (`tools/call`, `policy/decision`, `tools/result`, `session/start`, `session/end`).
4. `tapelog verify` re-computes the chain to detect tampering.

## Trust boundaries

| Boundary | Assumption |
|---|---|
| Model / harness → tapelog | **Untrusted.** A fully prompt-injected agent must not exceed delegated authority. Tool *descriptors* are untrusted (hash-pinned to detect rug pulls). |
| tapelog → MCP server | Trusted forwarding of *already-approved* calls only. |
| Session log | Trusted for integrity (SHA-256 hash chain); confidentiality is the user's responsibility (redaction is best-effort — see THREAT_MODEL.md). |
| Policy file | Trusted configuration; `policy test` lets users validate before deployment. |

## Key design decisions

See [docs/adr/](docs/adr/):
- [0001-language-go.md](docs/adr/0001-language-go.md) — Go core for adoption & contributions
- [0002-policy-engine-cedar.md](docs/adr/0002-policy-engine-cedar.md) — Cedar primary, YAML front-end, evaluator interface
- [0003-session-log-hash-chain.md](docs/adr/0003-session-log-hash-chain.md) — append-only hash-chained JSONL format
- [0004-mux-and-transports.md](docs/adr/0004-mux-and-transports.md) — multi-server mux & transports
- [0005-value-level-taint.md](docs/adr/0005-value-level-taint.md) — value-level taint tracking (experimental)

## Non-goals (v0)

- Hosted/telemetry components of any kind
- LLM-judged content filtering (prompt-injection *prevention* at the model)
- Kernel-level enforcement as a hard guarantee — Landlock sandboxing exists as opt-in defense-in-depth (`--sandbox-*`, Linux); eBPF/seccomp remains out of scope
- Multi-agent / A2A protocol mediation
