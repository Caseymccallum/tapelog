# The Rogue Agent demo

A 60-second tour of cassette, in the form of an attack story:

1. **Record** — an agent talks to a "helpful" MCP server. Everything is
   logged to a tamper-evident session log; a policy is enforced.
2. **The attack** — mid-session the server *poisons its own tool descriptor*
   (a rug pull: the tool now claims to phone home). `--deny-on-drift` stops
   the next call with a hard deny: `rule_id: descriptor-drift`.
3. **Forensics** — `inspect` shows the timeline with the drift flagged;
   `verify` proves the log is intact.
4. **Replay** — the whole session re-runs hermetically against the recording
   (`cassette replay`), fail-loud on anything unrecorded.
5. **What-if** — re-evaluate history against a stricter policy: exactly which
   verdicts would change?
6. **Observability** — export the session as an OpenTelemetry trace
   (`execute_tool` spans, GenAI semantic conventions).

## Run it

```powershell
powershell -File examples/rogue-agent/demo.ps1
```

Everything runs offline against the test doubles in `test-fixtures/`
(`fake-mcp-server-drift.ps1` performs the poisoning on the second
`tools/list`). No API keys, no network.

## Recording a GIF for launch

Run the demo in a clean terminal at ~120x35 with a large font (e.g.
Cascadia Code 16pt), record the terminal window, and keep it under 60s.
Suggested beats to linger on: the `-32010` deny with `descriptor-drift`,
the `[DRIFT]` line in `inspect`, and the replay summary (`1 played,
1 missed`).
