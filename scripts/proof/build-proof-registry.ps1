[CmdletBinding()]
param([string]$JsonPath = "docs/PROOF_REGISTRY.json", [string]$MarkdownPath = "docs/PROOF_REGISTRY.md")
$ErrorActionPreference = "Stop"
$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path
Set-Location $repoRoot
$proofs = @()
$cmdProofDirs = Get-ChildItem -LiteralPath "cmd" -Directory | Where-Object { $_.Name -match '^proof\d+(real|server)?$' } | Sort-Object Name
foreach ($dir in $cmdProofDirs) {
    $numMatch = [regex]::Match($dir.Name, '^proof(\d+)')
    $num = if ($numMatch.Success) { [int]$numMatch.Groups[1].Value } else { $null }
    $spec = Get-ChildItem -LiteralPath "docs" -File | Where-Object { $_.Name -match ("^PROOF_0\.?" + $num + ".*\.md$") } | Select-Object -First 1
    $mainPath = Join-Path $dir.FullName "main.go"
    $content = if (Test-Path $mainPath) { Get-Content -Raw -LiteralPath $mainPath } else { "" }
    $docContent = if ($spec) { Get-Content -Raw -LiteralPath $spec.FullName } else { "" }
    $text = ($content + "`n" + $docContent)
    $real = $dir.Name -match 'real' -or $text -match '(?i)real Codex|real provider|Aerolink|ChatGPT|OAuth|device-login|Codex CLI'
    $windows = $text -match '(?i)windows|GOOS|proof executable is intended for Windows'
    $ui = $text -match '(?i)browser|Chromium|visual|desktop|UI|renderer'
    $network = $text -match '(?i)http|https|provider|download|server|loopback|network|MCP|Aerolink'
    $creds = $text -match '(?i)credential|secret|token|API key|signed in|OAuth|device-login|KEYDECK_PROOF2[345]_'
    $ciSafe = (-not $real) -and (-not $creds) -and ($dir.Name -notmatch 'server$')
    $proofs += [pscustomobject][ordered]@{
        proof_number = if ($null -ne $num) { "0.$num" } else { "unknown" }
        source_command = "go run ./cmd/$($dir.Name)"
        package_path = "./cmd/$($dir.Name)"
        deterministic_or_external = if ($real -or $creds -or $network) { if ($ciSafe) { "deterministic-local" } else { "external-or-environmental" } } else { "deterministic" }
        credentials_required = if ($creds) { $true } else { $false }
        windows_required = if ($windows) { $true } else { $false }
        ui_required = if ($ui) { $true } else { $false }
        network_required = if ($network -and -not $ciSafe) { $true } else { $false }
        ci_safe = $ciSafe
        expected_evidence = if ($text -match '(?i)report') { "report JSON or PASS output described by command/spec" } else { "unknown" }
        relevant_specification_document = if ($spec) { "docs/$($spec.Name)" } else { "unknown" }
    }
}
@{ generated_at_utc = (Get-Date).ToUniversalTime().ToString("o"); proofs = @($proofs) } | ConvertTo-Json -Depth 8 | Set-Content -LiteralPath $JsonPath -Encoding UTF8
$md = New-Object System.Collections.Generic.List[string]
$md.Add("# KeyDeck Proof Registry")
$md.Add("")
$md.Add("Generated from ``cmd/proof*`` commands and ``docs/PROOF_*`` specifications. Unknown values mean the repository did not provide enough deterministic evidence for this script to classify the field safely.")
$md.Add("")
$md.Add("| Proof | Command | Determinism | Credentials | Windows | UI | Network | CI-safe | Spec |")
$md.Add("|---|---|---|---:|---:|---:|---:|---:|---|")
foreach ($p in $proofs) { $md.Add("| $($p.proof_number) | ``$($p.source_command)`` | $($p.deterministic_or_external) | $($p.credentials_required) | $($p.windows_required) | $($p.ui_required) | $($p.network_required) | $($p.ci_safe) | $($p.relevant_specification_document) |") }
Set-Content -LiteralPath $MarkdownPath -Value $md -Encoding UTF8
Write-Host "Proof registry written: $JsonPath, $MarkdownPath"
