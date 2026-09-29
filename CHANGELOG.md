# Changelog

All notable changes to cassette are documented here.
Format: [Keep a Changelog](https://keepachangelog.com/). Versions: [SemVer](https://semver.org/).

## [Unreleased] — v0.1.0

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
