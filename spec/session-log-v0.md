# Agent Session Log Format — Version 0

**Spec status:** Working Draft · **Schema version:** `v: 0` · 2026-09-29
**Reference implementation:** [tapelog](https://github.com/Caseymccallum/tapelog)
**Machine-readable schema:** [schema/session-event.v0.schema.json](schema/session-event.v0.schema.json)
**Conformance test vectors:** [test-vectors/](test-vectors/)

The Agent Session Log Format records what AI agents *did*: every tool
call, the policy verdict it received, and the result it produced — in a
single append-only file that is portable, tamper-evident, and replayable.

The key words MUST, MUST NOT, SHOULD, SHOULD NOT, and MAY are to be
interpreted as described in RFC 2119.

## 1. Terminology

| Term | Meaning |
|---|---|
| **session log** | One file containing one agent session |
| **event** | One JSON object on one line of the file |
| **canonical form** | The exact byte string hashed for integrity (§4) |
| **hash chain** | Per-event linkage via `prev_hash`/`hash` (§5) |
| **writer** | Software that produces session logs |
| **verifier** | Software that checks the hash chain |
| **replayer** | Software that re-executes a session from the log (optional) |
| **redaction** | Deterministic masking of secret material (§7) |

## 2. File format

A session log is **JSON Lines** (one JSON object per line, UTF-8,
`\n`-terminated):

- Writers MUST write exactly one event per line and MUST NOT rewrite
  existing lines (append-only).
- Lines MUST NOT be empty. Readers MUST ignore a trailing empty line.
- Each event MUST conform to [the JSON Schema](schema/session-event.v0.schema.json).

## 3. Event envelope

```json
{
  "v": 0,
  "seq": 3,
  "ts": "2026-09-29T12:34:56.789Z",
  "type": "tools/call",
  "session_id": "20260929T123456.789-0b3132d234c1af4c",
  "payload": { "id": 2, "tool": "read_file", "args": { "path": "/tmp/a" } },
  "prev_hash": "9f2a…",
  "hash": "c41d…"
}
```

| Field | Type | Requirement |
|---|---|---|
| `v` | integer | Schema version. This document defines `0`. |
| `seq` | integer ≥ 1 | Monotonic within the session, starting at 1, contiguous |
| `ts` | string | RFC 3339 UTC timestamp, **millisecond** precision (`2006-01-02T15:04:05.000Z`) |
| `type` | string | Event type (§6) |
| `session_id` | string | Identifies the session; all events in a file share it |
| `payload` | object | Type-specific data; readers MUST ignore unknown fields |
| `prev_hash` | string | Hex SHA-256 of the previous event's `hash`; `""` when `seq` = 1 |
| `hash` | string | Hex SHA-256 of this event's canonical form (§4) |

## 4. Canonical form (normative)

The canonical form is the ASCII concatenation of the following fields,
each as `key=value` separated by `\n`, with **no trailing newline**:

```
v=<v>\nseq=<seq>\nts=<ts>\ntype=<type>\nsession_id=<session_id>\nprev_hash=<prev_hash>\npayload=<canonical-payload>
```

Integer and string field values are inserted verbatim (no quoting;
timestamps and ids MUST NOT contain `\n`).

**Canonical payload** serialization:

1. Object keys MUST be sorted lexicographically (by UTF-16 code units /
  code points — for ASCII, byte order) at **all** nesting depths.
2. No insignificant whitespace.
3. Strings MUST be encoded as JSON strings with **HTML escaping disabled**
  (`<`, `>`, `&` appear literally; `\u003c`-style escaping is invalid).
4. Standard JSON escaping applies otherwise (`\"`, `\\`, `\b`, `\f`, `\n`,
  `\r`, `\t`, and control characters as `\u00XX`).
5. Number literals MUST be preserved verbatim from the event's `payload`
  as written in the file (writers SHOULD store integers as JSON integers;
  readers implementing the hash MUST parse numbers losslessly, e.g. via a
  "raw number" type).
6. JSON `null` is serialized as `null`.

Any byte difference in the canonical form produces a different hash.
Vectors in [test-vectors/](test-vectors/) pin these rules.

## 5. Hash chain & verification (normative)

```
hash = hex(SHA-256(canonical form))
prev_hash(event n) = hash(event n-1), or "" when n = 1
```

A verifier MUST, for each event in order, check:

1. `seq` equals the expected next number (1, then +1 each event)
2. `v` is a supported version
3. `prev_hash` equals the previous event's `hash`
4. `hash` equals the recomputed hash of the event's canonical form

Failure of any check identifies the tampering point: the first failing
`seq`. Modifications, deletions, and reordering are all detectable; an
attacker who rewrites an event *and* its `hash` is still exposed by the
next event's `prev_hash` (to repair the chain they must rewrite every
subsequent event — and a verifier holding any earlier hash, or any
external copy, still detects it). This is tamper-*evidence*, not
tamper-proofing: an attacker who can rewrite the entire file can forge a
consistent chain. Anchor trust externally when that matters (publishing
the head hash, signing, a collector).

## 6. Event types (v0)

