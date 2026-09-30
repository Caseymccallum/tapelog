# Roadmap

Per [STACK.md](../STACK.md) — 4-week plan to Show HN. Checkboxes updated as we go.

## Week 1 — Foundation (this week)
- [x] Research + docs: RESEARCH.md, STACK.md, README, ARCHITECTURE, THREAT_MODEL, SCHEMA, governance set, ADRs
- [x] Go module scaffold: `cmd/tapelog` CLI (`record`, `verify`, `policy test`, `version`)
- [x] `internal/session`: event schema + hash-chained JSONL writer/verifier + redaction (with tests)
- [x] `internal/policy`: YAML policy → explainable allow/deny/confirm decisions (with tests)
- [x] `internal/proxy` + `internal/jsonrpc`: stdio MCP interception with deny short-circuit (with tests)
- [x] CI workflow (build/vet/test) + goreleaser config
- [x] E2E validated: record → deny/allow → verify against a fake MCP server (test-fixtures/)

## Week 2 — Replay engine
- [x] Tapelog capture format (session log = replay source; `tools/list` descriptors recorded)
- [x] Matching rules: canonical arg hashing, configurable matchers (`exact` / `subset` / `tool`)
- [x] `tapelog replay` — deterministic re-execution as a hermetic MCP server (VCR semantics: fail-loud on missing entries, consume-once, `--strict`)
- [x] `tapelog diff` — compare two sessions (replay vs. live, before/after a change)

## Week 3 — Policy v1
- [x] Cedar under the hood (YAML front-end kept; `Evaluator` interface stable) — `where` conditions are real Cedar via `cedar-go`
- [x] Scoped grants with expiry (`expires` RFC 3339, `tasks` per-task scoping)
- [x] Argument-level conditions (`where: 'context.args.path like "/tmp/*"'` etc.)
- [x] `policy what-if` — re-evaluate a recorded session against a candidate policy; diff verdicts (exits non-zero on change)
- [x] Tool-descriptor pinning enforcement: `record --deny-on-drift` + race-free ordering (calls wait for in-flight listings)
- [x] `policy compile` — portable Cedar export

## Week 4 — Polish & launch
- [x] TUI session viewer (`tapelog inspect`, bubbletea) + `--plain` mode
- [x] OTel export (GenAI semantic conventions; OTLP/HTTP + stdout)
- [x] TS + Python thin adapters (config/integration)
- [x] Demo: "rogue agent" scripted demo (`examples/rogue-agent/demo.ps1`)
- [x] Show HN post drafted + launch checklist (`docs/launch/SHOW-HN.md`)
- [ ] Demo GIF (launch-time artifact — see examples/rogue-agent/README.md)
- [ ] Tag v0.1.0 + signed release (after final name decision)

## Later / v2 candidates
- [x] Toxic-flow (taint) rules across tool calls — **v1 shipped: session-scoped taint (`flows:` rules); value-level CaMeL-style tracking remains research-grade**
- [x] Interactive `confirm` UX (per-call terminal approval prompts)
- [x] HTTP transport interception + remote MCP → **v1 shipped: `tapelog mux` multi-server aggregator with stdio + streamable-HTTP upstreams (server-initiated HTTP messages deferred)**
- [x] WASM plugin API → **v1 shipped: `--plugin` chain (wazero), tighten-only `verdict_hook` + `redact_hook`, reactor ABI in docs/PLUGINS.md, reference plugin in examples/**
- [x] Session-log schema as community spec → **v1 shipped: `spec/` (normative spec + JSON Schema + conformance vectors); foundation/community-process home is the launch-time follow-up**
- [x] cgroup/landlock defense-in-depth hooks → **v1 shipped: `--sandbox-ro`/`--sandbox-rw` Landlock sandbox for spawned servers (strict by default, `--sandbox-lenient` opt-out; cgroup/seccomp limits deferred)**

### Post-research gap fills (2026-09-29)
- [x] Inbound schema firewall (args vs advertised inputSchema) — shipped
- [x] Session limits: budgets, per-tool caps, rate limit, response payload cap — shipped
- [x] Shell completion — shipped
- [x] Resource/prompt surface gating (no unmediated surface) — **v1 shipped: surface calls through the full pipeline; mux routes first-success (catalog aggregation still open)**
- [x] Prompt-injection heuristics in tool results — **v1 shipped: `injection: mode: log|confirm|deny` with conservative built-ins + custom patterns**
- [x] Remote approval queue (async HITL "quarantine queue") — **v1 shipped: `--approval-listen` + `tapelog queue` CLI, fail-closed timeout, note-carrying audit trail**
- [x] Review web dashboard (approvals + live log tail) — **v1 shipped: XSS-safe embedded UI on the approval socket; token + host-pinning + CSRF defenses**
- [x] Resource/prompt catalog aggregation + namespacing in mux — **v1 shipped: merged annotated catalogs, namespaced prompts, owner-map routing for resource reads (research list complete)**

## v0.2 candidates (prioritized per docs/RESEARCH-EVOLUTION.md)
- [x] **`tapelog test` — agent regression CI** (mainline) — **v1 shipped: scenario DSL over cassettes, policy re-evaluation, CI exit codes (docs/TESTING.md)**
- [x] **`tapelog fuzz` — boundary hardening lab** — **v1 shipped: 8 mutation operators, comparative deny→allow oracle, ATLAS hints, CI/JSON output (docs/TESTING.md)**
- [x] Policy packs for popular MCP servers (community rules repo) — **v1 shipped: 7 packs + CI-enforced Cedar compilation (packs/); caught a `where` AND-vs-OR authoring bug via live dogfooding**
- [x] `tapelog doctor` (config/plugin/platform sanity) — **v1 shipped: preflight checks with ✓/!/✗ report + CI exit codes**
- [x] OTel spans for policy decisions — **v1 shipped: `policy.decision` span events + verdict/rule/reason attributes**
- [x] Web dashboard: multi-session support — **v1 shipped: `tapelog web --dir` session browser (list + per-session timeline, traversal-proof)**
- Value-level taint tracking (CaMeL-style) research spike
