<#
.SYNOPSIS
Installs demo-worker on this PC, started at every login, or removes it.

.DESCRIPTION
Builds demo-worker from this checkout into %LOCALAPPDATA%\cs2-demo-worker and
registers a scheduled task that starts it when you log in. Run it again after
pulling to install the new version: it stops the running worker first (a job
in progress goes back to the queue and is picked up again within two
minutes).

The worker reads DATABASE_URL and R2_* from packages\api\.env, the pro
catalogue from packages\web\src\pros.json and the radar files from
assets\radar - all in this checkout, read where they are, so a pull is
picked up without reinstalling for everything but the code.

It has no window. Its log is a file a day in
%LOCALAPPDATA%\cs2-demo-worker\logs. See docs/demo-worker.md.

.EXAMPLE
.\scripts\worker\install-worker.ps1

.EXAMPLE
.\scripts\worker\install-worker.ps1 -Uninstall
#>
param(
    # Remove the task and the installed binary. The logs are left.
    [switch]$Uninstall,
    # The settings file, when it is not this checkout's packages\api\.env.
    [string]$EnvFile = ""
)

$ErrorActionPreference = "Stop"
$TaskName = "cs2-demo-worker"
$Home_ = Join-Path $env:LOCALAPPDATA "cs2-demo-worker"
$Exe = Join-Path $Home_ "demo-worker.exe"
$Logs = Join-Path $Home_ "logs"
$Repo = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path
$Packages = Join-Path $Repo "packages"

function Stop-Worker {
    $task = Get-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
    if ($task -and $task.State -eq "Running") {
        Stop-ScheduledTask -TaskName $TaskName
    }
    # Stop-ScheduledTask ends the process it started; wait for the exe to be
    # free, since a running one cannot be overwritten on Windows.
    for ($i = 0; $i -lt 20; $i++) {
        $running = Get-Process -Name "demo-worker" -ErrorAction SilentlyContinue |
            Where-Object { $_.Path -eq $Exe }
        if (-not $running) { return }
        Start-Sleep -Milliseconds 500
    }
    throw "demo-worker is still running from $Exe; stop it in Task Manager and try again"
}

if ($Uninstall) {
    Stop-Worker
    Unregister-ScheduledTask -TaskName $TaskName -Confirm:$false -ErrorAction SilentlyContinue
    Remove-Item -Force $Exe -ErrorAction SilentlyContinue
    Write-Host "Removed the $TaskName task and $Exe. Logs are still in $Logs."
    return
}

if ($EnvFile -eq "") { $EnvFile = Join-Path $Packages "api\.env" }
if (-not (Test-Path $EnvFile)) {
    throw "No settings file at $EnvFile. Copy packages\api\.env.example and fill in DATABASE_URL and R2_* (docs/uploads.md)."
}
$EnvFile = (Resolve-Path $EnvFile).Path
foreach ($need in @("web\src\pros.json", "..\assets\radar")) {
    if (-not (Test-Path (Join-Path $Packages $need))) { throw "Missing $need under $Packages" }
}
if (-not (Get-Command go -ErrorAction SilentlyContinue)) { throw "Go is not on PATH" }

Stop-Worker
New-Item -ItemType Directory -Force $Home_, $Logs | Out-Null

# The commit, so every job's log and the workers table say which code ran.
# Stamped explicitly: Go's own VCS stamping does not reach this module, which
# sits in packages/ below the repository root.
$version = (git -C $Repo rev-parse --short=12 HEAD 2>$null)
if (-not $version) { $version = "unknown" }
elseif (git -C $Repo status --porcelain --untracked-files=no -- packages 2>$null) { $version += "-dirty" }

# -H=windowsgui: a GUI-subsystem binary, so starting it at login opens no
# console window. Its output goes to the log file instead.
Write-Host "Building demo-worker $version from $Repo"
Push-Location $Packages
try {
    go build -ldflags "-H=windowsgui -X main.version=$version" -o $Exe ./demo-worker
    if ($LASTEXITCODE -ne 0) { throw "go build failed" }
} finally {
    Pop-Location
}

$arguments = @(
    "-env", "`"$EnvFile`"",
    "-pros", "`"$(Join-Path $Packages 'web\src\pros.json')`"",
    "-radar", "`"$(Join-Path $Repo 'assets\radar')`"",
    "-log-dir", "`"$Logs`""
) -join " "
$action = New-ScheduledTaskAction -Execute $Exe -Argument $arguments -WorkingDirectory $Packages
$trigger = New-ScheduledTaskTrigger -AtLogOn -User "$env:USERDOMAIN\$env:USERNAME"
# No time limit, keep going on battery, and come back after a crash: the
# worker is meant to be there whenever the PC is.
$settings = New-ScheduledTaskSettingsSet `
    -ExecutionTimeLimit ([TimeSpan]::Zero) `
    -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries `
    -RestartCount 999 -RestartInterval (New-TimeSpan -Minutes 1) `
    -MultipleInstances IgnoreNew -StartWhenAvailable
# Runs as you, only while you are logged in, without storing a password.
$principal = New-ScheduledTaskPrincipal -UserId "$env:USERDOMAIN\$env:USERNAME" -LogonType Interactive -RunLevel Limited

Register-ScheduledTask -TaskName $TaskName -Action $action -Trigger $trigger -Settings $settings `
    -Principal $principal -Description "Processes demos uploaded to the site (cs2-demo-analyzer, docs/demo-worker.md)" -Force | Out-Null
Start-ScheduledTask -TaskName $TaskName

Start-Sleep -Seconds 3
$state = (Get-ScheduledTask -TaskName $TaskName).State
Write-Host "Installed $Exe and started it ($state); it starts again at every login."
Write-Host "Logs: $Logs"
Write-Host "Stop:  Stop-ScheduledTask $TaskName    Remove: .\scripts\worker\install-worker.ps1 -Uninstall"
