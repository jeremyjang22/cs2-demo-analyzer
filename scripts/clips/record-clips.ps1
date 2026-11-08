<#
.SYNOPSIS
Records every clip clip-list chose, on the Windows machine that has CS2.

.DESCRIPTION
For each CS Demo Manager config that `clip-list -csdm <dir>` wrote, this makes
sure CS Demo Manager has analysed the demo, then runs `csdm video
--config-file`, which launches CS2 through HLAE, records each clip from the
chosen player's point of view and encodes it with FFmpeg. One demo at a time:
CS2 only runs once.

Resolution, framerate, codec and whether HLAE is used come from the video
settings in the CS Demo Manager app. Set them there once.

See docs/clip-capture.md for the one-time setup.

.EXAMPLE
.\record-clips.ps1 -ConfigDir C:\clips\csdm

.EXAMPLE
.\record-clips.ps1 -ConfigDir C:\clips\csdm -Only scrim1 -SkipAnalyze
#>
param(
    # The folder clip-list -csdm wrote, one <demo>.json per demo.
    [Parameter(Mandatory = $true)][string]$ConfigDir,
    # The CS Demo Manager command line. "csdm" when it is on PATH.
    [string]$Csdm = "csdm",
    # Record only these demos (basenames, no .dem).
    [string[]]$Only = @(),
    # Skip `csdm analyze`, for demos already in CS Demo Manager.
    [switch]$SkipAnalyze
)

$ErrorActionPreference = "Stop"

if (-not (Get-Command $Csdm -ErrorAction SilentlyContinue)) {
    throw "CS Demo Manager's CLI ($Csdm) was not found. Install CS Demo Manager, or pass -Csdm with the full path to csdm.exe."
}

$configs = Get-ChildItem -Path $ConfigDir -Filter *.json | Sort-Object Name
if ($Only.Count -gt 0) {
    $configs = $configs | Where-Object { $Only -contains $_.BaseName }
}
if (-not $configs) {
    throw "No configs to record in $ConfigDir."
}

$results = @()
foreach ($cfg in $configs) {
    $config = Get-Content -Raw -Path $cfg.FullName | ConvertFrom-Json
    $demo = $config.demoPath
    $count = @($config.sequences).Count
    Write-Host ""
    Write-Host "== $($cfg.BaseName): $count clip(s) from $demo" -ForegroundColor Cyan

    if (-not (Test-Path -LiteralPath $demo)) {
        Write-Warning "Demo not found: $demo. Copy it there, or rerun clip-list with the right -demo-dir."
        $results += [pscustomobject]@{ Demo = $cfg.BaseName; Clips = $count; Result = "demo missing" }
        continue
    }

    if (-not $SkipAnalyze) {
        # csdm skips a demo it already has, so this is cheap on a rerun.
        & $Csdm analyze $demo
        if ($LASTEXITCODE -ne 0) {
            $results += [pscustomobject]@{ Demo = $cfg.BaseName; Clips = $count; Result = "analyze failed ($LASTEXITCODE)" }
            continue
        }
    }

    $started = Get-Date
    & $Csdm video --config-file $cfg.FullName
    $took = [int]((Get-Date) - $started).TotalSeconds
    $result = if ($LASTEXITCODE -eq 0) { "recorded in ${took}s" } else { "video failed ($LASTEXITCODE)" }
    $results += [pscustomobject]@{ Demo = $cfg.BaseName; Clips = $count; Result = $result }
}

Write-Host ""
$results | Format-Table -AutoSize
if ($results | Where-Object { $_.Result -notlike "recorded*" }) { exit 1 }
