# run-claude-tests.ps1 -- executes INSIDE Windows Sandbox (via run-sandbox.ps1 -Runner run-claude-tests.ps1).
#
# Phase 2 of the integration test: drive the REAL Claude Code CLI through the
# transparent proxy and confirm (a) Claude Code (Node/OpenSSL) trusts our locally
# installed CA so its HTTPS to api.anthropic.com is intercepted, and (b) the DLP
# pipeline blocks a secret prompt before any egress while passing a benign one.
#
# We test CA trust TWO ways: system-store-only (install.ps1 puts the CA in
# LocalMachine\Root; Claude Code's default CLAUDE_CODE_CERT_STORE="bundled,system"
# should pick it up) and explicit NODE_EXTRA_CA_CERTS. Auth uses an INVALID api key
# (no secret, no valid key): a benign prompt then yields a real 401 *through* the
# proxy (proves interception + upstream reach); a secret prompt is BLOCKED locally.
#
# ASCII-only (PS 5.1 safe). Japanese block text is verified host-side from the saved
# claude-*.txt outputs (we match the ASCII marker LOCAL_DLP_NOTE here).

$ErrorActionPreference = "Continue"

$share      = "C:\share"
$transcript = Join-Path $share "transcript-claude.txt"
$resultsPath= Join-Path $share "results-claude.json"
$donePath   = Join-Path $share "DONE"
$logFile    = Join-Path $env:ProgramData "LocalLfmDlpProxy\logs\proxy.log"
$cfgFile    = Join-Path $env:ProgramData "LocalLfmDlpProxy\config.yaml"
$caCert     = Join-Path $env:ProgramData "LocalLfmDlpProxy\ca\ca.crt"
$repoSrc    = "C:\repo"
$work       = "C:\work\proxy-server"
$fakeKey    = "sk-ant-sandbox-invalid-0000000000"

New-Item -ItemType Directory -Force -Path $share | Out-Null
Remove-Item $donePath -ErrorAction SilentlyContinue
Start-Transcript -Path $transcript -Force | Out-Null

$results = New-Object System.Collections.Generic.List[object]
function Add-Result($name, $expect, $got, $pass) {
    $results.Add([pscustomobject][ordered]@{ name = $name; expect = $expect; got = "$got"; pass = [bool]$pass })
    $tag = if ($pass) { "PASS" } else { "FAIL" }
    Write-Host ("[{0}] {1} | expect: {2} | got: {3}" -f $tag, $name, $expect, $got)
}
function Save-Results {
    $json = $results | ConvertTo-Json -Depth 6
    [System.IO.File]::WriteAllText($resultsPath, $json, (New-Object System.Text.UTF8Encoding($false)))
}
# Safe single-line snippet for the results 'got' field (collapse whitespace, clamp).
function Short($s, $n) {
    $t = "$s" -replace '\s+', ' '
    if ($t.Length -le $n) { return $t }
    return $t.Substring(0, $n)
}
function Test-Port($p) {
    try {
        $c = New-Object System.Net.Sockets.TcpClient
        $iar = $c.BeginConnect("127.0.0.1", $p, $null, $null)
        $ok = $iar.AsyncWaitHandle.WaitOne(800)
        if ($ok -and $c.Connected) { $c.EndConnect($iar); $c.Close(); return $true }
        $c.Close(); return $false
    } catch { return $false }
}
function Wait-Port($p, $sec) {
    $deadline = (Get-Date).AddSeconds($sec)
    while ((Get-Date) -lt $deadline) { if (Test-Port $p) { return $true }; Start-Sleep -Milliseconds 400 }
    return $false
}
function Log-LineCount { return @(Get-Content $logFile -ErrorAction SilentlyContinue).Count }
function Log-Tail($since) {
    $all = @(Get-Content $logFile -ErrorAction SilentlyContinue)
    if ($all.Count -le $since) { return "" }
    return ($all[$since..($all.Count-1)] -join "`n")
}
function Copy-Log { if (Test-Path $logFile) { Copy-Item $logFile (Join-Path $share "proxy-claude.log") -Force -ErrorAction SilentlyContinue } }

# Run `claude -p <prompt>` in an isolated child (own env), with a hard timeout so a
# stray interactive prompt cannot hang the run. $useExtra toggles NODE_EXTRA_CA_CERTS.
function Invoke-Claude($name, $claudeExe, $prompt, $useExtra, $timeoutSec = 120) {
    $job = Start-Job -ScriptBlock {
        param($exe, $prompt, $key, $caCert, $useExtra)
        $env:ANTHROPIC_API_KEY = $key
        Remove-Item Env:ANTHROPIC_BASE_URL -ErrorAction SilentlyContinue
        Remove-Item Env:HTTPS_PROXY -ErrorAction SilentlyContinue
        Remove-Item Env:HTTP_PROXY -ErrorAction SilentlyContinue
        if ($useExtra) { $env:NODE_EXTRA_CA_CERTS = $caCert } else { Remove-Item Env:NODE_EXTRA_CA_CERTS -ErrorAction SilentlyContinue }
        $env:DISABLE_TELEMETRY = "1"; $env:DISABLE_AUTOUPDATER = "1"; $env:CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC = "1"
        & $exe -p $prompt 2>&1 | Out-String
    } -ArgumentList $claudeExe, $prompt, $fakeKey, $caCert, $useExtra
    $done = Wait-Job $job -Timeout $timeoutSec
    if ($done) { $out = (Receive-Job $job | Out-String) } else { Stop-Job $job -ErrorAction SilentlyContinue; $out = "(TIMED OUT after ${timeoutSec}s)" }
    Remove-Job $job -Force -ErrorAction SilentlyContinue
    [System.IO.File]::WriteAllText((Join-Path $share "$name.txt"), $out, (New-Object System.Text.UTF8Encoding($false)))
    Write-Host ("  claude[{0}] useExtra={1} outlen={2}" -f $name, $useExtra, $out.Length)
    return $out
}

