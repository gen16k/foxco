# run-sandbox.ps1 -- HOST-side launcher for the transparent-HTTPS-interception
# integration test in Windows Sandbox.
#
# It (1) builds proxy.exe on the host, (2) prepares a clean shared-results folder
# OUTSIDE the proxy-server tree (so it does not collide with the read-only repo
# mapping), (3) generates a concrete .wsb with absolute host paths derived from this
# script's own location (nothing machine-specific is committed), and (4) launches
# Windows Sandbox. The sandbox auto-runs test\sandbox\run-tests.ps1 at logon and
# writes transcript.txt + results.json + DONE into the shared folder.
#
#   .\test\sandbox\run-sandbox.ps1            # build + generate + launch
#   .\test\sandbox\run-sandbox.ps1 -SkipBuild # reuse existing test\sandbox\proxy.exe
#   .\test\sandbox\run-sandbox.ps1 -NoLaunch  # build + generate only (print paths)
#
# Safety: all dangerous mutations (hosts file, trust store, :443, redirect) happen
# only inside the disposable, NAT-isolated VM. The host repo is mapped READ-ONLY.

[CmdletBinding()]
param(
    [switch]$SkipBuild,
    [switch]$NoLaunch
)

$ErrorActionPreference = "Stop"

$repo  = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path        # proxy-server root
$share = Join-Path $env:TEMP "foxco-dlp-sandbox-share"                # outside the repo AND outside the read-only map
$exe   = Join-Path $PSScriptRoot "proxy.exe"
$wsb   = Join-Path $share "dlp-proxy-test.wsb"

Write-Host "Repo (read-only map): $repo"
Write-Host "Shared results dir  : $share"

# 1. Build proxy.exe on the host (the sandbox has no Go toolchain).
if (-not $SkipBuild) {
    Write-Host "Building proxy.exe (host)..."
    Push-Location $repo
    try {
        & go build -o $exe .\cmd\proxy
        if ($LASTEXITCODE -ne 0) { throw "go build failed ($LASTEXITCODE)" }
    } finally { Pop-Location }
} elseif (-not (Test-Path $exe)) {
    throw "-SkipBuild set but $exe not found; build it first."
}
Write-Host "proxy.exe: $exe"

# 2. Prepare a fresh shared-results folder. We clear known artifacts rather than
#    removing the directory, because a previous sandbox's VM worker (vmwp) can keep
#    a handle on the mapped folder during teardown, which blocks Remove-Item on the
#    directory itself. Writing/overwriting files inside it still works.
New-Item -ItemType Directory -Force -Path $share | Out-Null
Get-ChildItem $share -File -ErrorAction SilentlyContinue | ForEach-Object {
    try { Remove-Item $_.FullName -Force -ErrorAction Stop }
    catch { Write-Host "  (could not remove $($_.Name): in use, will overwrite)" }
}

# 3. Generate the .wsb (UTF-8, no BOM; WindowsSandbox parses it as XML).
$xml = @"
<Configuration>
  <VGpu>Disable</VGpu>
  <Networking>Default</Networking>
  <MappedFolders>
    <MappedFolder>
      <HostFolder>$repo</HostFolder>
      <SandboxFolder>C:\repo</SandboxFolder>
      <ReadOnly>true</ReadOnly>
    </MappedFolder>
    <MappedFolder>
      <HostFolder>$share</HostFolder>
      <SandboxFolder>C:\share</SandboxFolder>
      <ReadOnly>false</ReadOnly>
    </MappedFolder>
  </MappedFolders>
  <LogonCommand>
    <Command>powershell.exe -NoProfile -ExecutionPolicy Bypass -File C:\repo\test\sandbox\run-tests.ps1</Command>
  </LogonCommand>
</Configuration>
"@
[System.IO.File]::WriteAllText($wsb, $xml, (New-Object System.Text.UTF8Encoding($false)))
Write-Host "Generated: $wsb"

# 4. Launch (only one Windows Sandbox instance may run at a time).
if ($NoLaunch) {
    Write-Host "NoLaunch set; not starting the sandbox."
} else {
    $running = Get-Process -Name WindowsSandbox, WindowsSandboxClient -ErrorAction SilentlyContinue
    if ($running) { throw "A Windows Sandbox instance is already running; close it first (only one allowed)." }
    Write-Host "Launching Windows Sandbox..."
    Start-Process WindowsSandbox.exe -ArgumentList $wsb
}

Write-Host ""
Write-Host "SHARE=$share"
Write-Host "WSB=$wsb"
Write-Host "Poll for DONE at: $(Join-Path $share 'DONE')"
