# The canonical trajectory demo

One artifact that shows the entire product:

```
agent -> tapelog boundary -> MCP server (secret producer + external sink)

record -> trajectory -> policy block -> verify -> assertions -> what-if
       -> replay -> tamper -> verification failure -> anchored verify
```

Run it from the repo root (PowerShell):

```powershell
go build -o bin\tapelog.exe .\cmd\tapelog
go build -o bin\fakecmd.exe .\internal\compat\fakecmd
.\examples\trajectory-demo\demo.ps1
```

## What happens, and why it matters

| Step | Shows | The claim it proves |
|---|---|---|
| 1. record | an agent calls 6 tools through the boundary | every call is recorded and mediated |
| 2. inspect | the full trajectory | observability |
| 3. policy | two denies with provenance | `send_http` blocked because a **value** from `read_secrets` reached a sink (`[contaminated by: read_secrets]`); `delete_file` blocked by rule |
| 4. verify | chain intact + printed chain head | tamper-evidence (keep the head out-of-band!) |
| 5. test | 9 trajectory assertions green | the trajectory is a regression test |
| 6. what-if | 6 same, 0 changed | policy verdicts are reproducible from the log |
| 7. replay | the tape re-serves results hermetically | deterministic replay |
| 8-9. tamper | verdict flipped in the log | **`tapelog test` still GREEN — the hash chain RED** |
| 10. anchored verify | tail truncated | internally-consistent chain, caught by `--expect <head>` |

The money line (step 9): **behavioral tests pass on a tampered log; the
hash chain doesn't.** Assertions check behavior. The chain checks
integrity. You need both.

## Files

- `policy.yaml` — one hard rule (`no-delete`) + one value-taint flow
  (`no-secret-exfil`: values from `read_secrets` must not reach `send_*`)
- `agent.jsonl` — the scripted agent conversation (8 JSON-RPC requests)
- `scenario.yaml` — trajectory assertions (`called`, `sequence`, `attempted`,
  `denied`, `result_contains`, `invariant: no_deny_bypassed`)
- `demo.ps1` — runs the whole story; writes `demo.jsonl`, `tampered.jsonl`,
  `truncated.jsonl` at the repo root (gitignored)

The fake MCP server is `internal/compat/fakecmd` (the compatibility lab's
hermetic server, one binary): tools `read_file`, `write_file`,
`read_secrets` (canary producer), `send_http` (external sink),
`delete_file`, `boom`, `needs_input` (MRTR).

## For the demo GIF

Steps 1, 3, 4, 8, 9 are the camera beats: the deny with contamination
evidence, the chain head, the tamper one-liner, and the green-vs-red
verdict pair.