$tlsErrRe = "(?i)self.?signed|unable to (verify|get)|UNABLE_TO|SELF_SIGNED|ERR_TLS|CERT_|certificate chain|local issuer"

try {
    Write-Host "===== ENVIRONMENT ====="
    $isAdmin = ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltinRole]::Administrator)
    Write-Host ("Elevated : {0}" -f $isAdmin)
    Write-Host ("PSVer    : {0}" -f $PSVersionTable.PSVersion)
    Add-Result "env.elevated" "True" $isAdmin $isAdmin
    Save-Results

    # ----- Stage + install proxy (transparent), reconfigure to keyword, start -----
    Write-Host "===== STAGE + INSTALL ====="
    New-Item -ItemType Directory -Force -Path "C:\work" | Out-Null
    Copy-Item -Recurse -Force $repoSrc $work
    $builtExe = Join-Path $work "test\sandbox\proxy.exe"
    if (Test-Path $builtExe) { Copy-Item $builtExe (Join-Path $work "proxy.exe") -Force }
    Set-Location $work
    try { & "$work\install.ps1" -SkipBuild *>&1 | ForEach-Object { Write-Host "  $_" } } catch { Write-Host "install raised: $($_.Exception.Message)" }

    # keyword classifier + warmup off -> deterministic ALLOW/BLOCK without an LFM.
    $c = Get-Content -Raw $cfgFile
    $c = $c -replace '(?m)^(\s*type:\s*)"llama_cpp_http"', '$1"keyword"'
    $c = $c -replace '(?m)^(\s*warmup_on_start:\s*)true', '$1false'
    [System.IO.File]::WriteAllText($cfgFile, $c, (New-Object System.Text.UTF8Encoding($false)))

    $caOk = Test-Path $caCert
    Add-Result "install.ca_cert_present" "ca.crt exists for NODE_EXTRA_CA_CERTS" $caCert $caOk
    $caStore = [bool](Get-ChildItem Cert:\LocalMachine\Root -ErrorAction SilentlyContinue | Where-Object { $_.Subject -like "*Local LFM DLP Proxy CA*" })
    Add-Result "install.ca_in_system_store" "CA in LocalMachine\Root" $caStore $caStore

    try { & "$work\proxyctl.ps1" start *>&1 | ForEach-Object { Write-Host "  $_" } } catch { Write-Host "start raised: $($_.Exception.Message)" }
    $port443 = Wait-Port 443 30
    ipconfig /flushdns | Out-Null
    $addrs = @(); try { $addrs = [System.Net.Dns]::GetHostAddresses("api.anthropic.com") | ForEach-Object { $_.IPAddressToString } } catch {}
    Add-Result "start.port443_listening" "127.0.0.1:443 accepts" $port443 $port443
    Add-Result "start.redirect_resolves" "api.anthropic.com -> 127.0.0.1" ($addrs -join ",") ($addrs -contains "127.0.0.1")
    Copy-Log; Save-Results

    # ----- Install Claude Code (native installer, isolated child process) -----
    Write-Host "===== INSTALL CLAUDE CODE ====="
    $installOut = & powershell -NoProfile -ExecutionPolicy Bypass -Command "irm https://claude.ai/install.ps1 | iex" 2>&1 | Out-String
    [System.IO.File]::WriteAllText((Join-Path $share "install-claude.txt"), $installOut, (New-Object System.Text.UTF8Encoding($false)))
    $claudeExe = Join-Path $env:USERPROFILE ".local\bin\claude.exe"
    if (-not (Test-Path $claudeExe)) {
        $cand = Get-Command claude.exe -ErrorAction SilentlyContinue
        if ($cand) { $claudeExe = $cand.Source }
    }
    $claudeOk = Test-Path $claudeExe
    Add-Result "claude.installed" "claude.exe present" $claudeExe $claudeOk
    if ($claudeOk) { Write-Host ("  claude version: " + ((& $claudeExe --version 2>&1 | Out-String).Trim())) }
    Copy-Log; Save-Results

    if ($claudeOk) {
        $benign = "What is the capital of France? Reply in one word."
        $secret = "Remember this credential: my password is hunter2 and my api_key is sk-test-12345."

        # (1) Benign, SYSTEM STORE ONLY (no NODE_EXTRA_CA_CERTS) -> does install.ps1's
        #     LocalMachine\Root install alone make Claude Code trust the CA?
        Write-Host "===== CLAUDE (benign, system-store only) ====="
        $n = Log-LineCount
        $o1 = Invoke-Claude "claude-benign-systemstore" $claudeExe $benign $false
        Start-Sleep -Milliseconds 1500; $t1 = Log-Tail $n
        $reached1 = [bool]($t1 -match "result=(ALLOW|BLOCK|PASSTHROUGH)")
        Add-Result "claude.systemstore_reached_proxy" "request reached proxy (CA trusted via system store)" $reached1 $reached1
        Add-Result "claude.systemstore_no_tls_error" "no TLS/cert error in claude output" (Short $o1 120) (-not ($o1 -match $tlsErrRe))
        Copy-Log; Save-Results

        # (2) Benign, with NODE_EXTRA_CA_CERTS -> explicit env-var trust path.
        Write-Host "===== CLAUDE (benign, NODE_EXTRA_CA_CERTS) ====="
        $n = Log-LineCount
        $o2 = Invoke-Claude "claude-benign-nodeextra" $claudeExe $benign $true
        Start-Sleep -Milliseconds 1500; $t2 = Log-Tail $n
        $reached2 = [bool]($t2 -match "result=(ALLOW|BLOCK|PASSTHROUGH)")
        $allow2 = [bool]($t2 -match "result=ALLOW")
        Add-Result "claude.nodeextra_reached_proxy" "request reached proxy (CA trusted via NODE_EXTRA_CA_CERTS)" $reached2 $reached2
        Add-Result "claude.nodeextra_allow_forwarded" "benign ALLOW forwarded upstream (log)" $allow2 $allow2
        Add-Result "claude.benign_upstream_401" "claude saw a real 401/auth error from upstream" (Short $o2 160) ([bool]($o2 -match "(?i)401|authentication|invalid x-api-key|invalid_api_key"))
        Copy-Log; Save-Results

        # (3) Secret prompt -> DLP BLOCK before egress; block notice reaches Claude Code.
        Write-Host "===== CLAUDE (secret, expect BLOCK) ====="
        $n = Log-LineCount
        $o3 = Invoke-Claude "claude-block" $claudeExe $secret $true
        Start-Sleep -Milliseconds 1500; $t3 = Log-Tail $n
        $blockLog = [bool]($t3 -match "result=BLOCK" -and $t3 -notmatch "classifier_unavailable")
        $noAllow3 = -not ($t3 -match "result=ALLOW upstream_status")
        $clientGotBlock = [bool]($o3 -match "LOCAL_DLP_NOTE")
        Add-Result "claude.block_logged" "log: BLOCK (content, not warming)" $blockLog $blockLog
        Add-Result "claude.block_no_egress" "no ALLOW/upstream call for the secret turn" $noAllow3 $noAllow3
        Add-Result "claude.block_reached_client" "block notice (LOCAL_DLP_NOTE) reached Claude Code" $clientGotBlock $clientGotBlock
        Copy-Log; Save-Results
    }

    # ----- Uninstall (revert) -----
    Write-Host "===== UNINSTALL ====="
    try { & "$work\proxyctl.ps1" stop *>&1 | Out-Null } catch {}
    try { & "$work\uninstall.ps1" *>&1 | ForEach-Object { Write-Host "  $_" } } catch { Write-Host "uninstall raised: $($_.Exception.Message)" }
    ipconfig /flushdns | Out-Null
    $svcGone = -not (Get-Service -Name LocalLfmDlpProxy -ErrorAction SilentlyContinue)
    $caGone = -not (Get-ChildItem Cert:\LocalMachine\Root -ErrorAction SilentlyContinue | Where-Object { $_.Subject -like "*Local LFM DLP Proxy CA*" })
    $hostsTxt = Get-Content -Raw (Join-Path $env:SystemRoot "System32\drivers\etc\hosts") -ErrorAction SilentlyContinue
    Add-Result "uninstall.service_removed" "service gone" $svcGone $svcGone
    Add-Result "uninstall.ca_removed" "CA removed from store" $caGone $caGone
    Add-Result "uninstall.hosts_clean" "hosts block stripped" (-not ($hostsTxt -match "LocalLfmDlpProxy")) (-not ($hostsTxt -match "LocalLfmDlpProxy"))
    Save-Results
}
catch {
    Write-Host "FATAL: $($_.Exception.Message)"
    Write-Host $_.ScriptStackTrace
}
finally {
    Copy-Log
    Save-Results
    $passCount = (@($results | Where-Object { $_.pass })).Count
    $total = $results.Count
    Write-Host ("===== DONE (claude): {0}/{1} checks passed =====" -f $passCount, $total)
    Stop-Transcript | Out-Null
    Set-Content -Path $donePath -Value ("claude {0}/{1}" -f $passCount, $total) -Encoding ascii
}

# Auto-dispose the VM so this iterative run is hands-off (a cosmetic "Windows
# Sandbox" error dialog may flash as the guest shuts down - harmless). All results
# are already persisted to the host-side mapped share before this point.
Start-Sleep -Seconds 3
shutdown /s /t 0
