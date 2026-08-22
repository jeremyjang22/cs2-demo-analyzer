<#
.SYNOPSIS
  Run atlas with the variables from .env loaded.

.DESCRIPTION
  Atlas has no -env-file flag and does not read .env on its own: the getenv()
  calls in atlas.hcl read the real process environment. Without this, the
  config resolves to empty strings and atlas reports the misleading
  'required flag(s) "dev-url" not set'.

.EXAMPLE
  .\migrate.ps1 migrate diff initial
  .\migrate.ps1 migrate apply
  .\migrate.ps1 migrate status
#>
param(
    [Parameter(ValueFromRemainingArguments = $true)]
    [string[]]$Args
)

$ErrorActionPreference = "Stop"
Set-Location $PSScriptRoot

if (-not (Test-Path .env)) {
    Write-Error "no .env here - copy .env.example and fill in both Neon URLs"
}

Get-Content .env | ForEach-Object {
    $line = $_.Trim()
    # Skip blanks and comments; split on the FIRST '=' only, because a
    # connection string contains '=' in its query parameters.
    if ($line -and -not $line.StartsWith("#") -and $line.Contains("=")) {
        $i = $line.IndexOf("=")
        $name = $line.Substring(0, $i).Trim()
        $value = $line.Substring($i + 1).Trim().Trim('"').Trim("'")
        Set-Item -Path "env:$name" -Value $value
    }
}

foreach ($v in @("DATABASE_URL", "DATABASE_DEV_URL")) {
    if (-not (Get-Item -Path "env:$v" -ErrorAction SilentlyContinue).Value) {
        Write-Error "$v is empty in .env"
    }
}

& atlas @Args --env neon
exit $LASTEXITCODE
