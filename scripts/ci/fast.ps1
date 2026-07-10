param(
  [string]$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path
)
$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest
Set-Location $RepoRoot

function Invoke-CheckedNative {
  param(
    [Parameter(Mandatory=$true)][string]$Label,
    [Parameter(Mandatory=$true)][scriptblock]$Command
  )
  & $Command
  if ($LASTEXITCODE -ne 0) {
    throw "$Label failed with exit code $LASTEXITCODE"
  }
}

Write-Host "KEYDECK fast CI: repo=$RepoRoot"
Invoke-CheckedNative "go version" { go version }
Write-Host "Running go test ./..."
Invoke-CheckedNative "go test ./..." { go test ./... }
Write-Host "Running go vet ./..."
Invoke-CheckedNative "go vet ./..." { go vet ./... }
Write-Host "KEYDECK fast CI: PASS"