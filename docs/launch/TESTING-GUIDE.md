# tapelog — hands-on testing guide

A 30-minute, copy-paste walkthrough of everything tapelog does. Every
command below was run exactly as written on Windows/PowerShell before
this guide was published — the "You should see" boxes are real output.
If a step doesn't match, that's a bug worth reporting (see the end).

**Setup:** open PowerShell in the repo root (`C:\Users\Casey\Web Apps\agent-boundary`).

```powershell
# Build the binary (skip if bin\tapelog.exe is up to date)
cmd /c 'set PATH=C:\Program Files\Go\bin;%PATH% && go build -o bin\tapelog.exe .\cmd\tapelog'
```

> Gotcha #1 (ours, not yours): if tests behave strangely after pulling
> new code, rebuild first — a stale `bin\tapelog.exe` is the #1 false
> alarm.
>
> Copy tip: copy only the command lines **inside** the boxes, not the
> word `powershell` above them (it's a fence label; pasting it makes
> PowerShell print a "term not recognized" error). And always run the
> binary as `.\bin\tapelog.exe` — bare `tapelog.exe` triggers a
> Windows "command not found, but exists in current location" note.

---

## Test 0 — Is the install healthy? (1 min)

```powershell
.\bin\tapelog.exe doctor
```

You should see mostly `✓`, maybe some `!` (warnings are fine):

```
✓ runtime — tapelog 0.3.0-dev, windows/amd64
! policy — none given — record/mux will run observe-only (add --policy to enforce)
! plugins — none configured (optional)
! os-sandbox — no OS enforcement on this platform (Linux required); policy still enforces
✓ terminal — interactive approval prompts available
✓ log-location — . is writable
✓ node — on PATH (for npx-based MCP servers)
all required checks passed
```

✅ **Pass:** exit code 0 and `all required checks passed`.

---

## Test 1 — The flight recorder (5 min)

Record a fake MCP session (a toy server with `read_file` / `delete_file`
tools; the input file drives a few calls through it):

```powershell
cmd /c 'bin\tapelog.exe record --log my-session.jsonl --auto-confirm -- powershell -NoProfile -File test-fixtures\fake-mcp-server.ps1 < test-fixtures\client-input.jsonl'
```

Then look at what was recorded:

```powershell
.\bin\tapelog.exe inspect my-session.jsonl --plain
```

You should see a timeline like this (note `api_key` is `[REDACTED]` —
secret redaction is on by default):

```
    1  session/start   harness=unknown policy=observe
    2  tools/list      2 tools
>   3  tools/call      read_file {"api_key":"[REDACTED]","path":"/tmp/a.txt"}
+   4  policy/decision allow (observe) — no policy configured; observe-only recording
>   5  tools/call      delete_file {"path":"/tmp/a.txt"}
...
```

✅ **Pass:** every request line has a matching result/decision line, and
no real secret strings appear in the log.

---

## Test 2 — Tamper evidence (5 min)

The log is hash-chained: each event's hash covers the previous one.
Editing a byte must be detectable. First confirm the fresh log is
healthy:

```powershell
.\bin\tapelog.exe verify my-session.jsonl
```

```
OK: my-session.jsonl
  9 events, chain intact
  chain head: <64 hex chars>
```

Now tamper with one event — **use this exact command** (it writes
BOM-free UTF-8; `Set-Content -Encoding utf8` silently adds a BOM and
confuses the demo):

```powershell
$t=[IO.File]::ReadAllText("$PWD\my-session.jsonl"); $t=$t.Replace('"verdict":"allow","rule_id":"observe"','"verdict":"denyyy","rule_id":"observe"'); [IO.File]::WriteAllText("$PWD\tampered.jsonl",$t,[Text.UTF8Encoding]::new($false))
.\bin\tapelog.exe verify tampered.jsonl
```

You should see the tamper caught, pinpointed to the exact event:

```
FAILED: tampered.jsonl
  first bad event: seq 4
  problem: hash mismatch: event 4 was modified after writing
  verified 3 events before the problem
error: session log failed verification
```

✅ **Pass:** exit code 1 and `first bad event` matches the event you
edited (seq 4).

---

## Test 3 — The policy boundary (5 min)

Dry-run a policy against sample calls before deploying it. Write two
sample calls and evaluate them against the shipped filesystem pack:

```powershell
$lines = @('{"tool":"read_file","args":{"path":"notes.txt"}}','{"tool":"write_file","args":{"path":"out.txt","content":"hi"}}')
[IO.File]::WriteAllLines("$PWD\policy-samples.jsonl",$lines,[Text.UTF8Encoding]::new($false))
.\bin\tapelog.exe policy test --policy packs\filesystem.yaml --calls policy-samples.jsonl
```

You should see a verdict table — the read is allowed, the write needs a
human:

```
VERDICT  TOOL                           RULE           REASON
allow    read_file                      default        no rule matched; policy default is "allow"
confirm  write_file                     writes-need-confirm filesystem mutations need a human in the loop

2 calls evaluated against packs\filesystem.yaml
```

✅ **Pass:** `write_file` shows `confirm`. Try adding
`{"tool":"delete_file","args":{"path":"x"}}` to `policy-samples.jsonl` —
the pack should deny it.

---

## Test 4 — Value-level taint: the precision demo (5 min)

This is the headline capability. A toy server returns a fake secret
(`sk-CANARY-7f3a9b`); the policy blocks secret **values** reaching a
sink — but must not block clean calls:

```powershell
cmd /c 'bin\tapelog.exe record --policy test-fixtures\valueflow-policy.yaml --log taint-demo.jsonl --auto-confirm -- powershell -NoProfile -File test-fixtures\fake-mcp-taint.ps1 < test-fixtures\taint-input.jsonl'
```

