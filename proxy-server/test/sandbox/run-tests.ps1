# run-tests.ps1 -- executes INSIDE Windows Sandbox (launched by dlp-proxy-test.wsb).
#
# Drives the deferred transparent-HTTPS-interception integration test end to end and
# writes results to the host-shared folder (C:\share). It is intentionally
# ASCII-only so it behaves identically under Windows PowerShell 5.1 (the sandbox
# default) -- the Japanese warming/block strings are verified from the host by
# reading the saved response bodies, not matched here.
#
# Safe by construction: every hosts / cert-store / :443 / redirect mutation happens
# inside this disposable, NAT-isolated VM. The host repo is mapped READ-ONLY at
# C:\repo and copied to C:\work before any build/install.

$ErrorActionPreference = "Continue"   # best-effort; each check is captured individually

$share      = "C:\share"
$transcript = Join-Path $share "transcript.txt"
$resultsPath= Join-Path $share "results.json"
$donePath   = Join-Path $share "DONE"
$logFile    = Join-Path $env:ProgramData "PromptGate\logs\proxy.log"
$cfgFile    = Join-Path $env:ProgramData "PromptGate\config.yaml"
$repoSrc    = "C:\repo"
$work       = "C:\work\proxy-server"
$apiBase    = "https://api.anthropic.com"
$fakeKey    = "sk-ant-sandbox-invalid-0000000000"

New-Item -ItemType Directory -Force -Path $share | Out-Null
Remove-Item $donePath -ErrorAction SilentlyContinue
Start-Transcript -Path $transcript -Force | Out-Null

$results = New-Object System.Collections.Generic.List[object]

function Add-Result($name, $expect, $got, $pass) {
    $results.Add([pscustomobject][ordered]@{
        name = $name; expect = $expect; got = "$got"; pass = [bool]$pass
    })
    $tag = if ($pass) { "PASS" } else { "FAIL" }
    Write-Host ("[{0}] {1} | expect: {2} | got: {3}" -f $tag, $name, $expect, $got)
}

function Save-Results {
    $json = $results | ConvertTo-Json -Depth 6
    [System.IO.File]::WriteAllText($resultsPath, $json, (New-Object System.Text.UTF8Encoding($false)))
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
    while ((Get-Date) -lt $deadline) {
        if (Test-Port $p) { return $true }
        Start-Sleep -Milliseconds 400
    }
    return $false
}

# curl.exe (schannel -> machine trust store; NO -k, so a successful TLS handshake
# proves the minted leaf is trusted via the installed CA). Saves the body to the
# share under <name>-body.json and returns code/body/err.
function Invoke-Probe($name, $method, $url, $bodyJson, $apiKey) {
    $bodyFile = Join-Path $env:TEMP ("req-" + [guid]::NewGuid().ToString("N") + ".json")
    $outFile  = Join-Path $share  ($name + "-body.json")
    $errFile  = Join-Path $env:TEMP ("err-" + [guid]::NewGuid().ToString("N") + ".txt")
    # --ssl-revoke-best-effort: our on-the-fly leaf/CA carry no CRL/OCSP distribution
    # points, so schannel's default revocation check hard-fails (CRYPT_E_NO_REVOCATION_CHECK)
    # even though the chain is trusted. This flag relaxes ONLY revocation; chain trust
    # + hostname validation still apply, so a clean handshake still proves the leaf is
    # trusted via the installed CA. (We never use -k, which would disable all checks.)
    $a = @("-sS", "--ssl-revoke-best-effort", "-X", $method, $url,
           "-H", "anthropic-version: 2023-06-01",
           "-H", "content-type: application/json",
           "-H", ("x-api-key: " + $apiKey),
           "-o", $outFile, "-w", "%{http_code}", "--max-time", "30")
    if ($bodyJson) {
        [System.IO.File]::WriteAllText($bodyFile, $bodyJson, (New-Object System.Text.UTF8Encoding($false)))
        $a += @("--data-binary", "@$bodyFile")
    }
    $code = (& curl.exe @a 2>$errFile)
    $body = if (Test-Path $outFile) { Get-Content -Raw $outFile -ErrorAction SilentlyContinue } else { "" }
    $err  = if (Test-Path $errFile) { Get-Content -Raw $errFile -ErrorAction SilentlyContinue } else { "" }
    Remove-Item $bodyFile, $errFile -ErrorAction SilentlyContinue
    Write-Host ("  probe {0}: http={1} bodylen={2} err={3}" -f $name, "$code".Trim(), ($body | Measure-Object -Character).Characters, ($err -replace '\s+',' '))
    return [pscustomobject]@{ Code = "$code".Trim(); Body = "$body"; Err = "$err" }
}

