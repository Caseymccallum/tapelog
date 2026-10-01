# Threat Model

**Scope:** tapelog v0.x mediates MCP tool traffic passing through its proxy. Claims below apply only to that mediated traffic.

## Core assumption: the untrusted model

The language model driving the agent may be **fully attacker-controlled** (e.g., via indirect prompt injection in a tool result, web page, or document). The boundary must hold even then:

> A prompt-injected agent must be unable to exceed the authority explicitly delegated to it by policy.

We do not try to detect malicious *reasoning*; we constrain *actions*.

## In scope — what v0 defends against

| # | Attack class | Defense in v0/v1 |
|---|---|---|
| 1 | Excessive agency — agent calls dangerous tools | Policy allow/deny/confirm on `tools/call`, evaluated **before side effects**; fail-closed default; `confirm` routes to a **human prompt** (allow once / allow session / deny) |
| 2 | MCP tool poisoning / rug pulls — server silently changes tool behavior between calls | Tool-descriptor hash pinning; drift recorded + flagged; `--deny-on-drift` denies (race-free: calls wait for in-flight listings) |
| 3 | Cross-tool exfiltration ("toxic flows") — `read_x` + `send_y` compose into theft though each is allowed | **Session-scoped taint flow rules** (`flows:` in policy): source→sink restrictions with named taint sources; `action: deny` or `confirm` |
| 4 | Residual authority replay — a stale grant is reused later | Per-task scoped grants with expiry (policy `expires` + `tasks`) |
| 5 | "What happened?" blindness — no forensic trail | Hash-chained, append-only log (`tapelog verify` detects modification/deletion/reordering, including recomputed-hash edits of individual events; whole-log rewrites need an external anchor — the printed chain head with `verify --expect`, or a **signed checkpoint** with `verify --checkpoint`, whose transparency witness proves *when* the head existed) |
| 6 | Log credential leakage — traces leak secrets | Deterministic `[REDACTED]` masking of known secret patterns + field names (before hashing) |
| 7 | Tool server over-reach — a mediated server touches files outside its lane | **OS sandbox (defense-in-depth):** spawned servers run under Landlock filesystem restrictions (`--sandbox-ro`/`--sandbox-rw`, Linux; re-exec + syscall.Exec, strict by default) |
| 8 | Exfiltration via non-tool surfaces — `resources/read` / `prompts/get` bypass tool policy | **No unmediated surface:** every client request except the protocol plumbing (`initialize`/`ping`/`tools/list`/`server/discover` — no agent-facing payloads) is mediated as a surface call (same pipeline: policy, flows, limits, confirm, recording) |
| 9 | Tool results carry injected instructions ("ignore previous instructions") | **Injection scanning** of results (heuristic markers; `injection: mode: log` default, `confirm`/`deny` opt-in — blocked delivery with the original still recorded as evidence) |

## Out of scope — honest non-claims

- **Harness built-in tools.** tapelog mediates the MCP traffic between
  an agent harness and MCP servers. File/exec/search tools built into
  the harness itself (Roo Code, Cline, Claude Code, …) never cross the
  boundary: an agent that edits a file with its native tool is neither
  recorded nor enforced. Route risky capabilities through MCP servers,
  and/or restrict built-ins at the harness layer.

- **Prompt injection at the model** (detecting/filtering malicious instructions) — optional defense-in-depth only; not our claim.
- **Sandbox escape.** With `--sandbox-*` flags, mediated servers are Landlock-restricted (Linux). We make no claims about kernel exploits or Landlock bypasses; without the flags, a tool that is *allowed* runs with the server's full privileges. Use OS-level sandboxing (containers, seatbelt/landlock) alongside us.
- **The approval queue's transport.** `--approval-listen` serves plain HTTP with a localhost-first trust model: anyone who can reach the socket can decide parked approvals (set `--approval-token`, and/or bind behind your own TLS). Decisions are authenticated only by reachability + optional bearer token.
- **The web dashboard renders attacker-controlled data.** Tool names/args/results are displayed with `textContent` only under a `default-src 'self'` CSP, with loopback Host pinning (DNS rebinding) and same-origin-only POSTs (CSRF). These are defenses, not proofs — report any bypass as a security issue (SECURITY.md).
- **Value-level taint is heuristic, not proof.** `flows: mode: value`
  detects contamination by substring matching of recorded values;
  transformations (base64, paraphrase, splitting) evade it — the model
  can carry data beyond recognition. Must-never flows need `mode: session`
  rules (and even those assume the model *can* exfiltrate via any allowed
  sink — see the toxic-flow claim above).
- **Semantic correctness.** We record and constrain; we don't judge whether an action is *wise*, only whether policy permits it.
- **Toxic flows (value level).** Flow rules track taint per session (conservative). Tracking specific values *through the model* (CaMeL-style capabilities) is out of scope — the model's internal data flow is opaque to the boundary.
- **Log confidentiality.** Redaction is best-effort pattern matching. Treat logs as sensitive.
- **Multi-agent / A2A delegation.**
- **Kernel-level enforcement** (compromised tapelog process itself). v0 is userland.

## Attack surfaces we inherit (MCP, spec 2026-07-28)

Confused deputy, token passthrough, SSRF via tool servers, state-handle hijacking, tool descriptor untrustworthiness. tapelog reduces exposure by mediating calls and pinning descriptors, but correct **authorization** (which identity may do what) is delegated to the MCP client/server stack in v0.

## Reporting

Found a vulnerability in tapelog itself? See [SECURITY.md](../SECURITY.md).
