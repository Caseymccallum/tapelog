# Changelog

All notable changes to cassette are documented here.
Format: [Keep a Changelog](https://keepachangelog.com/). Versions: [SemVer](https://semver.org/).

## [Unreleased] — v0.1.0 (week 1 of 4)

### Added
- **Session log format v0** — append-only, hash-chained JSONL (`docs/SCHEMA.md`, `schema/session-event.v0.schema.json`)
- `cassette record` — transparent stdio MCP proxy that records every tool call, result, and policy verdict into the session log (with secret redaction and tool-descriptor hash pinning)
- `cassette verify` — hash-chain verification; detects modification, deletion, and reordering (incl. recomputed-hash attacks)
- `cassette policy test` — evaluate sample tool calls against a policy before deploying it
- **Policy engine v0** — ordered YAML rules (`allow` / `deny` / `confirm`), glob tool matching, explainable decisions with reasons, fail-closed default
- Threat model, governance set (GOVERNANCE, SECURITY, CODE_OF_CONDUCT, CONTRIBUTING), ADRs 0001–0003
- CI (Linux race tests + Windows), goreleaser config

### Notes
- Working name `cassette`; final name TBD before publication
- Replay engine is next (week 2): `cassette replay` against recorded tool I/O
