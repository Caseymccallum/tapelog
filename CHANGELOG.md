# Changelog

All notable changes to cassette are documented here.
Format: [Keep a Changelog](https://keepachangelog.com/). Versions: [SemVer](https://semver.org/).

## [Unreleased] — v0.1.0

### Post-plan development — v2, part 3
- **Session log format as standalone spec** — `spec/` package: normative `session-log-v0.md` (RFC 2119, byte-precise canonical form, verification algorithm, replay semantics, conformance classes), JSON Schema, **machine-readable conformance test vectors** (`tools/gen-vectors` + `internal/session` conformance tests bind spec ↔ code). `docs/SCHEMA.md` is now a pointer stub

### Post-plan development — v2, part 2
- **`cassette mux`** — one mediated boundary across multiple MCP servers: stdio commands and/or streamable-HTTP endpoints from a YAML config; tools namespaced `<server>__<tool>`; one session log, one policy, one taint state. Live listing refresh keeps `--deny-on-drift` working across upstreams
- **`internal/mediator`** — the decision pipeline (drift → flows → policy → confirm → taint → logging) extracted into one shared package used by `record` and `mux`; enforcement semantics cannot diverge
- **Transports** — `internal/transport`: stdio subprocess + minimal streamable-HTTP client (POST, JSON/SSE responses); `internal/mcpclient`: minimal MCP client (initialize / tools/list / tools/call) with id correlation

### Post-plan development — v2, part 1
- **Flow rules (toxic-flow guards)** — cross-tool data-flow policy: `flows:` in the policy file restricts `from` (source) → `to` (sink) tool pairs with session-scoped taint. Deny reasons name the taint sources. `action: deny` or `confirm`. This closes the category-wide gap (including the enterprise platforms') that per-call rules cannot see. Sequence-aware in `policy whatif` too
- **Human-in-the-loop `confirm` UX** — git-style terminal prompt: allow once / allow for session / deny. Fail-closed without a terminal unless `--auto-confirm`; every treatment recorded

### Week 4 — polish & launch
- **`cassette inspect`** — interactive TUI session viewer (bubbletea): event timeline with color-coded verdicts, drift flags, payload detail pane; `--plain` for CI
- **`cassette export otel`** — OpenTelemetry trace export (GenAI semantic conventions: `execute_tool` spans, `gen_ai.tool.name`); OTLP/HTTP to a collector or stdout
- **TypeScript & Python adapters** — thin, dependency-free config wrappers (`adapters/`)
- **Rogue-agent demo** — scripted 60-second attack story (`examples/rogue-agent/`)
- **Show HN launch kit** — post draft, checklist, talking points (`docs/launch/`)
- Replay: tool catalogs now merge listings newest-wins (no duplicate descriptors)

### Week 3 — policy v1
- **Cedar condition engine** — `where` clauses are real Cedar expressions over `context.tool` / `context.args`, evaluated by `cedar-go` (fail-closed on errors); full reference in `docs/POLICY.md`
- **Scoped grants** — `expires` (RFC 3339 time-boxed authority) + `tasks` (per-task delegation via `record --task`)
- **`cassette policy whatif`** — re-evaluate a recorded session against a candidate policy and diff verdicts; exits non-zero on change → CI policy regression tests for agents
- **`cassette policy compile`** — export policies as portable Cedar text (documented approximations)
- **`record --deny-on-drift`** — deny tool calls whose descriptor changed since first listing (tool-poisoning defense); tool calls now wait for in-flight `tools/list` responses so enforcement cannot be raced; descriptor pins now recorded on every call

### Week 2 — replay engine
- **`cassette replay`** — deterministic re-execution: a recorded session is served as a **hermetic MCP server** (`initialize`, `tools/list`, `tools/call` answered from the recording). VCR semantics: redaction-aware canonical matching (`--match exact|subset|tool`), consume-once FIFO, **fail-loud** on missing recordings (error `-32011` + remediation hint), `--strict` mode for CI
- **`cassette diff`** — compare two sessions (added/removed calls, changed results, order drift); exits non-zero when they differ
- `tools/list` events now recorded (descriptors redacted + canonicalized) — replay can serve the exact tool catalog
- `tool_descriptor_hash` is now over the *redacted canonical* descriptor (verifiable from the log)

### Week 1 — foundation
- **Session log format v0** — append-only, hash-chained JSONL (`docs/SCHEMA.md` at the time — now `spec/session-log-v0.md`)
- `cassette record` — transparent stdio MCP proxy that records every tool call, result, and policy verdict into the session log (with secret redaction and tool-descriptor hash pinning)
- `cassette verify` — hash-chain verification; detects modification, deletion, and reordering (incl. recomputed-hash attacks)
- `cassette policy test` — evaluate sample tool calls against a policy before deploying it
- **Policy engine v0** — ordered YAML rules (`allow` / `deny` / `confirm`), glob tool matching, explainable decisions with reasons, fail-closed default
- Threat model, governance set (GOVERNANCE, SECURITY, CODE_OF_CONDUCT, CONTRIBUTING), ADRs 0001–0003
- CI (Linux race tests + Windows), goreleaser config

### Notes
- Working name `cassette`; final name TBD before publication
