# M5 evidence: prove the Policy Studio never writes.
#
# Starts nothing. Point it at a running workbench:
#   bin\tapelog.exe web --dir m5-demo-sessions --policy packs/starter.yaml
#   powershell -File docs\reviews\evidence\m5\verify-no-write.ps1
#
# It exercises every Studio endpoint and prints a before/after hash for the
# policy file and every recording.
param(
  [string]$Base = 'http://127.0.0.1:8934',
  [string]$Policy = 'packs\starter.yaml',
  [string]$Recordings = 'm5-demo-sessions'
)

function Fingerprint([string]$path) {
  (Get-FileHash -LiteralPath $path -Algorithm SHA256).Hash + ' ' + (Get-Item -LiteralPath $path).LastWriteTimeUtc.Ticks
}

function Snapshot {
  $p = Fingerprint $Policy
  $r = Get-ChildItem -LiteralPath $Recordings -Filter *.jsonl | Sort-Object Name | ForEach-Object {
    $_.Name + ' ' + (Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash
  }
  return @{ policy = $p; recordings = ($r -join "`n") }
}

$before = Snapshot
Write-Output 'BEFORE'
Write-Output ('  policy: ' + $before.policy)
Write-Output ($before.recordings -split "`n" | ForEach-Object { '  ' + $_ })

$headers = @{ 'Content-Type' = 'application/json' }

$policies = Invoke-RestMethod "$Base/api/policy/policies"
Write-Output ''
Write-Output ('GET /api/policy/policies -> active=' + ($policies.active.name) + ' packs=' + $policies.packs.Count)

$inspect = Invoke-RestMethod "$Base/api/policy/inspect?source=active"
Write-Output ('GET /api/policy/inspect -> valid=' + $inspect.valid + ' rules=' + $inspect.analysis.rules.Count + ' unsupported=' + $inspect.analysis.unsupported.Count)

$activity = Invoke-RestMethod "$Base/api/activity?file=01-workspace-audit.jsonl"
$revision = $activity.session.revision
$seq = ($activity.activities | Where-Object { $_.tool -eq 'packages__lookup' } | Select-Object -First 1).seq
Write-Output ('GET /api/activity -> revision=' + $revision.Substring(0,12) + '... target seq=' + $seq)

$fromEvent = Invoke-RestMethod -Method Post -Uri "$Base/api/policy/from-event" -Headers $headers -Body (@{ file='01-workspace-audit.jsonl'; revision=$revision; seq=$seq } | ConvertTo-Json)
Write-Output ('POST /api/policy/from-event -> tool=' + $fromEvent.tool + ' suggested=' + $fromEvent.suggested_action + ' arg_keys=' + $fromEvent.arg_keys.Count)

$body = @{
  base = 'pack:starter'
  default = 'deny'
  file = '01-workspace-audit.jsonl'
  revision = $revision
  rule = @{ tools = @('packages__lookup'); action = 'confirm'; reason = 'fictional walkthrough' }
} | ConvertTo-Json -Depth 6
$preview = Invoke-RestMethod -Method Post -Uri "$Base/api/policy/preview" -Headers $headers -Body $body
Write-Output ('POST /api/policy/preview -> valid=' + $preview.valid + ' extended=' + $preview.extended + ' changed=' + $preview.simulation.changed + '/' + $preview.simulation.total + ' candidate_bytes=' + $preview.candidate.Length)

$sim = Invoke-RestMethod -Method Post -Uri "$Base/api/policy/simulate" -Headers $headers -Body (@{ source='pack:starter'; file='01-workspace-audit.jsonl'; revision=$revision } | ConvertTo-Json)
Write-Output ('POST /api/policy/simulate -> changed=' + $sim.simulation.changed + '/' + $sim.simulation.total)

$after = Snapshot
Write-Output ''
Write-Output 'AFTER'
Write-Output ('  policy: ' + $after.policy)
Write-Output ($after.recordings -split "`n" | ForEach-Object { '  ' + $_ })

Write-Output ''
if ($before.policy -eq $after.policy -and $before.recordings -eq $after.recordings) {
  Write-Output 'RESULT: unchanged. The Studio read the policy and the recordings and wrote nothing.'
} else {
  Write-Output 'RESULT: CHANGED. Investigate before accepting this milestone.'
  exit 1
}
