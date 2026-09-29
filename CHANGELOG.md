# Changelog

All notable changes to cassette are documented here.
Format: [Keep a Changelog](https://keepachangelog.com/). Versions: [SemVer](https://semver.org/).

## [Unreleased] — v0.1.0

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
- **Session log format v0** — append-only, hash-chained JSONL (`docs/SCHEMA.md`, `schema/session-event.v0.schema.json`)
- `cassette record` — transparent stdio MCP proxy that records every tool call, result, and policy verdict into the session log (with secret redaction and tool-descriptor hash pinning)
- `cassette verify` — hash-chain verification; detects modification, deletion, and reordering (incl. recomputed-hash attacks)
- `cassette policy test` — evaluate sample tool calls against a policy before deploying it
- **Policy engine v0** — ordered YAML rules (`allow` / `deny` / `confirm`), glob tool matching, explainable decisions with reasons, fail-closed default
- Threat model, governance set (GOVERNANCE, SECURITY, CODE_OF_CONDUCT, CONTRIBUTING), ADRs 0001–0003
- CI (Linux race tests + Windows), goreleaser config

### Notes
- Working name `cassette`; final name TBD before publication
