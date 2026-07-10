param(
  [string]$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path,
  [string]$OutJson = (Join-Path $RepoRoot "docs\PROOF_REGISTRY.json"),
  [string]$OutMarkdown = (Join-Path $RepoRoot "docs\PROOF_REGISTRY.md"),
  [string]$SourceCommit = "",
  [switch]$RequireClean
)
$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest
Set-Location $RepoRoot
if ($RequireClean) {
  $dirty = @(git -C $RepoRoot status --porcelain --untracked-files=no)
  if ($LASTEXITCODE -ne 0) { throw "Unable to inspect Git state." }
  if ($dirty.Count) { throw ("Proof registry requires clean tracked files:`n" + ($dirty -join "`n")) }
}
if (-not $SourceCommit) {
  $SourceCommit = (git -C $RepoRoot rev-parse HEAD).Trim()
  if ($LASTEXITCODE -ne 0) { throw "Unable to resolve source commit." }
}
$validationPath = Join-Path $RepoRoot "docs\KeyDeck-v0.35.0-RECONSTRUCTED-VALIDATION.json"
if (-not (Test-Path $validationPath)) { throw "Missing validation file: $validationPath" }
$validation = Get-Content -LiteralPath $validationPath -Raw | ConvertFrom-Json
$items = New-Object System.Collections.Generic.List[object]
foreach ($n in 1..8) {
  $suffix = if ($n -in @(6,7,8)) { "-policy" } else { "" }
  $items.Add([pscustomobject]@{
    proof = "0.$n"; status = "PASS"; scenario_count = $null; report_sha256 = $null
    source_report_path = "docs/KeyDeck-Proof-0.$n$suffix-report.json"
    evidence_class = "historical_verified_report_present"
    note = "Scenario count not rederived by automation bootstrap."
  }) | Out-Null
}
foreach ($prop in $validation.proofs.PSObject.Properties | Sort-Object { [int]$_.Name }) {
  $items.Add([pscustomobject]@{
    proof = "0.$($prop.Name)"
    status = if ($prop.Value.passed) { "PASS" } else { "FAIL" }
    scenario_count = $prop.Value.scenario_count
    report_sha256 = $prop.Value.report_sha256
    source_report_path = "docs/KeyDeck-Proof-0.$($prop.Name)-report.json"
    evidence_class = "validated_v0.35_reconstructed_report"
  }) | Out-Null
}
$missing = @($items | Where-Object { -not (Test-Path -LiteralPath (Join-Path $RepoRoot $_.source_report_path)) })
foreach ($item in $missing) {
  $item.evidence_class = "historical_reference_missing_source_report"
  $item | Add-Member `
    -MemberType NoteProperty `
    -Name note `
    -Value "Referenced report file not present in current repository evidence boundary." `
    -Force
}
$data = [ordered]@{
  schema_version = 1
  generated_at_utc = [datetime]::UtcNow.ToString("o")
  repository = "revenchedarnob-wq/KeyDeck"
  source_commit = $SourceCommit
  baseline = $validation.release
  latest_verified_proof = "0.38"
  next_gate = "0.39 Secret-Safe Production Visual Bootstrap and First-Run Desktop Launch"
  evidence_boundary = $validation.evidence_boundary
  proofs = $items
}
foreach ($p in @($OutJson,$OutMarkdown)) {
  $parent = Split-Path -Parent $p
  if ($parent) { New-Item -ItemType Directory -Force -Path $parent | Out-Null }
}
$data | ConvertTo-Json -Depth 8 | Set-Content -LiteralPath $OutJson -Encoding utf8
$lines = @(
  "# Proof Registry","",
  "- Generated UTC: ``$($data.generated_at_utc)``",
  "- Source commit: ``$($data.source_commit)``",
  "- Baseline: ``$($data.baseline)``",
  "- Latest verified proof: ``0.38``",
  "- Next gate: ``$($data.next_gate)``","",
  "| Proof | Status | Scenarios | Report SHA-256 |",
  "|---:|---|---:|---|"
)
$lines += $items | ForEach-Object { "| $($_.proof) | $($_.status) | $($_.scenario_count) | ``$($_.report_sha256)`` |" }
$lines -join "`n" | Set-Content -LiteralPath $OutMarkdown -Encoding utf8
Write-Host "Wrote $OutJson and $OutMarkdown"