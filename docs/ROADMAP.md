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
- [ ] Cedar under the hood (keep YAML front-end; `Evaluator` interface stable)
- [ ] Scoped grants with expiry (`expires`, per-task scoping)
- [ ] Argument-level conditions (path/domain scope constraints)
- [ ] `policy what-if`: re-evaluate a recorded session against a candidate policy; diff verdicts
- [ ] Tool-descriptor pinning enforcement (deny-on-drift mode)

## Week 4 — Polish & launch
- [ ] TUI session viewer / scrubger (bubbletea)
- [ ] OTel export (GenAI semantic conventions)
- [ ] TS + Python thin adapters (config/integration)
- [ ] Demo: "rogue agent" scripted demo + GIF
- [ ] Show HN post + schema spec published standalone

## Later / v2 candidates
- Toxic-flow (taint) rules across tool calls — the known gap in every tool in this category
- HTTP transport interception + remote MCP
- WASM plugin API (external policy/transform plugins)
- Session-log schema as community spec (foundation home)
- Interactive `confirm` UX (per-call TUI approval prompts)
- cgroup/landlock defense-in-depth hooks
