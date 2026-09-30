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

1. **Keep tapelog as the mainline** and evolve it into lane 1 + 2
   (`tapelog test` + `tapelog fuzz`) — the market is arriving at our
   doorstep and the hard parts are already built. Release v0.1.0 as planned.
2. **Fresh project for variety**: idea #1 scoped sharp (a code *reader*
   TUI, not a graph tool) is the only one that survived re-validation.
   Start it only if the variety is the point — its window is narrowing.
