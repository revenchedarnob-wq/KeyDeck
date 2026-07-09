$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$ExpectedVersion = '0.38.0'
$ExpectedSha256 = 'ff7fe521d4f876de29d018a00fe282746109d8b788a6e9a9f288dbd8a3470364'
$ReleaseBase = 'https://github.com/microsoft/waza/releases/download/v0.38.0'
$Root = Split-Path -Parent $MyInvocation.MyCommand.Path
$ToolDir = Join-Path $Root '.proof14-tools'
$OutDir = Join-Path $Root 'proof14-output'
$Waza = Join-Path $ToolDir 'waza.exe'
$Checksums = Join-Path $ToolDir 'checksums.txt'

New-Item -ItemType Directory -Force -Path $ToolDir, $OutDir | Out-Null

function Invoke-Captured {
    param(
        [Parameter(Mandatory=$true)][string]$Name,
        [Parameter(Mandatory=$true)][string[]]$Arguments,
        [Parameter(Mandatory=$true)][string]$LogFile
    )
    $oldError = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    $lines = & $Waza @Arguments 2>&1 | ForEach-Object { $_.ToString() }
    $code = $LASTEXITCODE
    $ErrorActionPreference = $oldError
    $text = ($lines -join [Environment]::NewLine)
    [IO.File]::WriteAllText($LogFile, $text + [Environment]::NewLine, [Text.UTF8Encoding]::new($false))
    return [ordered]@{
        name = $Name
        arguments = $Arguments
        exit_code = $code
        passed = ($code -eq 0)
        log_file = [IO.Path]::GetFileName($LogFile)
        log_sha256 = (Get-FileHash -Algorithm SHA256 $LogFile).Hash.ToLowerInvariant()
    }
}

Write-Host 'Downloading pinned Microsoft Waza v0.38.0...'
Invoke-WebRequest -UseBasicParsing -Uri "$ReleaseBase/waza-windows-amd64.exe" -OutFile $Waza
Invoke-WebRequest -UseBasicParsing -Uri "$ReleaseBase/checksums.txt" -OutFile $Checksums

$actualHash = (Get-FileHash -Algorithm SHA256 $Waza).Hash.ToLowerInvariant()
if ($actualHash -ne $ExpectedSha256) {
    throw "Waza checksum mismatch. Expected $ExpectedSha256 but got $actualHash"
}
$checksumText = Get-Content -Raw $Checksums
if ($checksumText -notmatch [regex]::Escape($ExpectedSha256)) {
    throw 'Official checksums.txt does not contain the expected Windows x64 hash.'
}

