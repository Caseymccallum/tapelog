# Changelog

All notable changes to tapelog are documented here.
Format: [Keep a Changelog](https://keepachangelog.com/). Versions: [SemVer](https://semver.org/).

### v0.3 in progress
- **MCP 2026-07-28 compatibility (dual-era)** — the client and both bundled servers now speak the current spec end-to-end: `server/discover` probe with legacy `initialize` fallback (handshake-less against modern servers), per-request `_meta` self-description (`io.modelcontextprotocol/{protocolVersion,clientCapabilities,clientInfo}`), `-32022 UnsupportedProtocolVersionError` negotiated retry, `MCP-Protocol-Version`/`Mcp-Method`/`Mcp-Name` request-metadata headers on streamable HTTP, and `server/discover` is protocol plumbing (never policy-mediated — a deny there would break compliant clients at discovery). The proxy's un-mediated plumbing set is now `initialize`/`ping`/`tools/list`/`server/discover`; JSON-RPC notifications are never mediated
- **MRTR params preserved end-to-end** — `tapelog mux` no longer rebuilds `{name, arguments}` for upstream calls; the client's params (`_meta`, `inputResponses`, anything future) are forwarded verbatim with only the namespaced name rewritten (`CallToolParams`), so Multi Round-Trip Requests work through the boundary
- **Audit fail-closed (`--audit-mode`)** — session-log append failures no longer vanish: the first failure latches "audit-degraded" and, in strict mode (default), the affected call is denied and every later call fails closed (`rule_id: audit-degraded`) — an unrecorded tool call must not execute. `best-effort` is the explicit availability-over-integrity opt-out. `tools/result` appends that fail block result delivery the same way
- **`tapelog verify --expect <chain_head>`** — external trust anchor for the hash chain: the chain alone anchors at an empty `prev_hash`, so a whole-log rewrite with recomputed hashes is internally consistent; `--expect` (chain head recorded outside the log — CI output, ticket) detects whole-log rewrites and tail truncation. `verify` now prints the chain head
- **`--strict-schemas`** — opt-in strict schema-firewall: tools whose advertised `inputSchema` fails to compile are denied (`schema-firewall`) instead of silently skipped (default fail-open preserved)
- **Policy evaluates redacted args** — `where:` conditions now see the same (deterministically redacted) args the log records, so `tapelog policy whatif` verdicts reproduce live verdicts exactly; value-taint matching still operates on raw args (it must, to match secret values). This resolves the cedar.go/mediator contract drift
- **`policy whatif` replays value-level taint** — the what-if walker now rebuilds contamination from recorded results and consults `flows: mode: value` rules before session flows and per-call rules (it previously ignored value-mode flows, so contaminated-sink denies looked like verdict "changes"); parity locked by `TestWhatIfReproducesValueTaintVerdicts`
- **Log-overwrite protection unified** — the "existing log will be OVERWRITTEN" warning lives in `session.NewWriter`, so `record` and `mux` behave identically (mux previously truncated silently)
- **Release workflow fixed** — `release.yml` contained two `on:`/`jobs:` blocks (the survivor-by-luck used `@latest`/`main` installs); now one job, pinned installer actions, no floating tool versions
- **Version single-sourcing** — `internal/buildinfo` is the one place the version lives (CLI, MCP clientInfo, mux/replay serverInfo); goreleaser stamps it. MCP protocol version single-sourced in `transport.MCPVersion`
- **Live-tamper-safe recording** — the session writer no longer holds the log file open: each event is appended open-write-close, so an on-camera edit of a live log can neither lock the editor out (Windows sharing) nor strand later events at a stale file offset. A mid-session edit now prints a loud WARNING, the recording keeps appending, and `tapelog verify` pinpoints the edit (regression test: `TestWriterSurvivesExternalEditMidSession`)
- **Web dashboard: live chain verdict** — `/api/log` now verifies the hash chain on every poll (not just at page load) and carries `chain_ok`/`first_bad_seq`/`problem` + the file's max seq, so a tamper mid-session surfaces the ⚠ banner live in both the record dashboard (`--approval-listen`) and `tapelog web --dir` — plain refresh (F5) shows the truth; the renderer also re-pulls the whole log when the chain breaks or the file is replaced/truncated, instead of keeping stale pre-tamper rows
- **Windows + Roo/Cline integration fix (docs)** — root-caused "MCP error -32000: Connection closed": Roo wraps stdio commands in `cmd.exe /c`, whose quote rule chops paths containing spaces at the first space; fix is 8.3 short paths in the client config (recipe in `docs/CLIENT-SETUP.md`), verified byte-for-byte against Roo 3.54.0's actual spawn code
- **Onboarding: `docs/CLIENT-SETUP.md`** — "put it in front of your agent" recipes for Roo Code, VS Code (Copilot), Claude Code/Desktop, and any stdio client; documents the client-driven `confirm` trap (stdin is the MCP channel → approval queue / `--auto-confirm` / deny-only) and a troubleshooting table. README gains an Install section (releases + go install) and a 2-minute "Connect your agent" quickstart
- **Web dashboard: chain badges** — `tapelog web --dir` now verifies every session's hash chain on listing: tampered logs get a `⚠` in the session picker and a red "chain broken — first bad event: seq N" banner instead of silently rendering as authentic (spotted during product testing)
- **Spec community materials** — `spec/OTel-COMPARE.md` (honest "why not just OpenTelemetry" comparison + bridging strategy via decision spans) and `spec/FAQ.md` (adoption/security/process, including the hash-chain non-claims); linked from `spec/README.md`. Community distribution happens with the Show HN launch
- **`tapelog test` CI integrations** — `--junit <path>` JUnit XML report (GitLab/Jenkins/Azure/GitHub ingestion), `--annotate` GitHub Actions `::error` inline annotations (automatic on `GITHUB_ACTIONS`), and a CI dogfood job running tapelog's own scenario suite with the JUnit artifact uploaded
- **Fuzz operators v2** — 5 new boundary probes: `arg_unicode` (zero-width/homoglyph value evasions), `arg_boundary` (empty/null/extreme values), `arg_encoding` (base64/URL-encoded), `tool_namespace` (`server__tool` confusion), `swap_rotate` (non-adjacent reordering — sink moved before its source, the case `swap_adjacent` cannot reach); 13 operators total, all on the deny→allow oracle
- **Value-level taint (`flows: mode: value`)** — CaMeL-inspired precision layer (experimental, ADR 0005): flow rules fire only when sink arguments are *contaminated* by recorded values from source-tool results (`[contaminated by: ...]` evidence in deny reasons); clean sinks pass. Bounded contamination store; honest heuristic limits documented (transformations evade matching — keep `mode: session` for must-never flows)

## [Unreleased]

### Added
- **Release automation** — `release` workflow (tag `v*` or workflow_dispatch):
  goreleaser builds 6 static binaries, per-archive SBOMs (syft), keyless
  cosign-signed `checksums.txt`, and GitHub build-provenance attestation —
  fulfilling the SECURITY.md supply-chain promise; `workflow_dispatch`
  backfills older tags

## [0.2.0] — 2026-09-30

The **agent regression CI & boundary hardening** release: behavioral
testing at the tool boundary (`tapelog test`), policy fuzzing (`tapelog fuzz`),
batteries-included policy packs, a multi-session web dashboard, setup
preflight (`tapelog doctor`), and OTel policy-decision events. Built from
the community research in docs/RESEARCH-EVOLUTION.md — the tools the
eval/gateway wave does not have.

### Added (agent regression CI & boundary hardening, per docs/RESEARCH-EVOLUTION.md)
- **Multi-session dashboard** — `tapelog web --dir sessions/` serves a read-only browser dashboard over a directory of session logs: session list (event + denial counts), per-session event timeline, same security posture (loopback pinning, strict CSP, optional token, path-traversal-proof file selection)
- **`tapelog doctor`** — setup preflight: policy compiles (rules/Cedar/limits), WASM plugins load, platform capabilities (Landlock, terminal), log writability, node presence; ✓/!/✗ report, CI-friendly exit codes
- **OTel decision spans** — `policy.decision` is now a first-class span event on every `execute_tool` span (verdict, rule_id, reason as both attributes and event fields): the boundary's decisions are queryable in any trace backend
- **Policy packs** (`packs/`) — batteries-included policies for filesystem/git/fetch/postgres/slack/shell + a default-deny starter: deny the dangerous, confirm the risky, allow the routine. Every pack is Cedar-compiled in CI (`packs/packs_test.go`); live dogfooding caught and fixed an AND-vs-OR `where` authoring bug (now documented in POLICY.md)
- **`tapelog fuzz`** — the boundary hardening lab: attack-shaped mutations of recorded sessions (tool-name case/space/homoglyph/traversal spoofing, argument traversal/type/overflow tampering, toxic-flow reordering) re-evaluated against the policy; a deny→allow flip is a reproducible boundary bypass with rule id + ATLAS tactic hint. CI exit codes + JSON output. (Research: nobody fuzzes the agent↔tool boundary — prompt fuzzers and ATLAS generators exist, policy-boundary fuzzing did not.)
- **`tapelog test`** — behavioral regression at the tool boundary: YAML scenarios assert over recorded cassettes (`called`/`never_called`/`args`/`times`/`sequence`/`taint_never`/`result_contains`/`allowed`/`denied`/invariants), optional policy re-evaluation (what-if semantics), CI-native exit codes + `--plain`. Research note: the eval wave does prompt scoring; nobody does tool-call trajectory regression with hash-chained provenance

## [0.1.0] — 2026-09-30

First release: the flight recorder, boundary, and replay engine for MCP
tool-calling agents. Highlights: tamper-evident hash-chained session logs
(with a standalone format spec + conformance vectors), VCR-style hermetic
replay + diff, the full decision pipeline (policy rules with Cedar
conditions, schema firewall, limits, taint flows, drift detection,
injection scanning), human-in-the-loop approvals (terminal, queue, web
dashboard), multi-server mux, WASM plugins, OTel export, and an OS
sandbox for spawned servers.

### Research-driven gap fills (2026-09-29 internet research)
- **mux catalog aggregation + precise routing** — `resources/list`, `prompts/list`, `resources/templates/list` merge across upstreams (annotated `_tapelog_server`); prompt names namespaced like tools; resource reads route to the catalog's owner (unknown URIs fall back to first-success) — closes the last research item
- **Review web dashboard** — `--approval-listen` now serves a dependency-free dashboard on the same socket: parked approvals (with notes) + live session-log tail. XSS-safe rendering (`textContent` only + strict CSP), DNS-rebinding Host pinning, CSRF same-origin checks, `no-store`; `tapelog queue` CLI unchanged
- **Remote approval queue ("quarantine queue")** — `--approval-listen` parks `confirm` verdicts at the boundary (held, then fail-closed to deny on `--approval-timeout`); `tapelog queue list|allow|allow_session|deny` decides from any terminal via a localhost JSON API (`--approval-token` optional); every decision is recorded with its provenance and note
- **Injection scanning of results** — tool/surface results are scanned for prompt-injection markers (conservative built-ins + `injection.patterns`); `injection.mode: log` (default) records explainable `injection-scan` decisions, `confirm` routes through the HITL prompt, `deny` withholds the payload via the result-veto (original still recorded)
- **No unmediated surface** — `resources/read`, `prompts/get`, `resources/templates/get` (and any future method) now flow through the full decision pipeline as "surface calls" (`tool:` = method name, args = MCP params; Cedar `where:` conditions can match `context.args.uri`/`name`). They consume limits, feed taint, and are recorded as `tool_call`/`tool_result` events. In `mux` they route first-success in deterministic server order. Previously these crossed the boundary completely uninspected.
- **Schema firewall (inbound)** — tool-call arguments are validated against the `inputSchema` the server itself advertised (jsonschema draft-agnostic); violations deny with `rule_id: schema-firewall` before anything is forwarded. Fail-open for tools without/broken schemas. ("Allowing a tool name isn't enough — risk hides in the payload.")
- **Session limits** — policy `limits:` block: `max_calls` (session budget), `max_calls_per_tool` (glob patterns), `max_per_minute` (sliding window), `max_response_bytes` (payload cap — over-cap results are replaced with a structured `response_too_large` error while the log keeps the evidence). Explainable denials like every other verdict.
- **`tapelog completion`** — bash/zsh/fish/powershell scripts.

### Project identity
- **Renamed `cassette` → `tapelog`** after a full availability audit (docs/launch/NAMING.md): npm/PyPI/crates all clear, GitHub org `tapelog-dev`, Go module `github.com/Caseymccallum/tapelog`. The replay type is now `replay.Tape`; logs are "tapelogs". Also fixes a long-standing `.gitignore` bug (bare `cassette` pattern had silently excluded `cmd/` from git since the first commit)

### Post-plan development — v2, part 5 (roadmap complete)
- **OS sandbox for spawned servers** — `--sandbox-ro`/`--sandbox-rw` (repeatable) run MCP servers under Landlock filesystem restrictions via a hidden re-exec wrapper (`__sandbox_exec` + `syscall.Exec`); strict by default, `--sandbox-lenient` degrades with a warning; baselines keep binaries runnable (`/usr`, `/lib`, … RO, `/tmp` RW). Linux enforcement + gated kernel test; cross-platform wrapping tested everywhere

### Post-plan development — v2, part 4
- **WASM plugin API** (`--plugin`, repeatable) — sandboxed pure-computation plugins (wazero, no cgo): `verdict_hook` may only **tighten** decisions (host-enforced lattice — plugins physically cannot weaken enforcement; failures fail closed) and `redact_hook` adds org-specific redaction on already-redacted text. WASI reactor ABI documented in `docs/PLUGINS.md` with a complete reference plugin (`examples/plugins/argguard`, built with plain Go)

### Post-plan development — v2, part 3
- **Session log format as standalone spec** — `spec/` package: normative `session-log-v0.md` (RFC 2119, byte-precise canonical form, verification algorithm, replay semantics, conformance classes), JSON Schema, **machine-readable conformance test vectors** (`tools/gen-vectors` + `internal/session` conformance tests bind spec ↔ code). `docs/SCHEMA.md` is now a pointer stub

### Post-plan development — v2, part 2
- **`tapelog mux`** — one mediated boundary across multiple MCP servers: stdio commands and/or streamable-HTTP endpoints from a YAML config; tools namespaced `<server>__<tool>`; one session log, one policy, one taint state. Live listing refresh keeps `--deny-on-drift` working across upstreams
- **`internal/mediator`** — the decision pipeline (drift → flows → policy → confirm → taint → logging) extracted into one shared package used by `record` and `mux`; enforcement semantics cannot diverge
- **Transports** — `internal/transport`: stdio subprocess + minimal streamable-HTTP client (POST, JSON/SSE responses); `internal/mcpclient`: minimal MCP client (initialize / tools/list / tools/call) with id correlation

### Post-plan development — v2, part 1
- **Flow rules (toxic-flow guards)** — cross-tool data-flow policy: `flows:` in the policy file restricts `from` (source) → `to` (sink) tool pairs with session-scoped taint. Deny reasons name the taint sources. `action: deny` or `confirm`. This closes the category-wide gap (including the enterprise platforms') that per-call rules cannot see. Sequence-aware in `policy whatif` too
- **Human-in-the-loop `confirm` UX** — git-style terminal prompt: allow once / allow for session / deny. Fail-closed without a terminal unless `--auto-confirm`; every treatment recorded

### Week 4 — polish & launch
- **`tapelog inspect`** — interactive TUI session viewer (bubbletea): event timeline with color-coded verdicts, drift flags, payload detail pane; `--plain` for CI
- **`tapelog export otel`** — OpenTelemetry trace export (GenAI semantic conventions: `execute_tool` spans, `gen_ai.tool.name`); OTLP/HTTP to a collector or stdout
- **TypeScript & Python adapters** — thin, dependency-free config wrappers (`adapters/`)
- **Rogue-agent demo** — scripted 60-second attack story (`examples/rogue-agent/`)
- **Show HN launch kit** — post draft, checklist, talking points (`docs/launch/`)
- Replay: tool catalogs now merge listings newest-wins (no duplicate descriptors)

### Week 3 — policy v1
- **Cedar condition engine** — `where` clauses are real Cedar expressions over `context.tool` / `context.args`, evaluated by `cedar-go` (fail-closed on errors); full reference in `docs/POLICY.md`
- **Scoped grants** — `expires` (RFC 3339 time-boxed authority) + `tasks` (per-task delegation via `record --task`)
- **`tapelog policy whatif`** — re-evaluate a recorded session against a candidate policy and diff verdicts; exits non-zero on change → CI policy regression tests for agents
- **`tapelog policy compile`** — export policies as portable Cedar text (documented approximations)
- **`record --deny-on-drift`** — deny tool calls whose descriptor changed since first listing (tool-poisoning defense); tool calls now wait for in-flight `tools/list` responses so enforcement cannot be raced; descriptor pins now recorded on every call

### Week 2 — replay engine
- **`tapelog replay`** — deterministic re-execution: a recorded session is served as a **hermetic MCP server** (`initialize`, `tools/list`, `tools/call` answered from the recording). VCR semantics: redaction-aware canonical matching (`--match exact|subset|tool`), consume-once FIFO, **fail-loud** on missing recordings (error `-32011` + remediation hint), `--strict` mode for CI
- **`tapelog diff`** — compare two sessions (added/removed calls, changed results, order drift); exits non-zero when they differ
- `tools/list` events now recorded (descriptors redacted + canonicalized) — replay can serve the exact tool catalog
- `tool_descriptor_hash` is now over the *redacted canonical* descriptor (verifiable from the log)

### Week 1 — foundation
- **Session log format v0** — append-only, hash-chained JSONL (`docs/SCHEMA.md` at the time — now `spec/session-log-v0.md`)
- `tapelog record` — transparent stdio MCP proxy that records every tool call, result, and policy verdict into the session log (with secret redaction and tool-descriptor hash pinning)
- `tapelog verify` — hash-chain verification; detects modification, deletion, and reordering (including recomputed-hash edits of individual events; whole-log rewrites require an external anchor such as the chain head printed at session end)
- `tapelog policy test` — evaluate sample tool calls against a policy before deploying it
- **Policy engine v0** — ordered YAML rules (`allow` / `deny` / `confirm`), glob tool matching, explainable decisions with reasons, fail-closed default
- Threat model, governance set (GOVERNANCE, SECURITY, CODE_OF_CONDUCT, CONTRIBUTING), ADRs 0001–0003
- CI (Linux race tests + Windows), goreleaser config

### Notes
- Working name `tapelog`; final name TBD before publication
