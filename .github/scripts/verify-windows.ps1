# Verifies the Windows archive the release workflow just built, by executing it.
# Runs on windows-latest (pwsh); also runnable locally against a built dist.
[CmdletBinding()]
param(
  [string]$DistDir = 'dist',
  [string]$Port = '8989'
)
$ErrorActionPreference = 'Stop'

$expected = (git rev-parse HEAD).Trim()
if (-not $expected) { throw 'cannot resolve the checked-out commit' }

$zip = Get-ChildItem "$DistDir/*windows_amd64.zip" | Select-Object -First 1
if (-not $zip) { throw "no windows archive in $DistDir" }

$line = Get-Content "$DistDir/checksums.txt" | Where-Object { $_ -match [regex]::Escape($zip.Name) } | Select-Object -First 1
if (-not $line) { throw "no checksum line for $($zip.Name)" }
# sha256sum format: "<hash> *<filename>" (or "<hash>  <filename>")
$want = $line.Trim().Split(' ')[0]
$got = (Get-FileHash $zip.FullName -Algorithm SHA256).Hash.ToLowerInvariant()
if ($got -ne $want.ToLowerInvariant()) { throw "checksum mismatch: $got != $want" }
Write-Host "checksum ok: $got"

$work = Join-Path ([IO.Path]::GetTempPath()) ("tapelog-verify-" + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $work | Out-Null
Push-Location $work
try {
  Expand-Archive -Path $zip.FullName -DestinationPath pkg -Force
  $dir = Get-ChildItem pkg -Directory | Select-Object -First 1
  if (-not $dir) { throw 'archive did not contain a top-level directory' }
  Push-Location $dir.FullName
  try {
    $version = (& .\tapelog.exe version) -join "`n"
    Write-Host "version: $version"
    if ($version -notmatch [regex]::Escape("($expected)")) { throw "version does not name the released commit $expected" }
    $manifest = Get-Content MANIFEST.txt -Raw
    if ($manifest -notmatch [regex]::Escape("commit: $expected")) { throw "MANIFEST.txt does not name $expected" }
    if (-not (Test-Path 'docs/GETTING_STARTED.md')) { throw 'docs/GETTING_STARTED.md missing' }
    if (-not (Test-Path 'docs/SECURITY-AND-LIMITATIONS.md')) { throw 'docs/SECURITY-AND-LIMITATIONS.md missing' }
    if (-not (Test-Path 'examples/regression-demo/demo-suite.json')) { throw 'demo suite missing' }

    & .\tapelog.exe demo --dir demo-run | Out-Host
    if ($LASTEXITCODE -ne 0) { throw "demo exited $LASTEXITCODE" }

    $rec = (Get-ChildItem demo-run/recordings/*.jsonl | Select-Object -First 1).FullName
    $verify = (& .\tapelog.exe verify $rec) -join "`n"
    if ($verify -notmatch 'chain intact') { throw 'verify did not report an intact chain' }

    # A native command that exits 1 under $ErrorActionPreference='Stop' raises in
    # Windows PowerShell when stderr is redirected, so drive it through cmd.
    $suiteOut = cmd /c ".\tapelog.exe lab suite --dir examples/regression-demo --suite demo-suite.json 2>&1"
    $suiteCode = $LASTEXITCODE
    $suiteOut | Set-Content lab.log
    if ($suiteCode -ne 1) { throw "bundled suite exited $suiteCode, expected 1 (a deliberate FAIL)" }
    if (-not ((Get-Content lab.log -Raw) -match '2 pass, 1 fail, 2 inconclusive')) { throw 'suite summary missing' }

    $proc = Start-Process -FilePath .\tapelog.exe -ArgumentList 'web','--dir','examples/regression-demo','--listen',"127.0.0.1:$Port" -PassThru -WindowStyle Hidden
    try {
      $ready = $false
      for ($i = 0; $i -lt 40; $i++) {
        try { Invoke-WebRequest -UseBasicParsing "http://127.0.0.1:$Port/api/sessions" -TimeoutSec 2 | Out-Null; $ready = $true; break } catch { Start-Sleep -Milliseconds 500 }
      }
      if (-not $ready) { throw "web server did not answer on 127.0.0.1:$Port" }
      $root = (Invoke-WebRequest -UseBasicParsing "http://127.0.0.1:$Port/").Content
      if ($root -notmatch 'Your agent sessions') { throw 'workspace HTML not served' }
      $js = (Invoke-WebRequest -UseBasicParsing "http://127.0.0.1:$Port/app.js").Content
      if ($js -notmatch 'suite-case-list') { throw 'embedded workspace assets missing' }
      $css = (Invoke-WebRequest -UseBasicParsing "http://127.0.0.1:$Port/app.css").Content
      if ($css -notmatch '#behaviour-stats') { throw 'embedded stylesheet missing' }
    } finally {
      if ($proc -and -not $proc.HasExited) { Stop-Process -Id $proc.Id -Force }
    }
    Write-Host 'windows archive verified'
  } finally { Pop-Location }
} finally { Pop-Location; Remove-Item -Recurse -Force $work -ErrorAction SilentlyContinue }
