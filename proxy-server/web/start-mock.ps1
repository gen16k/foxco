#requires -Version 5.1
<#
.SYNOPSIS
  Start the admin UI in MOCK mode on a separate port — for demos.

.DESCRIPTION
  Serves fake data via lib/mock.ts (USE_MOCK=1), so the dashboard — including the
  Network Flow tab — can be shown WITHOUT a running proxy or real Claude traffic.

  It runs on its own port AND its own build output dir (.next-mock), so it never
  collides with a real instance on the default port (3939) using ".next". You can
  therefore run this demo alongside your normal server.

.PARAMETER Port
  Port to listen on. Default 3940 (real instance uses 3939).

.PARAMETER Prod
  Build once and serve the optimized production bundle (smoothest for a live demo).
  Default is `next dev`, which starts instantly with no build step.

.EXAMPLE
  .\start-mock.ps1
  # mock dev server on http://127.0.0.1:3940

.EXAMPLE
  .\start-mock.ps1 -Port 4000 -Prod
  # production build of the mock, served on http://127.0.0.1:4000
#>
[CmdletBinding()]
param(
  [int]$Port = 3940,
  [switch]$Prod
)

$ErrorActionPreference = "Stop"
Set-Location $PSScriptRoot

if (-not (Test-Path (Join-Path $PSScriptRoot "node_modules"))) {
  Write-Host "node_modules not found. Run 'npm install' in this directory first." -ForegroundColor Red
  exit 1
}

# Mock data + an isolated build dir so this can run beside the real instance.
$env:USE_MOCK = "1"
$env:NEXT_DIST_DIR = ".next-mock"

$url = "http://127.0.0.1:$Port"
Write-Host ""
Write-Host "  FoxCo admin UI - MOCK demo" -ForegroundColor Cyan
Write-Host "  URL    : $url"
Write-Host "  Login  : admin / admin  (unless overridden in .env.local)"
Write-Host "  Tab    : Network Flow"
Write-Host "  Output : $($env:NEXT_DIST_DIR)  (isolated from the real .next)" -ForegroundColor DarkGray
Write-Host ""

if ($Prod) {
  Write-Host "Building production bundle into $($env:NEXT_DIST_DIR) ..." -ForegroundColor Yellow
  & npx next build
  if ($LASTEXITCODE -ne 0) { throw "next build failed" }
  & npx next start -p $Port -H 127.0.0.1
}
else {
  & npx next dev -p $Port -H 127.0.0.1
}
