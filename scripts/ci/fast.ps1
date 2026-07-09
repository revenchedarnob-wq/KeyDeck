param(
  [string]$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path
)
$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest
Set-Location $RepoRoot
Write-Host "KEYDECK fast CI: repo=$RepoRoot"
go version
Write-Host "Running go test ./..."
go test ./...
Write-Host "Running go vet ./..."
go vet ./...
Write-Host "KEYDECK fast CI: PASS"
