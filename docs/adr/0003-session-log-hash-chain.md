# ADR 0003: Session log = append-only hash-chained JSONL

**Status:** Accepted · 2026-09-29

## Context

We need a log that serves three masters: audit (tamper evidence), replay (deterministic re-execution), and interop (other tools can read/write it). Prior art: JSONL audit logs (no integrity), Merkle trees (complex), VCR cassettes (replay but no integrity), OTel spans (behavior but no replay).

## Decision

**One JSONL file, one event per line, each event hash-chained** to the previous (`prev_hash` / `hash` over a canonical form). Schema in `docs/SCHEMA.md` + `schema/session-event.v0.schema.json`.

Key choices:
- **JSONL:** greppable, streamable, diffable, language-neutral.
- **SHA-256 chain per event:** verifying the whole file (or a prefix) detects modification, deletion, and reordering. Simpler than Merkle proofs; adequate for a local-first tool. Not Byzantine-tamper-evident against an attacker who can rewrite the entire file — **documented honestly** in THREAT_MODEL.
- **Canonical form is hand-specified** (fixed field order + sorted-key payload JSON) so any language can implement a verifier in ~50 LOC.
- **Redaction happens before hashing** — the log never contains raw secrets; replay matching operates on redacted data and is therefore stable.

## Consequences

- Schema changes that touch the canonical form are breaking (`v` bump + migration) — enforced by GOVERNANCE.md rule 3.
- The same file is the replay cassette — no second storage format.
- Future: OTel export is derived from the log, never the source of truth.
