# Canonical trajectory demo — the whole product in one script.
#
#   record -> trajectory -> policy block -> replay/what-if -> tamper -> verify
#
# Money line: behavioral assertions pass on a tampered log; the hash chain
# doesn't. Run from the repo root.

$ErrorActionPreference = "Stop"
Set-Location (Split-Path $PSScriptRoot -Parent | Split-Path -Parent)

# 0. Build the tools the demo needs.
go build -o bin\tapelog.exe .\cmd\tapelog
go build -o bin\fakecmd.exe .\internal\compat\fakecmd

Write-Host "`n=== 1. RECORD: an agent runs against a secret-producing MCP server ===" -ForegroundColor Cyan
Get-Content examples\trajectory-demo\agent.jsonl |
  .\bin\tapelog.exe record --policy examples\trajectory-demo\policy.yaml `
    --log demo.jsonl --auto-confirm -- .\bin\fakecmd.exe -era 2026-07-28

Write-Host "`n=== 2. TRAJECTORY: what actually happened ===" -ForegroundColor Cyan
.\bin\tapelog.exe inspect demo.jsonl --plain

Write-Host "`n=== 3. POLICY: two calls crossed the boundary and were stopped ===" -ForegroundColor Cyan
Write-Host "  send_http  -> DENIED  (rule no-secret-exfil: value from read_secrets reached a sink)"
Write-Host "  delete_file -> DENIED  (rule no-delete)"
Write-Host "  the canary never left; the clean send_http went through"

Write-Host "`n=== 4. VERIFY: the chain is intact; keep this head out-of-band ===" -ForegroundColor Cyan
.\bin\tapelog.exe verify demo.jsonl
$head = (.\bin\tapelog.exe verify demo.jsonl | Select-String "chain head:").Line.Split(" ")[-1]
Write-Host "  saved chain head: $head"

Write-Host "`n=== 5. BEHAVIORAL ASSERTIONS: the trajectory is a regression test ===" -ForegroundColor Cyan
.\bin\tapelog.exe test examples\trajectory-demo\scenario.yaml

Write-Host "`n=== 6. WHAT-IF: the policy reproduces every recorded verdict ===" -ForegroundColor Cyan
.\bin\tapelog.exe policy whatif demo.jsonl --policy examples\trajectory-demo\policy.yaml

Write-Host "`n=== 7. REPLAY: the tape re-serves the recorded results hermetically ===" -ForegroundColor Cyan
'{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}
{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"read_secrets","arguments":{}}}' |
  .\bin\tapelog.exe replay demo.jsonl

Write-Host "`n=== 8. TAMPER: flip one verdict in the log ===" -ForegroundColor Yellow
$t = [IO.File]::ReadAllText("$PWD\demo.jsonl")
$t = $t.Replace('"verdict":"allow","rule_id":"default"', '"verdict":"denyyy","rule_id":"default"')
[IO.File]::WriteAllText("$PWD\tampered.jsonl", $t, [Text.UTF8Encoding]::new($false))
(Get-Content examples\trajectory-demo\scenario.yaml) | ForEach-Object {
  if ($_ -match '^fixture:') { 'fixture: ../../tampered.jsonl' } else { $_ }
} | Set-Content examples\trajectory-demo\scenario-tampered.yaml

Write-Host "`n=== 9. THE MONEY LINE ===" -ForegroundColor Green
Write-Host "  Behavioral assertions on the tampered log:" -ForegroundColor Green
.\bin\tapelog.exe test examples\trajectory-demo\scenario-tampered.yaml
Write-Host "  ...still GREEN. Assertions check behavior, not integrity." -ForegroundColor Green
Write-Host "  The hash chain:" -ForegroundColor Green
.\bin\tapelog.exe verify tampered.jsonl   # exits non-zero: first bad seq
Write-Host "  ...RED. tamelog test passes on a tampered log; the hash chain doesn't." -ForegroundColor Green

Write-Host "`n=== 10. ANCHORED VERIFY: even a consistent rewrite can't hide ===" -ForegroundColor Cyan
Write-Host "  (truncating the tail leaves an internally-consistent chain...)"
$lines = [IO.File]::ReadAllLines("$PWD\demo.jsonl")
[IO.File]::WriteAllLines("$PWD\truncated.jsonl", $lines[0..($lines.Length - 3)])
.\bin\tapelog.exe verify truncated.jsonl
Write-Host "  (...but the recorded head says otherwise:)"
.\bin\tapelog.exe verify truncated.jsonl --expect $head   # exits non-zero

Write-Host "`nDone. Files: demo.jsonl, tampered.jsonl, truncated.jsonl" -ForegroundColor Cyan