# compat-live.ps1 — real-world compat matrix runner (docs/ROADMAP.md v0.4).
#
# Runs the adaptive live tier (internal/compat/live_test.go) against the
# pinned third-party MCP servers. Scheduled + non-blocking: hermetic cells
# gate CI, this tier produces evidence. Every server version is PINNED so
# runs are reproducible; bump pins deliberately.
#
# Usage (from the repo root):
#   .\tools\compat-live.ps1                  # run every pinned server
#   .\tools\compat-live.ps1 -Server filesystem,fetch
#   .\tools\compat-live.ps1 -GitHubToken $env:GITHUB_TOKEN

param(
    [string[]]$Server = @("filesystem", "fetch", "github", "postgres"),
    [string]$GitHubToken = $env:GITHUB_TOKEN,
    [string]$PostgresURL = $env:TAPELOG_POSTGRES_URL
)

$ErrorActionPreference = "Continue"

# Scratch dir for the filesystem server: MUST be space-free — the pinned
# command is split on whitespace by the live harness (same cmd.exe+spaces
# trap root-caused in docs/CLIENT-SETUP.md).
$scratch = Join-Path $env:TEMP "tapelog-compat-scratch"

# Pinned real-world servers (bump deliberately; evidence cites these).
$pins = @{
    filesystem = "npx -y @modelcontextprotocol/server-filesystem@2026.8.31 $scratch"
    fetch      = "uvx mcp-server-fetch@2026.8.18"
    github     = "npx -y @modelcontextprotocol/server-github@2025.4.8"
    postgres   = "npx -y @modelcontextprotocol/server-postgres@0.6.2 $PostgresURL"
}

New-Item -ItemType Directory -Force -Path $scratch | Out-Null

$results = @()
foreach ($name in $Server) {
    $cmd = $pins[$name]
    if (-not $cmd) {
        Write-Host "?? unknown server '$name' (known: $($pins.Keys -join ', '))" -ForegroundColor Yellow
        continue
    }
    if ($name -eq "github" -and -not $GitHubToken) {
        Write-Host "- $name : skipped (no GITHUB_TOKEN)" -ForegroundColor Yellow
        $results += [pscustomobject]@{ Server = $name; Verdict = "SKIP" }
        continue
    }
    if ($name -eq "postgres" -and -not $PostgresURL) {
        Write-Host "- $name : skipped (no TAPELOG_POSTGRES_URL)" -ForegroundColor Yellow
        $results += [pscustomobject]@{ Server = $name; Verdict = "SKIP" }
        continue
    }

    Write-Host "`n=== $name ===" -ForegroundColor Cyan
    Write-Host "  TAPELOG_COMPAT_LIVE=$cmd"
    $env:TAPELOG_COMPAT_LIVE = $cmd
    if ($name -eq "github") { $env:GITHUB_PERSONAL_ACCESS_TOKEN = $GitHubToken }
    go test ./internal/compat/ -run TestLiveStdio -count=1 -v
    $verdict = if ($LASTEXITCODE -eq 0) { "PASS" } else { "FAIL" }
    $color = if ($verdict -eq "PASS") { "Green" } else { "Red" }
    Write-Host "-> $name : $verdict" -ForegroundColor $color
    $results += [pscustomobject]@{ Server = $name; Verdict = $verdict }
}

Write-Host "`n=== real-world matrix summary ===" -ForegroundColor Cyan
$results | Format-Table -AutoSize
if ($results.Verdict -contains "FAIL") { exit 1 } else { exit 0 }