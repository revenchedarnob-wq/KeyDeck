param(
  [string]$Version = "v0.35.0-RECONSTRUCTED",
  [string]$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path,
  [string]$OutDir = (Join-Path $RepoRoot "_out\release")
)
$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest
Set-Location $RepoRoot
New-Item -ItemType Directory -Force -Path $OutDir | Out-Null
& (Join-Path $RepoRoot "scripts\ci\secret-scan.ps1") -RepoRoot $RepoRoot

$sourceCommit = (git -C $RepoRoot rev-parse HEAD).Trim()
if ($LASTEXITCODE -ne 0) { throw "Unable to resolve release source commit." }

$zip = Join-Path $OutDir ("KeyDeck-source-$Version.zip")
$manifest = Join-Path $OutDir "RELEASE-MANIFEST.csv"
$proofJson = Join-Path $OutDir "PROOF_REGISTRY.json"
$proofMarkdown = Join-Path $OutDir "PROOF_REGISTRY.md"

foreach ($path in @($zip,$manifest,$proofJson,$proofMarkdown)) {
  if (Test-Path $path) { Remove-Item -LiteralPath $path -Force }
}

& (Join-Path $RepoRoot "scripts\proof\build-proof-registry.ps1") `
  -RepoRoot $RepoRoot `
  -OutJson $proofJson `
  -OutMarkdown $proofMarkdown `
  -SourceCommit $sourceCommit `
  -RequireClean

git archive --format zip --output $zip HEAD
if ($LASTEXITCODE -ne 0) { throw "git archive failed." }
& (Join-Path $RepoRoot "scripts\artifacts\new-manifest.ps1") -InputPath $OutDir -OutputCsv $manifest

foreach ($required in @($zip,$manifest,$proofJson,$proofMarkdown)) {
  if (-not (Test-Path $required)) { throw "Release output missing: $required" }
}
Write-Host "Release artifacts staged under $OutDir"