| `type` | `payload` | Notes |
|---|---|---|
| `session/start` | `harness`, `policy_id`, `policy_hash` | SHOULD be first; pins context |
| `tools/list` | `tools` (array of descriptors) | Advertised tool catalog (redacted) |
| `tools/call` | `id`, `tool`, `args`, `tool_descriptor_hash?`, `descriptor_drift?` | `id` is the transport JSON-RPC id |
| `policy/decision` | `id`, `verdict`, `rule_id`, `reason` | `verdict` ∈ `allow` \| `confirm` \| `deny` |
| `tools/result` | `id`, `is_error`, `result` | `result` carries the error object when `is_error` |
| `session/end` | `reason` | SHOULD be last |

Writers MUST emit `policy/decision` for every `tools/call`. `tools/result`
is omitted when a call never executed (e.g. denied). Unknown future
`type` values MUST be ignored by readers (forward compatibility).

### 6.1 Causation & correlation (optional, additive)

All `tools/call`, `policy/decision`, and `tools/result` payloads MAY
carry two optional linkage fields (readers MUST ignore them when
absent; they are additive and were introduced while `v: 0`):

- `parent_seq` (integer): the `seq` of the event that *caused* this
  event. A `policy/decision` or `tools/result` SHOULD point at its
  `tools/call` event; an async continuation (task progress, notification
  follow-up) points at the event that spawned it. Absent = no known
  parent (e.g. the initiating call).
- `traceparent` (string): the W3C `traceparent` value copied verbatim
  from the request's `_meta.traceparent` when the transport carried one
  (MCP `_meta` passthrough). Correlation only — NOT integrity-protected
  beyond the hash chain (it is inside the hashed payload).

Neither field changes the canonical form rules (§4): they are ordinary
payload keys hashed with the rest. A future `v: 0` async event type can
attach to an existing call via `parent_seq` without a schema break.

### 6.2 Large payloads: blob references (optional conformance class: **BlobStore**)

Any `args` or `result` value MAY instead be a **blob reference**
placeholder:

```json
{"$blob": {"sha256": "<64 hex>", "size": <bytes>}}
```

The referenced bytes live out-of-band (tapelog: a content-addressed
`<log>.blobs/` directory, `<digest>.blob` files). Rules:

1. The placeholder is what enters the canonical form and the hash chain:
   the chain therefore binds the *digest*, so replacing the out-of-band
   blob is detectable (recompute SHA-256 of the bytes and compare).
2. A reader that resolves blobs MUST verify the digest and MUST fail
   loudly on a missing or mismatched blob — it MUST NOT replay, assert
   over, or display the placeholder as if it were content.
3. A reader that does NOT resolve blobs (e.g. a pure chain verifier) can
   still verify the chain; it SHOULD report unresolvable references when
   the consumer asked for content.
4. Writers SHOULD only offload `args`/`result` (payload fields whose
   values are content), never structural fields (`id`, `tool`, …).

tapelog implements this via `record --blob-threshold N` (off by
default); `tapelog verify` checks blob digests whenever the store is
present. Replay, `tapelog test`, and `whatif` resolve placeholders from
the store and fail loud when it is missing.

## 7. Redaction (normative requirement, informative patterns)

Writers MUST NOT store secret material in the log:

- Redaction MUST be **deterministic** — identical input redacts
  identically — so replay matching (§8) remains stable.
- Redaction MUST be applied **before** hashing.
- Writers SHOULD mask values of object keys whose names suggest secrets
  (`password`, `token`, `api_key`, `secret`, `authorization`, …) and
  known credential patterns (API keys, bearer tokens, PEM blocks).
- Redaction is best-effort; logs SHOULD still be treated as sensitive.

The replacement marker is conventionally `[REDACTED]` (not required).

## 8. Replay semantics (optional conformance class: **Replayer**)

A replayer re-executes a session from its log as a hermetic tool server:

1. Matching of live calls to recorded `tools/call` events MUST run on
   *redacted canonical* arguments (VCR semantics — fresh secrets still
   match their recording).
2. Each recorded interaction MUST be consumed at most once (FIFO).
3. Replayers MUST fail loudly on unmatched calls (structured error) and
   MUST NOT invent results.
4. Recorded `is_error` outcomes MUST replay as errors.
5. Denied calls (no `tools/result`) MUST NOT replay as successes.

## 9. Versioning & compatibility

- The `v` field is the schema version. Additive changes (new event types,
  new payload fields) do **not** bump `v`; readers ignore unknowns.
- Any change to the canonical form (§4) or hash chain (§5) is **breaking**
  and MUST bump `v`.
- Verifiers MUST reject unsupported `v` values.

## 10. Security considerations

- The log is evidence about an agent, therefore a **target**: treat as
  sensitive (§7), protect against rollback (external anchoring, §5), and
  note that `policy/decision` reasons may quote tool arguments.
- The format records what crossed a mediation boundary; it makes no claim
  about the *correctness* of the agent's behavior, only about what
  happened and what was permitted.

## 11. Conformance

| Class | Requirements |
|---|---|
| **Writer** | §2, §3, §4, §6, §7 |
| **Verifier** | §5 (plus Writer, or read-only) |
| **Reader** | §2, §3, §6 (forward compatibility) |
| **Replayer** | §8 |

Implementations SHOULD validate against
[schema/session-event.v0.schema.json](schema/session-event.v0.schema.json)
and MUST pass [test-vectors/](test-vectors/).

