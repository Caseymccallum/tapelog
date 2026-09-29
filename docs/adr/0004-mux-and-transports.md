# ADR 0004: Multi-server mux architecture & transport scope

**Status:** Accepted · 2026-09-29

## Context

Real harness setups run many MCP servers (5–10), some remote. Mediating
one server per `tapelog record` invocation does not fit; we need one
boundary covering all upstreams with one session log and one policy.

## Decision

1. **`tapelog mux --config mux.yaml`** aggregates upstreams (stdio
   commands and streamable-HTTP endpoints) behind one stdio MCP server.
2. **Tool namespacing**: upstream `read_file` on server `fs` becomes
   `fs__read_file`. Policy rules in mux mode match namespaced names
   (`*__read*`). Server names may not contain `__`.
3. **Shared mediation**: the decision pipeline (drift → flows → policy →
   confirm → taint → logging) lives in `internal/mediator` and is used by
   both `record` and `mux` — enforcement semantics cannot diverge.
4. **Strictly ordered processing** of harness requests: deterministic
   taint transitions and decision ordering beat parallel throughput at
   the boundary. (Upstream calls are therefore serialized per session.)
5. **Live listings**: each `tools/list` re-fetches upstream listings and
   re-pins descriptors, so `--deny-on-drift` works across the mux.
6. **Transports**: `internal/transport` — stdio subprocesses and minimal
   streamable-HTTP (POST + JSON/SSE responses). Server-initiated messages
   over the HTTP GET/SSE channel are **not** supported in v1 (requests we
   send are answered on their POST); this covers initialize/list/call.

## Consequences

- A mux policy is namespaced; migrating a single-server policy requires
  pattern updates (documented in docs/POLICY.md).
- Serialized calls mean one slow upstream stalls the session — acceptable
  for a mediation boundary; revisit with per-upstream queues if needed.
- The hand-rolled transports can be swapped for `modelcontextprotocol/go-sdk`
  transports later without changing the mux or mediator.
