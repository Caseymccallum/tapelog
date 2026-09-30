# Evolution research — 2026-09-30 (post-v0.1.0)

Method: live web research (market news, GitHub, PyPI) on where the agent
tooling space is heading, plus a re-validation of the two unused ideas
from the original brainstorm (RESEARCH.md). Verified live this date.

## The market is moving toward tapelog, not away

- **Agentic security is consolidating hard**: $3.6B+ funding and ~$96B M&A
  in the category; Palo Alto bought Portkey (MCP gateway), F5 bought
  SurePath AI; earlier: Cisco/Robust, Check Point/Lakera, Palo Alto/CalypsoAI.
  Enterprise gateways are becoming an infrastructure category — the
  enterprise lane is being bought, not built by indies.
- **Agent identity** is the hottest sub-area (IDSync's 2026 report) — the
  one "gateway job" we deliberately skipped.
- **Agent evals / replay / regression testing is exploding**: a wave of new
  repos (agentclash, tracegym, agenthound, tracecase, agentpytest, replayd,
  agent-replay, agentoptics/rewind …) — noisy small projects, i.e. demand
  is real but unconsolidated. **Nobody in that wave has our foundation**
  (tamper-evident log + policy + replay engine + spec).
- Security *testing* tooling has strong pull (aragentsec/agent-security-testing
  mapped to MITRE ATLAS, ~1.7k stars).

## tapelog evolution — ranked lanes

| Lane | Evidence | Our leverage | Verdict |
|---|---|---|---|
| **1. Agent CI/evals — `tapelog test`**: cassettes as hermetic agent-behavior regression fixtures; diff behaviors across runs/versions | Eval wave = screaming demand; LangSmith closed-source pushes users to OSS | Replay engine already built (nobody else has it); `diff` + `what-if` are half the product | **MAINLINE** |
| **2. Boundary hardening lab — `tapelog fuzz`**: mutate recorded sessions (tool names/args/taint order), assert the policy boundary holds; "property-based testing for agent policies" | Security-testing pull (ATLAS tooling popularity) | what-if + replay + policy test compose into this almost for free | High novelty, cheap |
| **3. Policy packs + spec push**: curated policies for the top-50 MCP servers; promote the Agent Session Log Format as the standard | Community ecosystem pattern (OPA, Semgrep rules) | spec + conformance vectors already exist | Community moat |
| **4. Identity/token exchange, SIEM export** | IDSync report; enterprise demand | Weak fit — this is what the acquired gateways sell | **Deprioritize** (leave to plugins/PRs) |

## Fresh-project re-validation (the unused top-3 ideas)

| Idea (original #) | Then | Now | Verdict |
|---|---|---|---|
| **Code-reading call-graph explorer (#1)** | Clear gap | **Neighbors filled**: `skeletree` (Rust MCP *context-packer* for agents, 0★ early), `codesight` (VS Code graph *map*, 5★), `codesextant` (PyPI, active), atlas/dekko/codegraph/codetree/srcwalk … — but **none is the reading-pane UX** (open one function, calls highlighted, click to open the callee beside it — the "Source Insight" experience). They serialize structure or draw maps; nobody ships the *reading* tool | **Still viable but narrowed** — must position as "the reader, not another graph map", ideally a sharp TUI (`tig`/`lazygit` school) |
| **CI step-debugger (#2)** | Clear gap | **Wedge crowded**: `wrkflw` (CLI+TUI runner, Terminal Trove coverage), `Rehearse` (hosted act alternative), `agent-ci.dev`, fermata, ciwalk … The *debugger* half (breakpoints, edit-and-continue) may still be open, but the category window narrowed a lot | **Deprioritized** — too crowded now |

## Recommendation

> *(2026-09-29 snapshot — v0.1.0 and v0.2.0 have since shipped; see
> docs/ROADMAP.md for current state.)*

1. **Keep tapelog as the mainline** and evolve it into lane 1 + 2
   (`tapelog test` + `tapelog fuzz`) — the market is arriving at our
   doorstep and the hard parts are already built. Release v0.1.0 as planned.
2. **Fresh project for variety**: idea #1 scoped sharp (a code *reader*
   TUI, not a graph tool) is the only one that survived re-validation.
   Start it only if the variety is the point — its window is narrowing.

## `tapelog test` — the scenario format (draft v0)

Differentiation vs. the eval wave (promptfoo/DeepEval/Braintrust/LangSmith —
research above): those are **prompt evaluators** (judge-scores, cost,
latency). We are **behavioral regression at the tool boundary**: assertions
on the actual tool-call trajectory, with hash-chained provenance, runnable
offline in CI. Keep the DSL a *thin* YAML over cassettes — deliberately
smaller than promptfoo's assert zoo.

```yaml
# agent-tests/ci-release.yaml
version: 1
scenario: release-agent
fixture: ../fixtures/release-session.jsonl   # recorded cassette (or `live:` block)
policy: ../policies/prod.yaml                # optional: assert verdicts under policy
assert:
  - called: publish_artifact
    args: { channel: "stable" }
    times: 1
  - never_called: "shell_*"
  - sequence: [build_artifact, test_artifact, publish_artifact]
  - taint_never: [read_secrets, "send_*"]   # no secrets ever flowed to a sink
  - result_contains: { tool: deploy, text: "success" }
  - allowed: deploy
  - denied: delete_db
  - invariants:                             # boundary invariants (also used by fuzz)
      - no_deny_bypassed
      - no_descriptor_drift
exit: non-zero on any failure (CI-native)
```

Design rules:
1. **Trajectory-first**: `called/never_called/args/sequence/taint_never`
   are exact, deterministic assertions. No LLM judge needed (optionally
   pluggable later — `assert.llm` behind a flag).
2. **Cassette-native**: `fixture` = a recorded tapelog session. Every run
   emits the cassette it exercised → failures are reproducible from CI
   artifacts alone.
3. **Policy-aware**: `policy:` re-runs the decision pipeline (what-if
   already does this) and asserts on verdicts (`allowed:`/`denied:`).
4. **CI-native**: non-zero exit on failure, `--plain` output, optional
   JUnit-ish XML later.

## `tapelog fuzz` — boundary hardening lab (draft v0)

Nobody fuzzes the agent↔tool boundary (research above: prompt fuzzing and
ATLAS attack generators exist; policy-boundary fuzzing does not).

- Input: a cassette + a policy.
- Mutation operators over recorded calls: argument tampering (type/size/
  unicode/traversal markers), tool-name spoofing & homoglyphs, reordering
  (toxic-flow permutation), taint injection (orders the original agent
  never tried), descriptor-drift replay, boundary-name games (`../`,
  `server__tool` confusion).
- **Oracle = the policy itself**: every mutant must either match the
  recorded verdict or trip a *documented* rule. A mutant that sails
  through a rule it should trip = finding (`no_deny_bypassed`).
- Output: reproducible findings (mutant cassette + the rule that broke),
  seed corpus exportable for CI regression; ATLAS mapping on findings
  (the aragentsec pattern) for security-team credibility.

