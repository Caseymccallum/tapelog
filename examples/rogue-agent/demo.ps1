# Rogue Agent demo — runs the full cassette story end-to-end.
# Usage: powershell -File examples/rogue-agent/demo.ps1
$ErrorActionPreference = 'Continue'
Set-Location (Join-Path $PSScriptRoot '..\..')

$cassette = 'bin\cassette.exe'
if (-not (Test-Path $cassette)) {
    Write-Host 'building cassette...'
    cmd /c 'set PATH=C:\Program Files\Go\bin;%PATH% && go build -o bin\cassette.exe ./cmd\cassette'
}

function Step($text) { Write-Host "`n=== $text ===" -ForegroundColor Cyan }

Step '1. An agent talks to a helpful MCP server (recorded, policy enforced)'
# NOTE: piping via cmd's `type` is deliberate — PowerShell line-pipes to
# native exes have a known intermittent first-line drop (PS 5.1 quirk).
cmd /c 'type test-fixtures\drift-input.jsonl | bin\cassette.exe record --policy examples\policy.yaml --deny-on-drift --log demo.jsonl --harness rogue-demo -- powershell -NoProfile -File test-fixtures\fake-mcp-server-drift.ps1'

Step '2. The server poisoned its tool descriptor mid-session -> hard deny'
& $cassette inspect demo.jsonl --plain

Step '3. The log is tamper-evident'
& $cassette verify demo.jsonl

Step '4. Replay the session hermetically (no real tools involved)'
cmd /c 'type test-fixtures\drift-input.jsonl | bin\cassette.exe replay demo.jsonl --match tool'

Step '5. Policy what-if: would a stricter policy change history?'
& $cassette policy whatif --policy examples\policy-strict.yaml demo.jsonl

Step '6. Export the session as an OpenTelemetry trace'
& $cassette export otel demo.jsonl | Select-Object -First 20

Write-Host "`nDemo complete. Artifacts: demo.jsonl (session log)" -ForegroundColor Green
