# Session Log Schema (v0)

The session log is cassette's core artifact: an **append-only, hash-chained JSONL** file, one event per line. It is the audit trail *and* the replay source. The format is language-neutral — other tools may write or verify it.

Machine-readable schema: [`schema/session-event.v0.schema.json`](../schema/session-event.v0.schema.json)

## Event envelope

```json
{
  "v": 0,
  "seq": 3,
  "ts": "2026-09-29T12:34:56.789Z",
  "type": "tools/call",
  "session_id": "01J...",          
  "payload": { "...type-specific..." },
  "prev_hash": "9f2a…",
  "hash": "c41d…"
}
```

| Field | Type | Description |
|---|---|---|
| `v` | int | Schema version (currently `0`) |
| `seq` | uint | Monotonic sequence number, starting at 1 within a session |
| `ts` | string | RFC 3339 timestamp (UTC, millisecond precision) |
| `type` | string | Event type (below) |
| `session_id` | string | ULID/UUID of the recording session |
| `payload` | object | Type-specific data |
| `prev_hash` | string | Hex SHA-256 of the previous event's `hash` (`""` for seq 1) |
| `hash` | string | Hex SHA-256 of this event's canonical form (below) |

## Hash chain — canonical form

`hash = SHA-256(canonical_bytes)` where `canonical_bytes` is the ASCII concatenation:

```
"v=" + <v> + "\n" +
"seq=" + <seq> + "\n" +
"ts=" + <ts> + "\n" +
"type=" + <type> + "\n" +
"session_id=" + <session_id> + "\n" +
"prev_hash=" + <prev_hash> + "\n" +
"payload=" + <payload serialized as compact JSON, hash-ordered keys>
```

Rules (deliberately simple and language-portable):
1. No spaces; UTF-8.
2. `payload` is serialized with **lexicographically sorted object keys at all depths** (JSON Canonicalization Scheme subset; exact JCS numbers are out of scope for v0 — our payloads use strings/ints only where hashed).
3. `hash` field itself is excluded.
4. Verification: recompute each line's hash, compare with stored `hash`, and with the next line's `prev_hash`. Any mismatch = tamper point at `seq`.

## Event types (v0)

| `type` | `payload` fields | Meaning |
|---|---|---|
| `session/start` | `harness`, `policy_id`, `policy_hash` | Recording begins; pins the policy in effect |
| `tools/call` | `id` (JSON-RPC id), `tool`, `args`, `tool_descriptor_hash` | Agent invoked a tool |
| `policy/decision` | `id`, `verdict` (`allow`/`confirm`/`deny`), `rule_id`, `reason` | Verdict for the call with the same `id` |
| `tools/result` | `id`, `is_error`, `result` | Tool execution result (redacted) |
| `session/end` | `reason` | Recording ends |

## Redaction

Applied **before** hashing (the log stores only redacted data):
- Known secret patterns (AWS keys, GitHub/Slack tokens, bearer tokens, PEM blocks, …) → `[REDACTED]`
- JSON fields named like secrets (`password`, `api_key`, `token`, `secret`, `authorization`, …) → `[REDACTED]`
- Redaction is deterministic (same input → same output) so replay matching stays stable.

## Compatibility

- Writers MUST set `v: 0` and follow the canonical form exactly, or verification will fail.
- Readers SHOULD ignore unknown `payload` fields (forward compatible).
- v1 will add replay metadata (clock/entropy virtualization records) — additive fields only.
