# Five minutes to proof

The success criteria, in order:

```text
0–1 min   Understand what tapelog is
1–2 min   Install it
2–3 min   Put it in front of one MCP server
3–4 min   Record one real interaction
4–5 min   Replay it
```

Prereqs: a terminal. You need an MCP agent harness you already use
(Roo Code / Cline / Claude Code / Cursor — anything that speaks MCP) —
if you don't have one at hand, jump to **No agent yet?** below.

## 0–1 min — What it is

A **flight recorder for agent tool use**: every MCP tool call an agent
makes crosses a policy boundary *before* it executes and lands in a
tamper-evident, hash-chained log. Any recorded session replays as a
hermetic MCP server in CI, and trajectory assertions run over it
deterministically. Record everything, enforce what you can prove,
replay the rest.

## 1–2 min — Install

```bash
# prebuilt binaries: https://github.com/Caseymccallum/tapelog/releases
go install github.com/Caseymccallum/tapelog/cmd/tapelog@latest
tapelog version    # tapelog 0.4.0
```

## 2–3 min — Put it in front of one MCP server

One config entry in your harness — tapelog spawns the real server and
mediates everything between (full examples: [CLIENT-SETUP.md](CLIENT-SETUP.md)):

```json
{
  "mcpServers": {
    "filesystem-logged": {
      "command": "tapelog",
      "args": ["record", "--log", "first.jsonl",
               "--", "npx", "-y", "@modelcontextprotocol/server-filesystem", "."]
    }
  }
}
```

No `--policy` yet = **no policy rules** (default-allow, everything
recorded) — the boundary's other protections stay active.

## 3–4 min — Record one real interaction

Ask your agent to read a file ("read README.md"). Then look at the
evidence:

```bash
tapelog inspect first.jsonl --plain
```

You'll see the `tools/call`, the `policy/decision`, and the
`tools/result` — redacted, ordered, hash-chained.

## 4–5 min — Replay it

```bash
tapelog replay first.jsonl
```

Point the agent at the replay the same way (change the command to
`tapelog replay first.jsonl`) and re-ask: the recorded answers are
served verbatim; anything unrecorded gets `-32011` fail-loud. You just
turned a session into a deterministic fixture.

## The second five minutes — the flight-recorder proof

Now add a policy and watch the boundary work
(`packs/filesystem.yaml` denies deletes):

```text
record with --policy  →  agent tries to delete a file  →  denied
inspect --plain      →  the DENIED block: rule, reason, provenance
verify               →  chain intact, chain head (your external anchor)
replay               →  the session re-serves exactly
policy whatif        →  which recorded verdicts would flip
```

```bash
tapelog record --policy packs/filesystem.yaml --log denied.jsonl -- <your server>
tapelog inspect denied.jsonl --plain     # the DENIED block
tapelog verify denied.jsonl              # chain head -> record it outside the log
tapelog replay denied.jsonl
tapelog policy whatif --policy packs/starter.yaml denied.jsonl
```

That is the product: **record → enforce → inspect → verify → replay →
what-if** — a flight recorder your CI can assert over. The full command
tour is in the [README](../README.md#command-tour); the 30-minute
walkthrough of everything else is
[docs/launch/TESTING-GUIDE.md](launch/TESTING-GUIDE.md).

## No agent yet?

The canonical demo needs nothing but the binary and a terminal — it
drives a scripted agent against a scripted MCP server and shows the
whole story (record → deny → assertions → what-if → replay → tamper):

```powershell
powershell -File examples/trajectory-demo/demo.ps1
```