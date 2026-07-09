[CmdletBinding()]
param([string]$WorkerRoot = "C:\KeyDeck-Proof-Worker", [switch]$PlanOnly = $true)
$ErrorActionPreference = "Stop"
$plan = [ordered]@{
    timestamp_utc = (Get-Date).ToUniversalTime().ToString("o")
    plan_only = $true
    worker_root = $WorkerRoot
    actions_not_performed = @("install runner", "register runner", "create Windows service", "accept GitHub jobs", "store credentials")
    recommended_directories = @($WorkerRoot, (Join-Path $WorkerRoot "work"), (Join-Path $WorkerRoot "receipts"), (Join-Path $WorkerRoot "artifacts"))
}
New-Item -ItemType Directory -Force -Path "receipts" | Out-Null
$plan | ConvertTo-Json -Depth 5 | Set-Content -LiteralPath "receipts/windows-proof-worker-plan.json" -Encoding UTF8
$plan | ConvertTo-Json -Depth 5