function Log-LineCount { return @(Get-Content $logFile -ErrorAction SilentlyContinue).Count }
function Log-Tail($since) {
    $all = @(Get-Content $logFile -ErrorAction SilentlyContinue)
    if ($all.Count -le $since) { return "" }
    return ($all[$since..($all.Count-1)] -join "`n")
}
function Copy-Log { if (Test-Path $logFile) { Copy-Item $logFile (Join-Path $share "proxy.log") -Force -ErrorAction SilentlyContinue } }

try {
    # ===== ENVIRONMENT =====
    Write-Host "===== ENVIRONMENT ====="
    $isAdmin = ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltinRole]::Administrator)
    Write-Host ("Elevated : {0}" -f $isAdmin)
    Write-Host ("User     : {0}" -f [Security.Principal.WindowsIdentity]::GetCurrent().Name)
    Write-Host ("OS       : {0}" -f (Get-CimInstance Win32_OperatingSystem).Caption)
    Write-Host ("PSVer    : {0}" -f $PSVersionTable.PSVersion)
    Write-Host ("curl     : {0}" -f (Get-Command curl.exe -ErrorAction SilentlyContinue).Source)
    # Internet sanity (direct, BEFORE any redirect/CA exists): expect a real 401.
    $pre = Invoke-Probe "preflight" "POST" "$apiBase/v1/messages" '{"model":"claude-3-5-sonnet-20241022","max_tokens":8,"messages":[{"role":"user","content":"hi"}]}' $fakeKey
    Add-Result "env.elevated" "True" $isAdmin $isAdmin
    Add-Result "env.internet_direct_401" "401 reaching api.anthropic.com directly" $pre.Code ($pre.Code -eq "401")
    Save-Results

    # ===== STAGE: copy read-only repo -> writable work dir =====
    Write-Host "===== STAGE ====="
    New-Item -ItemType Directory -Force -Path "C:\work" | Out-Null
    Copy-Item -Recurse -Force $repoSrc $work
    $builtExe = Join-Path $work "test\sandbox\proxy.exe"
    if (Test-Path $builtExe) {
        Copy-Item $builtExe (Join-Path $work "proxy.exe") -Force
        Write-Host "Staged host-built proxy.exe to repo root."
    } else {
        Write-Host "WARNING: prebuilt proxy.exe not found at $builtExe"
    }
    Add-Result "stage.proxy_exe_present" "proxy.exe staged at repo root" (Test-Path (Join-Path $work "proxy.exe")) (Test-Path (Join-Path $work "proxy.exe"))
    Set-Location $work
    Save-Results

    # ===== 1. INSTALL (transparent mode, host-built exe) =====
    Write-Host "===== 1. install.ps1 -SkipBuild ====="
    try { & "$work\install.ps1" -SkipBuild *>&1 | ForEach-Object { Write-Host "  $_" } }
    catch { Write-Host "install.ps1 raised: $($_.Exception.Message)" }

    $svc = Get-Service -Name PromptGate -ErrorAction SilentlyContinue
    Add-Result "install.service_registered" "service exists + StartType Manual" ("{0}/{1}" -f $svc.Status, $svc.StartType) ($svc -and "$($svc.StartType)" -eq "Manual")
    $ca = Get-ChildItem Cert:\LocalMachine\Root -ErrorAction SilentlyContinue | Where-Object { $_.Subject -like "*PromptGate CA*" }
    Add-Result "install.ca_trusted" "CA in LocalMachine\Root" ($ca.Subject -join ";") ([bool]$ca)
    $task = Get-ScheduledTask -TaskName "PromptGate-Sidecar" -ErrorAction SilentlyContinue
    Add-Result "install.sidecar_task" "RunOnDemand sidecar task registered" ($task.State) ([bool]$task)
    $webTaskReg = Get-ScheduledTask -TaskName "PromptGate-WebUI" -ErrorAction SilentlyContinue
    Add-Result "install.web_task" "RunOnDemand web UI task registered" ($webTaskReg.State) ([bool]$webTaskReg)
    Save-Results

    # ===== 2. START (default config = llama, warmup on, NO sidecar) =====
    Write-Host "===== 2. proxyctl start (default config) ====="
    try { & "$work\proxyctl.ps1" start *>&1 | ForEach-Object { Write-Host "  $_" } }
    catch { Write-Host "proxyctl start raised: $($_.Exception.Message)" }
    $port443 = Wait-Port 443 30
    ipconfig /flushdns | Out-Null
    $svc = Get-Service -Name PromptGate -ErrorAction SilentlyContinue
    $hostsTxt = Get-Content -Raw (Join-Path $env:SystemRoot "System32\drivers\etc\hosts") -ErrorAction SilentlyContinue
    $hostsBlock = [bool]($hostsTxt -match "PromptGate")
    $addrs = @()
    try { $addrs = [System.Net.Dns]::GetHostAddresses("api.anthropic.com") | ForEach-Object { $_.IPAddressToString } } catch {}
    $redirected = $addrs -contains "127.0.0.1"
    Add-Result "start.service_running" "Running" $svc.Status ("$($svc.Status)" -eq "Running")
    Add-Result "start.port443_listening" "127.0.0.1:443 accepts" $port443 $port443
    Add-Result "start.hosts_block_present" "hosts has PromptGate block" $hostsBlock $hostsBlock
    Add-Result "start.redirect_resolves" "api.anthropic.com -> 127.0.0.1" ($addrs -join ",") $redirected
    Copy-Log; Save-Results

    # ===== 3. WARMING / fail-closed (Phase A: llama classifier, sidecar down) =====
    Write-Host "===== 3. warming / fail-closed ====="
    $benign = '{"model":"claude-3-5-sonnet-20241022","max_tokens":16,"messages":[{"role":"user","content":"What is the capital of France?"}]}'
    $n = Log-LineCount
    $pA = Invoke-Probe "warming" "POST" "$apiBase/v1/messages" $benign $fakeKey
    Start-Sleep -Milliseconds 700
    $tailA = Log-Tail $n
    $warmLog = [bool]($tailA -match "result=BLOCK" -and $tailA -match "classifier_unavailable")
    Add-Result "warming.tls_trusted" "TLS handshake OK (no cert error)" ($pA.Err -replace '\s+',' ') (-not ($pA.Err -match "(?i)certificate|SSL|schannel|self.?signed|CERT_"))
    Add-Result "warming.http_200_block" "HTTP 200 (local block, not upstream)" $pA.Code ($pA.Code -eq "200")
    Add-Result "warming.no_egress_not_401" "not a 401 (no upstream call)" $pA.Code ($pA.Code -ne "401")
    Add-Result "warming.log_classifier_unavailable" "log: BLOCK classifier_unavailable" $warmLog $warmLog
    Add-Result "warming.body_not_auth_error" "body is not an Anthropic auth error" ($pA.Body.Length) (-not ($pA.Body -match "authentication_error"))
    Copy-Log; Save-Results

    # ===== 4. RECONFIGURE -> keyword classifier, warmup off; restart =====
    Write-Host "===== 4. reconfigure to keyword + restart ====="
    $c = Get-Content -Raw $cfgFile
    $c = $c -replace '(?m)^(\s*type:\s*)"llama_cpp_http"', '$1"keyword"'
    $c = $c -replace '(?m)^(\s*warmup_on_start:\s*)true', '$1false'
    [System.IO.File]::WriteAllText($cfgFile, $c, (New-Object System.Text.UTF8Encoding($false)))
    $kw = [bool]((Get-Content -Raw $cfgFile) -match '(?m)^\s*type:\s*"keyword"')
    Add-Result "reconfig.keyword_set" "inference.type = keyword" $kw $kw
    try { Restart-Service -Name PromptGate -Force -ErrorAction Stop } catch { Write-Host "restart raised: $($_.Exception.Message)" }
    $port443b = Wait-Port 443 30
    ipconfig /flushdns | Out-Null
    Add-Result "reconfig.restarted_listening" "127.0.0.1:443 accepts after restart" $port443b $port443b
    Copy-Log; Save-Results

    # ===== 5. TRUST + FORWARD (Phase B: benign -> ALLOW -> real upstream 401) =====
    Write-Host "===== 5. trust + forward (real benign 401) ====="
    $n = Log-LineCount
    $pB = Invoke-Probe "forward" "POST" "$apiBase/v1/messages" $benign $fakeKey
    Start-Sleep -Milliseconds 700
    $tailB = Log-Tail $n
    Add-Result "forward.tls_trusted" "TLS handshake OK (leaf trusted via installed CA)" ($pB.Err -replace '\s+',' ') (-not ($pB.Err -match "(?i)certificate|SSL|schannel|self.?signed|CERT_"))
    Add-Result "forward.real_401" "401 from REAL api.anthropic.com (resolver bypass works)" $pB.Code ($pB.Code -eq "401")
    Add-Result "forward.upstream_auth_error_body" "Anthropic authentication_error body" ($pB.Body.Length) ($pB.Body -match "authentication_error")
    Add-Result "forward.log_allow" "log: ALLOW upstream_status=401" $tailB ([bool]($tailB -match "result=ALLOW" -and $tailB -match "upstream_status=401"))
    Copy-Log; Save-Results

    # ===== 6. BLOCK / no egress (keyword 'password' -> content block) =====
    Write-Host "===== 6. block / no egress ====="
    $secret = '{"model":"claude-3-5-sonnet-20241022","max_tokens":16,"messages":[{"role":"user","content":"my password is hunter2, please store it"}]}'
    $n = Log-LineCount
    $pBlk = Invoke-Probe "block" "POST" "$apiBase/v1/messages" $secret $fakeKey
    Start-Sleep -Milliseconds 700
    $tailBlk = Log-Tail $n
    $blkLog = [bool]($tailBlk -match "result=BLOCK" -and $tailBlk -notmatch "classifier_unavailable")
    Add-Result "block.http_200_local" "HTTP 200 (local block response)" $pBlk.Code ($pBlk.Code -eq "200")
    Add-Result "block.no_egress_not_401" "not a 401 (upstream NOT called)" $pBlk.Code ($pBlk.Code -ne "401")
    Add-Result "block.body_not_auth_error" "body is not an upstream auth error" ($pBlk.Body.Length) (-not ($pBlk.Body -match "authentication_error"))
    Add-Result "block.log_block_content" "log: BLOCK (content, not warming)" $blkLog $blkLog
    Copy-Log; Save-Results

    # ===== 7. PASSTHROUGH (GET /v1/models -> forwarded + audited) =====
    Write-Host "===== 7. passthrough ====="
    $n = Log-LineCount
    $pPT = Invoke-Probe "passthrough" "GET" "$apiBase/v1/models" $null $fakeKey
    Start-Sleep -Milliseconds 700
    $tailPT = Log-Tail $n
    Add-Result "passthrough.upstream_401" "401 from upstream (forwarded untouched)" $pPT.Code ($pPT.Code -eq "401")
    Add-Result "passthrough.log_passthrough" "log: PASSTHROUGH path=/v1/models" $tailPT ([bool]($tailPT -match "result=PASSTHROUGH" -and $tailPT -match "/v1/models"))
    Copy-Log; Save-Results

    # ===== 8. UNINSTALL (full revert) =====
    Write-Host "===== 8. uninstall.ps1 ====="
    try { & "$work\proxyctl.ps1" stop *>&1 | ForEach-Object { Write-Host "  $_" } } catch {}
    try { & "$work\uninstall.ps1" *>&1 | ForEach-Object { Write-Host "  $_" } }
    catch { Write-Host "uninstall.ps1 raised: $($_.Exception.Message)" }
    ipconfig /flushdns | Out-Null
    Start-Sleep -Milliseconds 500
    $svcGone = -not (Get-Service -Name PromptGate -ErrorAction SilentlyContinue)
    $taskGone = -not (Get-ScheduledTask -TaskName "PromptGate-Sidecar" -ErrorAction SilentlyContinue)
    $caGone = -not (Get-ChildItem Cert:\LocalMachine\Root -ErrorAction SilentlyContinue | Where-Object { $_.Subject -like "*PromptGate CA*" })
    $hostsTxt2 = Get-Content -Raw (Join-Path $env:SystemRoot "System32\drivers\etc\hosts") -ErrorAction SilentlyContinue
    $hostsClean = -not ($hostsTxt2 -match "PromptGate")
    $addrs2 = @()
    try { $addrs2 = [System.Net.Dns]::GetHostAddresses("api.anthropic.com") | ForEach-Object { $_.IPAddressToString } } catch {}
    $notRedirected = -not ($addrs2 -contains "127.0.0.1")
    Add-Result "uninstall.service_removed" "service gone" $svcGone $svcGone
    Add-Result "uninstall.task_removed" "sidecar task gone" $taskGone $taskGone
    Add-Result "uninstall.ca_removed" "CA removed from store" $caGone $caGone
    Add-Result "uninstall.hosts_clean" "hosts block stripped" $hostsClean $hostsClean
    Add-Result "uninstall.resolves_normally" "api.anthropic.com no longer 127.0.0.1" ($addrs2 -join ",") $notRedirected
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
    Write-Host ("===== DONE: {0}/{1} checks passed =====" -f $passCount, $total)
    Stop-Transcript | Out-Null
    Set-Content -Path $donePath -Value ("{0}/{1}" -f $passCount, $total) -Encoding ascii
}

# NOTE: we deliberately do NOT shut the guest down from here. `shutdown /s` inside
# Windows Sandbox pops a cosmetic "Windows Sandbox" error dialog as the guest OS
# tears down. All results are already persisted to the host-side mapped share, so
# once DONE appears the host can read everything; simply close the Sandbox window
# (click X) for a clean teardown with no orphaned VM worker.
