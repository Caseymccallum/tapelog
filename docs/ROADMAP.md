# Roadmap

Per [STACK.md](../STACK.md) — 4-week plan to Show HN. Checkboxes updated as we go.

## Week 1 — Foundation (this week)
- [x] Research + docs: RESEARCH.md, STACK.md, README, ARCHITECTURE, THREAT_MODEL, SCHEMA, governance set, ADRs
- [x] Go module scaffold: `cmd/cassette` CLI (`record`, `verify`, `policy test`, `version`)
- [x] `internal/session`: event schema + hash-chained JSONL writer/verifier + redaction (with tests)
- [x] `internal/policy`: YAML policy → explainable allow/deny/confirm decisions (with tests)
- [x] `internal/proxy` + `internal/jsonrpc`: stdio MCP interception with deny short-circuit (with tests)
- [x] CI workflow (build/vet/test) + goreleaser config
- [x] E2E validated: record → deny/allow → verify against a fake MCP server (test-fixtures/)

## Week 2 — Replay engine
- [x] Cassette capture format (session log = replay source; `tools/list` descriptors recorded)
- [x] Matching rules: canonical arg hashing, configurable matchers (`exact` / `subset` / `tool`)
- [x] `cassette replay` — deterministic re-execution as a hermetic MCP server (VCR semantics: fail-loud on missing entries, consume-once, `--strict`)
- [x] `cassette diff` — compare two sessions (replay vs. live, before/after a change)

## Week 3 — Policy v1
- [x] Cedar under the hood (YAML front-end kept; `Evaluator` interface stable) — `where` conditions are real Cedar via `cedar-go`
- [x] Scoped grants with expiry (`expires` RFC 3339, `tasks` per-task scoping)
- [x] Argument-level conditions (`where: 'context.args.path like "/tmp/*"'` etc.)
- [x] `policy what-if` — re-evaluate a recorded session against a candidate policy; diff verdicts (exits non-zero on change)
- [x] Tool-descriptor pinning enforcement: `record --deny-on-drift` + race-free ordering (calls wait for in-flight listings)
- [x] `policy compile` — portable Cedar export

## Week 4 — Polish & launch
- [x] TUI session viewer (`cassette inspect`, bubbletea) + `--plain` mode
- [x] OTel export (GenAI semantic conventions; OTLP/HTTP + stdout)
- [x] TS + Python thin adapters (config/integration)
- [x] Demo: "rogue agent" scripted demo (`examples/rogue-agent/demo.ps1`)
- [x] Show HN post drafted + launch checklist (`docs/launch/SHOW-HN.md`)
- [ ] Demo GIF (launch-time artifact — see examples/rogue-agent/README.md)
- [ ] Tag v0.1.0 + signed release (after final name decision)

## Later / v2 candidates
- [x] Toxic-flow (taint) rules across tool calls — **v1 shipped: session-scoped taint (`flows:` rules); value-level CaMeL-style tracking remains research-grade**
- [x] Interactive `confirm` UX (per-call terminal approval prompts)
- [x] HTTP transport interception + remote MCP → **v1 shipped: `cassette mux` multi-server aggregator with stdio + streamable-HTTP upstreams (server-initiated HTTP messages deferred)**
- [x] WASM plugin API → **v1 shipped: `--plugin` chain (wazero), tighten-only `verdict_hook` + `redact_hook`, reactor ABI in docs/PLUGINS.md, reference plugin in examples/**
- [x] Session-log schema as community spec → **v1 shipped: `spec/` (normative spec + JSON Schema + conformance vectors); foundation/community-process home is the launch-time follow-up**
- [x] cgroup/landlock defense-in-depth hooks → **v1 shipped: `--sandbox-ro`/`--sandbox-rw` Landlock sandbox for spawned servers (strict by default, `--sandbox-lenient` opt-out; cgroup/seccomp limits deferred)**
