# Agent Session Log Format — specification package

A portable, tamper-evident, replayable record of what AI agents *did*.
Standalone: everything an implementation needs lives in this directory.

| File | What |
|---|---|
| [session-log-v0.md](session-log-v0.md) | **The normative specification** (RFC 2119) |
| [schema/session-event.v0.schema.json](schema/session-event.v0.schema.json) | JSON Schema (draft 2020-12) for one event |
| [test-vectors/](test-vectors/) | Machine-readable conformance vectors |

## Why this exists

Agent debugging, incident review, compliance evidence, regression tests,
and observability all need the same raw material: *which tool calls
happened, what was permitted, and what came back*. Today every tool
invents its own trace. This format is the neutral ground — one JSONL file
that serves as:

- **audit evidence** (hash-chained; `verify` detects tampering),
- **a replay cassette** (deterministic re-execution for CI),
- **an interop surface** (export to OTel, ingest from any mediator).

## Conformance classes

| Class | You are a… | Requirements |
|---|---|---|
| **Writer** | mediator / proxy / harness | §2–§4, §6, §7 of the spec |
| **Verifier** | audit / compliance tooling | §5 |
| **Reader** | viewers, exporters, analytics | §2, §3, §6 |
| **Replayer** | CI / test harnesses | §8 |

## Using the test vectors

`test-vectors/vectors.json` pins the normative behavior:

- `canonical` cases: `payload` → exact expected canonical-payload string
  and full canonical form + hash
- `session` cases: complete `.jsonl` files with expected verification
  results (intact / first-bad-seq)

An implementation is conformant when it reproduces these outputs
byte-for-byte. Regenerate with `go run ./tools/gen-vectors` (reference
implementation); never edit vectors by hand.

## Relationship to cassette

[cassette](../README.md) is the reference implementation (Writer +
Verifier + Replayer). The format is deliberately independent: it has no
cassette-specific fields, and this directory can be extracted into its own
repository when the community process starts (see docs/ROADMAP.md).

## Versioning

Specified in §9: additive changes stay at `v: 0` (readers ignore unknowns);
any canonical-form change is breaking and bumps `v`. Governance: the
schema-change rules in [../GOVERNANCE.md](../GOVERNANCE.md) apply.