Watch the three responses the harness (your "agent") receives:

- **call 3** `read_secrets` → returns the canary ✅ allowed
- **call 4** `send_http` with the canary in its arguments → **denied**:
  `tool call denied by policy ... "secret values must not reach a sink [contaminated by: read_secrets]"`
- **call 5** `send_http` with clean arguments → succeeds ✅ **allowed**
  (a plain session-taint policy would have wrongly blocked this one too)

✅ **Pass:** call 4's response is a `-32010` deny error naming
`contaminated by: read_secrets`, and call 5 succeeds.

---

## Test 5 — Agent regression tests (3 min)

Scenarios assert on a recorded trajectory (deterministic, no LLM judge):

```powershell
.\bin\tapelog.exe test --junit junit.xml --annotate test-fixtures\scenarios\pass.yaml
.\bin\tapelog.exe test test-fixtures\scenarios\fail.yaml
```

You should see `✓ release-flow (4 assertions)` for the first (and a
`junit.xml` file created), then `✗ no-deletes ... never_called
delete_file` and exit code 1 for the second.

✅ **Pass:** first command exits 0, second exits 1. This is exactly how
it behaves in CI (GitHub annotations appear automatically there).

---

## Test 6 — Fuzz the boundary (5 min)

Mutate recorded calls (name spoofing, homoglyphs, encoding tricks,
reordering) and report mutations that escape a `deny`:

```powershell
.\bin\tapelog.exe fuzz --policy packs\filesystem.yaml test-fixtures\queue-session.jsonl
.\bin\tapelog.exe fuzz --policy test-fixtures\valueflow-policy.yaml --operator arg_encoding --operator swap_rotate test-fixtures\queue-session.jsonl
```

You should see either findings (each names the operator, mutation, and
rule that broke) or the all-clear line:

```
no boundary bypasses found (1 calls mutated across 2 operators)
```

✅ **Pass:** exit code 0 on the all-clear. **Findings exit 1** (CI
semantics: a bypass is a build break) — each one is a real policy hole
("this mutation escaped your deny rule"), not a test bug; fix the rule
(usually a wildcard) and re-run until clean.

---

## Test 7 — The review dashboard (3 min)

```powershell
.\bin\tapelog.exe web --dir . --listen 127.0.0.1:8930
```

Open `http://127.0.0.1:8930` — you should see your session files in the
dropdown with event / denial counts; click one for the event timeline
with policy decisions highlighted. (Read-only; loopback + strict CSP.)
Stop the server with Ctrl+C.

**Tamper badges:** `tampered.jsonl` is flagged with a `⚠` in the
dropdown, and selecting it shows a red banner:
`⚠ chain broken — first bad event: seq 4 (hash mismatch...)` — the
dashboard verifies every log's hash chain so modified evidence can't
pass as authentic. `my-session.jsonl` and `taint-demo.jsonl` show no
warning. The verdict is re-checked on every poll (≈2s), so an edit made
*while the dashboard is open* raises the banner live — and the same
banner appears in the `record --approval-listen` dashboard, not just
`tapelog web`.

✅ **Pass:** sessions list renders, `tampered.jsonl` carries the ⚠ +
banner, and the timeline shows the deny from Test 4 with its reason.

---

## Optional — deeper dives

| Test | Command | What to look for |
|---|---|---|
| Replay determinism | `.\bin\tapelog.exe replay my-session.jsonl` (then point an MCP client at it) | recorded answers served verbatim; unknown calls answered with error `-32011` (fail-loud) |
| Signed checkpoint | `.\bin\tapelog.exe checkpoint my-session.jsonl --signer ssh --key <key> --witness none` then `verify my-session.jsonl --checkpoint my-session.jsonl.checkpoint.json` | WHO attested the chain head (signature); `--witness rekor` (default) additionally proves WHEN via a transparency log — see docs/CHECKPOINTS.md |
| Large payloads | `record --blob-threshold 65536 --log big.jsonl …` then `verify big.jsonl` | oversized `args`/`result` live in `big.jsonl.blobs/` with digests in the chain; verify reports `N blob reference(s), all digests verified` |
| Diff two sessions | `.\bin\tapelog.exe diff my-session.jsonl my-session.jsonl` | `identical` / exit 0 |
| What-if a policy | `.\bin\tapelog.exe policy whatif --policy packs\filesystem.yaml my-session.jsonl` | which recorded calls *would* change verdict under the new policy |
| Export | `.\bin\tapelog.exe export --help` | other formats (JSON/OTel) |
| Multi-server mux | see `test-fixtures\mux.yaml` + `test-fixtures\mux-input.jsonl` | namespaced tools from 2 servers through one boundary |

---

## If something breaks

Capture three things (that's all we need):

1. **The exact command** you ran (copy from your shell history)
2. **Expected vs actual** (the "You should see" box vs what you saw)
3. **Any produced files** (`*.jsonl`, `junit.xml`) — these are
   gitignored; attaching them to an issue is safe and immensely useful

Security-sensitive behavior (a policy bypass, XSS in the dashboard,
tampering that `verify` misses) → follow `SECURITY.md`, not a public
issue.

## Checklist

- [ ] Test 0 doctor — all required checks passed
- [ ] Test 1 record + inspect — full timeline, secrets redacted
- [ ] Test 2 tamper — caught at the exact event
- [ ] Test 3 policy test — confirm/deny verdicts match the pack
- [ ] Test 4 value taint — contaminated call denied, clean call allowed
- [ ] Test 5 test — pass scenario green, fail scenario red
- [ ] Test 6 fuzz — no bypasses (or findings triaged)
- [ ] Test 7 web — sessions + timeline render



