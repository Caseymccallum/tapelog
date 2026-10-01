# Show HN launch kit

## Title options (pick one at post time)

1. **Show HN: rr for AI agents – deterministic replay of tool-calling sessions**
2. **Show HN: Tapelog – flight recorder, policy boundary and replay for AI agents**
3. **Show HN: A tamper-evident flight recorder for AI agents (VCR for agent sessions)**
4. **Show HN: Tapelog – tamper-evident replayable logs for MCP agent tool calls, with regression CI**

Recommendation: #1 — it leads with the *developer* story (replay), which
resonates more than security framing, and "rr" is a known-good analogy.
#4 leads with the regression-CI angle if the discussion skews CI/testing.

## Post body (draft)

> We kept debugging agents by printf-ing JSON blobs, so we built tapelog:
> a local-first boundary layer between agent harnesses (Claude Code, Codex
> CLI, anything MCP) and their tool servers.
>
> Four things, one binary:
>
> 1. **Record everything.** Every tool call, result and policy verdict goes
>    into a hash-chained session log. Secrets are redacted before hashing;
>    `tapelog verify` detects any modification, deletion or reordering —
>    including edited events whose hashes were recomputed (the next link
>    exposes them). An attacker who rewrites the *entire* log can produce a
>    self-consistent chain, so each run prints a chain head — record it
>    somewhere outside the log (CI output, a ticket) and `verify --expect`
>    catches whole-log rewrites and truncation too.
> 2. **A hard boundary.** YAML policies (`allow`/`confirm`/`deny`) with
>    explainable decisions, scoped grants with expiry, and argument
>    conditions written in Cedar (we didn't invent an expression language).
>    A denied call never reaches the server. Tool descriptors are
>    hash-pinned — if a server changes its tool's behavior mid-session
>    (poisoning/rug pull), `--deny-on-drift` stops it.
> 3. **Deterministic replay.** The log *is* a tapelog: `tapelog replay`
>    serves a recorded session as a hermetic MCP server for agent
>    regression tests. VCR semantics: redaction-aware matching (a fresh
>    secret still matches its recording), consume-once, fail-loud on
>    missing recordings. Plus `diff` and `policy whatif` for CI.
> 4. **Regression CI for tool trajectories.** `tapelog test` asserts over
>    recorded sessions (called/never_called/sequence/invariants — no LLM
>    judge), `tapelog fuzz` mutates recorded calls hunting policy holes,
>    `tapelog doctor` preflights the setup. The eval wave scores prompts;
>    nobody scores the tool-call trajectory.
>
> There's also a TUI inspector and OTLP export (GenAI semconv spans).
>
> We did the market research first (full report in the repo): enterprise
> "agent governance" platforms cover policy+audit, and observability tools
> cover traces, but nobody shipped *replay*. We think that's the missing
> primitive — it's the one feature every debugging session wants.
>
> Honest non-claims (docs/THREAT_MODEL.md): we don't sandbox tool
> execution by default (opt-in Landlock on Linux), we don't filter prompt
> injection at the model, and value-level toxic-flow tracking is
> heuristic (substring contamination matching — transformations evade
> it; session-scoped flow rules are the must-never case). We constrain
> and record actions; the model stays untrusted.
>
> Apache-2.0, single static binary, no telemetry. Would love feedback —
> especially from people building MCP servers: what would you want from
> the boundary layer? [GitHub link]

## Launch checklist

- [ ] Final name check (GitHub org, npm, crates.io, PyPI) — currently `tapelog` (working name)
- [ ] Record the demo GIF (see examples/rogue-agent/README.md)
- [ ] `GOVERNANCE`/`SECURITY` contacts: replace placeholder emails
- [x] Tags + signed releases — v0.1.0/v0.2.0 tagged; goreleaser (SBOM + cosign) runs on each `v*` tag
- [ ] Post at Tue–Thu, 8–10am ET; reply actively for the first 3 hours
- [ ] Cross-posts: MCP Discord, r/LocalLLaMA, r/ExperiencedDevs, OTel community
- [x] Publish the session-log spec standalone → **`spec/` (normative `session-log-v0.md` + JSON Schema + conformance test vectors); community-process post at launch**

## Talking points / likely questions

- **"How is this different from Microsoft's Agent Governance Toolkit?"**
  AGT is a governance platform (SDK integration, compliance); tapelog is a
  single-binary dev tool. Replay + policy-whatif exist in neither. Our log
  format is interop-friendly (OTel export, portable Cedar export).
- **"Why not just Langfuse/Phoenix?"** Observability answers "what happened";
  replay answers "make it happen again, exactly" — offline, in CI.
- **"Is it production-ready?"** v0.x: the record/verify/policy path is solid
  and tested; replay semantics may evolve (matching rules). The log format
  is versioned (`v: 0`).
