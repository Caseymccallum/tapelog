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
| `cmd/tapelog` | CLI (cobra): `record`, `mux`, `verify`, `checkpoint`, `replay`, `diff`, `inspect`, `export`, `policy`, `test`, `fuzz`, `doctor`, `web`, `queue`, `completion`, `version` (+ hidden `__sandbox_exec` re-exec wrapper) |
| `internal/jsonrpc` | JSON-RPC 2.0 envelope parsing/serialization for the MCP wire format |
| `internal/proxy` | Transparent bidirectional MCP relay (single-server `record`); interception hooks; deny short-circuit |
| `internal/mux` | Multi-server aggregator: one boundary across stdio + streamable-HTTP upstreams, `<server>__<tool>` namespacing |
| `internal/mcpclient` / `internal/transport` | Hand-rolled MCP client + stdio/HTTP transports (dual-era: 2026-07-28 `server/discover` probe + legacy handshake) |
| `internal/mediator` | Per-session decision pipeline: drift pinning, schema firewall, limits, flows/taint, injection scan, confirm routing, recording |
| `internal/policy` | `Evaluator` interface, YAML policy loader, glob matching, flow rules, explainable decisions |
| `internal/session` | Event types, hash-chained JSONL writer/verifier, redaction, content-addressed blob store (large payloads) |
| `internal/checkpoint` | Signed session checkpoints: SSHSIG/cosign signers + Rekor transparency witness (offline verification) |
| `internal/replay` | Deterministic re-execution of a recorded session as a hermetic MCP server; what-if verdict re-evaluation |
| `internal/scenario` | `tapelog test`: trajectory assertions over recorded cassettes |
| `internal/fuzz` | `tapelog fuzz`: attack-shaped mutations of recorded calls with a deny→allow oracle |
| `internal/tui` | Session viewer (`inspect`): timeline, deny provenance, causal story |
| `internal/compat` | MCP compatibility laboratory: hermetic era matrix + real-world tier (`compat/fakecmd` server binary) |
| `internal/taint` | Value-level taint store (CaMeL-style contamination matching, `flows: mode: value`) |
| `internal/schemafire` | Inbound schema firewall (calls must satisfy the advertised `inputSchema`) |
| `internal/limits` | Session budgets / rate limits / payload caps |
| `internal/inject` | Prompt-injection scanning of tool results |
| `internal/plugin` | WASM plugin chain (wazero): tighten-only `verdict_hook` + `redact_hook` |
| `internal/sandbox` | Landlock filesystem sandbox for spawned servers (`--sandbox-*`, Linux) |
| `internal/approval` | Parked-approval queue (remote human-in-the-loop) |
| `internal/web` | Embedded review dashboard (approvals + live session log + chain verdicts) |
| `internal/report` | JUnit XML + GitHub annotations for `tapelog test` |
| `internal/otelx` | OpenTelemetry export helpers (`tapelog export otel`) |
| `internal/buildinfo` | Single source of truth for the build version |
| `spec/` | The session log format: normative spec + JSON Schema + conformance vectors |

## Data flow

1. Harness sends a JSON-RPC message to `tapelog` (acting as an MCP server).
2. If `method == tools/call`, tapelog extracts `params.name` + `params.arguments` and the **mediator** runs the decision pipeline *before any side effect* (descriptor pins → schema firewall → limits → flows/taint → policy evaluator → confirm routing). Lines the boundary cannot parse — or actionable methods sent without a request id — are rejected (JSON-RPC -32700/-32600/-32602) and recorded, never relayed:
   - `allow` → forward to the real MCP server (acting as MCP client).
   - `deny` → synthesize a JSON-RPC error response (`code: -32010`, structured data with policy id + reason); nothing is forwarded.
   - `confirm` → pause for a human: terminal prompt, or the remote approval queue (`--approval-listen`); `--auto-confirm` records the call as allowed; with none available, fail closed (deny).
3. Every tool call, decision, result, and tool listing is redacted and appended to the **session log** as a hash-chained event (`session/start`, `tools/list`, `tools/call`, `policy/decision`, `tools/result`, `session/end`). Oversized `args`/`result` may be offloaded to a content-addressed blob store (`--blob-threshold`); the chain then binds their digests (spec §6.2).
4. `tapelog verify` re-computes the chain to detect tampering (and re-hashes blob references when a store is present). `verify --expect` anchors the head externally; `verify --checkpoint` verifies a signed checkpoint (who attested the head, and when). `tapelog replay` re-executes the session from the log.

## Trust boundaries

| Boundary | Assumption |
|---|---|
| Model / harness → tapelog | **Untrusted.** A fully prompt-injected agent must not exceed delegated authority. Tool *descriptors* are untrusted (hash-pinned to detect rug pulls). |
| tapelog → MCP server | Trusted forwarding of *already-approved* calls only. |
| Session log | Trusted for integrity (SHA-256 hash chain); whole-log rewrites need an external anchor (`verify --expect` or a signed checkpoint). Confidentiality is the user's responsibility (redaction is best-effort — see THREAT_MODEL.md). |
| Blob store (large payloads) | Untrusted for integrity-by-position: content is bound by SHA-256 digests recorded in the chain and re-checked by `verify`; replay needs the store present (fail-loud otherwise). |
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
