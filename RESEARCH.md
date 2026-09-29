# Research Report: Open-Source AI Agent Execution Boundary Layer
**Compiled:** 2026-09-29 · Method: live competitive research (GitHub, product sites, MCP spec, HN), two parallel deep-research passes + independent verification.

---

## 1. Executive Summary

**Original concept:** a runtime that sits between an AI agent and its tools — traces every call, enforces allow/deny policy ("a hard no"), and supports replay of sessions.

**Key strategic finding:** the *policy-enforcement* half of this idea is being land-grabbed by giants. Microsoft shipped the **Agent Governance Toolkit (AGT)** — 6.4k stars, 2,774 commits, MIT, multi-language SDKs, Rego/Cedar policy engine, MCP Security Gateway, tamper-evident audit, sandboxing, OWASP/EU AI Act/SOC 2 compliance mappings. Every 2024-era LLM-firewall startup got acquired (CalypsoAI→F5, ProtectAI→Palo Alto, Lakera→Check Point, Invariant/mcp-scan→Snyk, Robust Intelligence→Cisco). **Do not build "another policy firewall" — that's competing with Microsoft + four security vendors for free.**

**The whitespace that remains (verified against AGT's own Known Limitations doc):**

1. **Deterministic session replay — nobody does it.** Not AGT, not sandbox vendors (Daytona/E2B/Morph), not observability vendors (Langfuse/Phoenix). This is the gap.
2. **Cross-action "toxic flow" enforcement** — AGT limitation #1, verbatim: it cannot "correlate sequences of individually-allowed actions that form a malicious workflow" (e.g., `read_database` + `send_slack_message` = exfiltration, both individually permitted). CaMeL-style taint tracking is research-grade with no production OSS.
3. **The indie/local-first developer tool position.** AGT is an enterprise compliance platform (60+ tutorials, DIDs, kill-switch SDKs). The "small sharp tool" slot — single binary, zero SDK, great UX — is open. Grassroots demand exists but current dabblers are 0–2-star hobby projects (MCPWarden, mcp-policy-firewall).
4. **Cross-harness, one-policy-file coverage.** Every enforcement tool is siloed (Codex→Codex, sandbox-runtime→Claude Code, MCPGuard→Linux MCP). A transparent MCP proxy needs no integration and covers any harness by construction.

**Recommended pivot — replay-first:** build the **flight recorder / `rr` for AI agents**. A tamper-evident session log that gives you (a) deterministic replay for debugging & regression-testing agents, (b) audit evidence for security/compliance, (c) policy verdicts in the same log, expandable into toxic-flow analysis later. Replay serves *developers* (huge audience) not just security teams, and it's the one capability nobody — including Microsoft — has shipped.

**Timing tailwind:** the MCP spec revision **2026-07-28 deprecated server logging and explicitly says "use OpenTelemetry for observability."** An OTel-aligned recorder/replayer is exactly what the ecosystem is being pushed toward.

---

## 2. Market Landscape (as of 2026-09-29)

### 2.1 Consolidation timeline
| Date | Event |
|---|---|
| 2025-04 | Invariant discloses MCP tool poisoning; ships `mcp-scan` |
| 2025 | F5 acquires CalypsoAI; Check Point acquires Lakera; Palo Alto acquires ProtectAI; Cisco absorbs Robust Intelligence [deal unverified, product confirmed] |
| 2025-06 | Snyk absorbs Invariant Labs; `mcp-scan` → **Snyk Agent Scan** |
| 2025-10 | Anthropic `sandbox-runtime` (`srt`) research preview → cross-platform 2026 |
| 2026-05–07 | Meta publishes **`mcpguard-dynamic`** — eBPF deny-by-default enforcement for MCP tool calls (Linux-only research code) |
| 2026-06 | **Daytona moves core private**; OSS frozen at AGPL v0.190.0 (cautionary tale for licensing/CLA) |
| 2026-07-28 | **MCP spec `2026-07-28`** — stateless protocol, MRTR pattern, deprecations incl. logging → OpenTelemetry |
| 2026 | Stacklok archives CodeGate → ToolHive MCP gateway + official MCP registry |
| 2026 | Langfuse joins ClickHouse; Langfuse v4; Phoenix remains ELv2 (note: *not* OSI-approved) |
| 2026-09 | Microsoft **AGT** active (6.4k★); NeMo Guardrails v0.24.1; Honeycomb AI Ecosystem launch |

### 2.2 Player map (capability coverage)
| Player | Trace | Policy allow/deny | Hard enforcement | Deterministic replay | Note |
|---|---|---|---|---|---|
| Microsoft AGT | ✅ tamper-evident audit | ✅ Rego/Cedar/custom | ✅ sandbox + kill switch | ❌ | Enterprise SDK-first; limitation: no flow correlation |
| Anthropic `sandbox-runtime` | partial | OS-level (fs/net) | ✅ | ❌ | File/network rules, not tool-call semantics |
| Meta `mcpguard-dynamic` | partial | ✅ kernel eBPF deny-by-default | ✅ | ❌ | Linux + MCP only; research-grade |
| Daytona / E2B / Morph | snapshots | ❌ | ✅ (where code runs) | snapshot ≠ replay | Daytona OSS dead-ended (AGPL) |
| Langfuse / Phoenix / OTel | ✅ behavior | ❌ | ❌ | ❌ | Observability only; Langfuse now ClickHouse-owned |
| Snyk Agent Scan / ToolHive | scan-time | catalog curation | ❌ | ❌ | Supply-chain scanning, not runtime |
| NeMo Guardrails / LlamaFirewall / Guardrails AI | partial | LLM-judged content rails | ❌ | ❌ | Input/output *content* filtering, not tool-call mediation |
| Grassroots firewalls (MCPWarden 0★, mcp-policy-firewall 2★) | JSONL audit | YAML patterns | ❌ | ❌ | Hobby-grade; proof the grassroots want this |

---

## 3. Deep-Dive: Microsoft AGT (the 800-lb gorilla)

**What it is:** ACS (Agent Control Specification) policy *decision* layer; hosts apply verdicts (allow / transform / deny / escalate). Language SDKs (Python, TS, .NET, Go, Rust), framework adapters (LangChain, CrewAI, OpenAI Agents, LangGraph, PydanticAI, Google ADK…), CLI governance for Claude Code / Codex CLI / Copilot CLI / OpenCode / Antigravity, MCP Security Gateway, identity (DIDs, trust scores), tamper-evident audit + offline verifiable receipts, execution sandboxing, kill switch, OTel observability, conformance studio, compliance mappings (OWASP ASI, NIST AI RMF, EU AI Act, SOC 2, ISO 42001).

**Its own "Known Limitations" (docs/LIMITATIONS.md) — our target list:**
1. **Action governance, not reasoning governance** — does not detect indirect prompt injection; does **not correlate sequences of individually-allowed actions into malicious workflows** (their example: `read_database` + `send_slack_message` exfiltration; also a *cross-session* variant). ← biggest hole; this is the CaMeL "toxic flow" problem.
2. (Further sections incl.) DID format mismatches across SDKs; **kill-switch semantics don't prove process termination** (`terminated=False` paths); SDK-cooperative termination only.
3. Requires integration (SDK wrappers `govern()` / adapters) — not a fully transparent harness-agnostic proxy (though it has sidecar + CLI governance modes).

**What AGT never claims anywhere in its docs: deterministic replay / forensic session re-execution.** Audit ≠ replay: receipts prove *what was logged*; replay *re-executes* the session for debugging, testing, and policy what-if analysis.

**Takeaway:** build *on* AGT's gravity, not against it — align our log schema with ACS/AOS event contracts and OTel so we can ingest/export their audit events. "The missing replay layer" is a friendly, complementary position that Microsoft itself could adopt us for.

---

## 4. Standards & Interoperability Targets

- **MCP spec `2026-07-28`** (current): stateless, `_meta` self-description, MRTR, stateful-tool handles (**new "State Handle Hijacking" attack** to defend), tool annotations explicitly *untrusted*, "there SHOULD always be a human in the loop with the ability to deny tool invocations". Deprecated: `logging` → **"use OpenTelemetry"**, sampling, roots, HTTP+SSE. Security-best-practices attack classes: confused deputy, token passthrough, SSRF, tool poisoning/rug pulls.
- **OpenTelemetry GenAI semantic conventions** — the canonical trace shape; align spans to tool calls; propagate context via `_meta`. MCP interceptor standardization (SEP-1763) is in flight — plumb ahead of finalization.
- **Threat model (v1 claims):** the **untrusted-model assumption** — a fully prompt-injected agent must be unable to exceed delegated authority. v1 mediates & records every MCP tool call: (a) allow/deny policy w/ explainable deny, (b) egress/scope constraints, (c) tool-descriptor hash pinning → rug-pull detection, (d) per-task scoped grants with expiry (mitigates residual-authority replay), (e) tamper-evident hash-chained log (ATLAS M0029/M0030/M0033 evidence). **Explicitly out of scope v1:** prompt-injection *prevention* at the model, sandbox-escape guarantees, A2A delegation, semantic correctness of agent behavior, toxic-flow *blocking* (schema must *support* it for v2).
- **Replay prior art:** `llm-vcr` / VCR-cassette semantics — canonical hash keys for matching, matching rules, redaction, fail-loud on missing cassettes. Steal these semantics wholesale; apply to *tool calls* at session scope.
- **Policy engines:** **Cedar** (fine-grained, explainable, embeddable) as decision engine; a human-readable YAML/DSL front-end compiling to Cedar. Defense-in-depth enforcement: seccomp / Landlock / Wasm-WASI / gVisor / Firecracker (all reusable unmodified). Avoid OPA/Rego as the *user-facing* language (poor DX for this audience) — offer Rego only as an escape hatch.

---

## 5. The Product: "Flight recorder & deterministic replay for AI agents"

**One-liner:** `rr` for AI agents — record every tool call your agents make, replay any session deterministically, and prove exactly what they could and couldn't do.

**Positioning:**
| | AGT / enterprise vendors | Observability (Langfuse/Phoenix) | **Us** |
|---|---|---|---|
| Audience | compliance, security teams | ML engineers | agent developers |
| Shape | SDK + platform | hosted/SaaS-leaning | single binary, local-first |
| Verdict | allow/deny + audit | none | allow/deny + **replay** |
| Killer feature | compliance matrices | dashboards | time-travel debugging & offline re-execution |

**Why replay-first wins:**
1. **Nobody has it** (verified: AGT limits, sandbox vendors, obs vendors all ❌).
2. **Developer audience** — everyone debugging agents today is printf-ing JSON blobs. Replay gives them time-travel + regression cassettes ("VCR for agents") — much bigger pull than security tooling alone.
3. **The log doubles as audit evidence** — the tamper-evident hash chain satisfies the security/compliance story without building a compliance platform.
4. **Natural expansion path** — v2: policy what-if re-evaluation ("replay this session against policy vX and diff the verdicts"), toxic-flow detection over recorded traces (AGT's admitted gap), CI integration (fail builds on verdict regressions).
5. **Interop-friendly** — OTel export (spec-mandated direction), AGT/ACS event import; we become the missing layer in everyone else's stack.

### Architecture (v1)
```
any harness (Claude Code / Codex CLI / custom agent / MCP client)
        │  MCP (stdio or HTTP)
        ▼
┌───────────────────────────────┐
│  boundary proxy (single bin)  │
│  1. intercept tool call       │
│  2. policy check (Cedar) ──────── verdict: allow / confirm / deny (+ reason)
│  3. hash-pin tool descriptor  │   (rug-pull detection)
│  4. forward to real MCP server│
│  5. append to session log     │   hash-chained, redacted, OTel-exportable
└───────────────────────────────┘
        │
        ▼
  session log ("cassette")  →  replay engine (deterministic re-execution,
                                matching rules, fail-loud, policy what-if)
```

### MVP scope — 4 weeks to Show HN
| Week | Deliverable |
|---|---|
| 1 | Session event schema + recorder: MCP proxy (stdio+HTTP), hash-chained JSONL log, secret redaction |
| 2 | Replay engine: cassette format (canonical arg hashing, matching rules, fail-loud), `record` / `replay` / `verify` CLI |
| 3 | Policy layer v0: YAML allow/deny/require-confirm w/ explainable deny reasons (Cedar under the hood), verdicts in the same log, scoped grants w/ expiry |
| 4 | TUI session viewer / scrubber, OTel export, tool-descriptor pinning + drift alerts, docs + Show HN post |

**Non-goals v1:** hosted service, web UI, LLM-judged content filtering, multi-agent/A2A, kernel-level enforcement (cross-platform proxy yes; eBPF later).

### Naming candidates (availability unverified — check GitHub/npm/crates.io)
`rewindd`, `agent-rr`, `cassette`, `tapelog`, `blackbox-ai`, `flightpath`, `devtrace`. Lean toward a name that says **replay**, not **firewall** (positioning matters: dev tool, not security theater).

### License & governance
- **Apache-2.0** (patent grant; corporate-friendly → company contributions; avoid AGPL à la Daytona dead-end; avoid ELv2 à la Phoenix).
- Ship `GOVERNANCE.md`, `SECURITY.md`, `CODE_OF_CONDUCT.md`, `CONTRIBUTING.md`, and a threat-model doc from day one — this audience reads those before starring.
- Signed commits, SBOM, cosign-signed releases — credibility signals for a security-adjacent project.

### Launch strategy
1. **Show HN** framed as dev tool: *"Show HN: rr for AI agents – deterministic replay of tool-calling sessions"*. Replay demos beat policy sermons.
2. Demo GIF: agent goes rogue in a demo → scrub back → show the hard deny → replay offline against a new policy and diff verdicts.
3. Cross-post: MCP Discord, r/LocalLLaMA, r/ExperiencedDevs, OTel community (they want GenAI use-cases).
4. Publish the session-log schema as a standalone spec early → invites ecosystem contributions and standardization attention.

---

## 6. Risks & Open Questions
1. **Microsoft adds replay to AGT.** Mitigation: stay a sharp single-purpose tool, stay harness-agnostic, ship a schema they can adopt; being adopted is a win, not a loss.
2. **Deterministic replay is genuinely hard** (tool side effects, clocks, network). Mitigation: replay *recorded* I/O first (VCR semantics — re-run the agent against mocked tool responses), call it "re-execution"; true side-effecting replay later. Be honest in docs.
3. **MCP churn** (2026-07-28 changed the protocol substantially). Mitigation: pin a spec version, abstract the transport layer.
4. **Language choice** — Go (proxy + CLI velocity, great OTel libs) vs Rust (safety story, static binary, WASM policy path). Decision needed based on your stack; cross-platform CLI is straightforward in both.
5. **How far to lean into security claims** — overclaiming invites scrutiny (this crowd destroys security theater). Recommend: "record everything, enforce what you can prove, replay the rest."

## 7. Verification Notes
- Verified live 2026-09-29: AGT scope + Known Limitations (GitHub + docs site), MCP 2026-07-28 changes (spec changelog), Daytona closure, Snyk/agent-scan redirect, MCPWarden (0★) & mcp-policy-firewall (2★) hobby-grade status, `agent-vcr` repo does not exist (404 — name likely available).
- Unverified: Cisco–Robust Intelligence deal terms, Morph internals, exact OWASP Agentic Top 10 entry names, `ProSecConf`/`ToolFilter` papers (couldn't locate — likely misremembered titles), name availability on package registries.

## 8. Next Steps (proposed)
1. Choose language (Go vs Rust) + confirm name availability on GitHub/npm/crates.io.
2. Write the session-event schema v0 draft (the artifact most likely to become a standard).
3. Scaffold repo with governance docs + threat model; implement week-1 recorder MVP.
4. Pick 2–3 real MCP servers (filesystem, fetch, git) for the demo; build the "rogue agent" demo script early — it drives both development and marketing.