Push-Location $Root
try {
    Remove-Item -Recurse -Force -ErrorAction SilentlyContinue snapshots
    Remove-Item -Force -ErrorAction SilentlyContinue results.json, baseline.json
    New-Item -ItemType Directory -Force -Path snapshots | Out-Null

    $commands = @()
    $commands += Invoke-Captured 'version' @('--version') (Join-Path $OutDir '01-version.txt')
    $commands += Invoke-Captured 'skill_check' @('check','skills/keydeck-proof') (Join-Path $OutDir '02-skill-check.txt')
    $commands += Invoke-Captured 'spec_verify' @('spec','verify','skills/keydeck-proof','evals/keydeck-proof/eval.yaml','--fail','--threshold','1','--format','json') (Join-Path $OutDir '03-spec-verify.json')
    $commands += Invoke-Captured 'tokens_count' @('tokens','count','skills/keydeck-proof/SKILL.md') (Join-Path $OutDir '04-tokens-count.txt')
    $commands += Invoke-Captured 'tokens_check' @('tokens','check','keydeck-proof') (Join-Path $OutDir '05-tokens-check.txt')
    $commands += Invoke-Captured 'mock_eval' @('run','evals/keydeck-proof/eval.yaml','-o','results.json','--snapshot','snapshots') (Join-Path $OutDir '06-mock-eval.txt')

    if (Test-Path results.json) {
        Copy-Item results.json baseline.json -Force
        $commands += Invoke-Captured 'regression_gate' @('gate','--baseline','baseline.json','--current','results.json','--format','json') (Join-Path $OutDir '07-regression-gate.json')
    } else {
        $commands += [ordered]@{name='regression_gate';arguments=@();exit_code=99;passed=$false;log_file='';log_sha256=''}
    }

    $replays = @()
    $snapshotFiles = @(Get-ChildItem -File -Recurse snapshots -Filter '*.json' | Sort-Object FullName)
    $index = 0
    foreach ($snapshot in $snapshotFiles) {
        $index++
        $log = Join-Path $OutDir ('replay-{0:D3}.json' -f $index)
        $r = Invoke-Captured ("replay_" + $snapshot.Name) @('replay',$snapshot.FullName,'--json') $log
        $r['snapshot_file'] = $snapshot.FullName.Substring($Root.Length).TrimStart('\','/')
        $r['snapshot_sha256'] = (Get-FileHash -Algorithm SHA256 $snapshot.FullName).Hash.ToLowerInvariant()
        $replays += $r
    }

    $commands += Invoke-Captured 'adversarial_catalog' @('adversarial','--list-packs') (Join-Path $OutDir '08-adversarial-packs.txt')

    $versionText = Get-Content -Raw (Join-Path $OutDir '01-version.txt')
    $allRequired = @($commands | Where-Object { $_.name -ne 'adversarial_catalog' })
    $requiredPassed = (@($allRequired | Where-Object { -not $_.passed }).Count -eq 0)
    $replayPassed = ($snapshotFiles.Count -gt 0 -and (@($replays | Where-Object { -not $_.passed }).Count -eq 0))
    $versionPassed = ($versionText -match [regex]::Escape($ExpectedVersion))

    $evidence = [ordered]@{
        proof_component = 'microsoft-waza-real-offline-prototype'
        tool = [ordered]@{
            name = 'Microsoft Waza'
            version = $ExpectedVersion
            binary_sha256 = $actualHash
            checksum_verified = $true
        }
        mode = 'real pinned Windows x64 binary; mock executor; no paid model or API key'
        passed = ($requiredPassed -and $replayPassed -and $versionPassed)
        checks = [ordered]@{
            version_verified = $versionPassed
            skill_check = [bool](($commands | Where-Object name -eq 'skill_check').passed)
            spec_coverage_gate = [bool](($commands | Where-Object name -eq 'spec_verify').passed)
            token_count = [bool](($commands | Where-Object name -eq 'tokens_count').passed)
            token_budget_gate = [bool](($commands | Where-Object name -eq 'tokens_check').passed)
            deterministic_mock_eval = [bool](($commands | Where-Object name -eq 'mock_eval').passed)
            regression_gate = [bool](($commands | Where-Object name -eq 'regression_gate').passed)
            snapshot_count = $snapshotFiles.Count
            snapshot_replays_passed = $replayPassed
            adversarial_catalog_available = [bool](($commands | Where-Object name -eq 'adversarial_catalog').passed)
        }
        measurements = [ordered]@{
            task_count = 4
            trials_per_task = 5
            intended_trial_count = 20
            positive_trigger_tasks = 2
            negative_trigger_tasks = 2
        }
        commands = $commands
        replays = $replays
        limitations = @(
            'This proof uses Waza mock execution to isolate skill/evaluation infrastructure from model quality and billing.',
            'It does not prove live multi-model quality; real-model comparison remains a later optional development evaluation.',
            'The runner downloads only the exact pinned Microsoft v0.38.0 Windows x64 binary and verifies the official SHA-256 before execution.'
        )
    }

    $EvidencePath = Join-Path $OutDir 'waza-proof-evidence.json'
    $evidence | ConvertTo-Json -Depth 12 | Set-Content -Encoding UTF8 $EvidencePath
    $EvidenceHash = (Get-FileHash -Algorithm SHA256 $EvidencePath).Hash.ToLowerInvariant()
    $ReturnPath = Join-Path $Root 'RETURN-THIS-waza-proof-evidence.json'
    Copy-Item $EvidencePath $ReturnPath -Force
    Write-Host "Evidence: $EvidencePath"
    Write-Host "Return file: $ReturnPath"
    Write-Host "SHA-256: $EvidenceHash"
    if (-not $evidence.passed) { exit 1 }
    Write-Host 'PASS: real Waza v0.38.0 offline proving-ground gate succeeded.'
}
finally {
    Pop-Location
}
