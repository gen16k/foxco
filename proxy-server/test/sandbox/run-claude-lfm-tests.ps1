# run-claude-lfm-tests.ps1 -- executes INSIDE Windows Sandbox
# (via run-sandbox.ps1 -Runner run-claude-lfm-tests.ps1).
#
# Phase 3: re-run the DLP block/allow checks with the REAL LFM classifier (not the
# coarse keyword stand-in), against BOTH a controlled curl request and real Claude
# Code. Goal: show (a) the real LFM cleanly BLOCKS a secret in the live turn (curl,
# deterministic), and (b) the real LFM does NOT over-block Claude Code's normal
# traffic the way keyword did (Claude Code benign -> ALLOW -> upstream 401).
#
# Heavy: downloads a CPU llama.cpp build + the LFM2.5-1.2B GGUF (~1GB) into the
# PERSISTENT cache (C:\cache, mapped read-write) so re-runs are fast. CPU inference
# is slow, so timeouts are generous. No GPU is used (sandbox vGPU disabled).
#
# ASCII-only (PS 5.1 safe). Auto-shuts the VM down when finished.

$ErrorActionPreference = "Continue"

$share      = "C:\share"
$cache      = "C:\cache"
$transcript = Join-Path $share "transcript-lfm.txt"
$resultsPath= Join-Path $share "results-lfm.json"
$donePath   = Join-Path $share "DONE"
$logFile    = Join-Path $env:ProgramData "PromptGate\logs\proxy.log"
$cfgFile    = Join-Path $env:ProgramData "PromptGate\config.yaml"
$caCert     = Join-Path $env:ProgramData "PromptGate\ca\ca.crt"
$repoSrc    = "C:\repo"
$work       = "C:\work\proxy-server"
$apiBase    = "https://api.anthropic.com"
$fakeKey    = "sk-ant-sandbox-invalid-0000000000"
$modelRef   = "LiquidAI/LFM2.5-1.2B-Instruct-GGUF:Q4_K_M"
$llamaDir   = Join-Path $cache "llamacpp"
$modelCache = Join-Path $cache "models"

New-Item -ItemType Directory -Force -Path $share, $cache, $modelCache | Out-Null
Remove-Item $donePath -ErrorAction SilentlyContinue
Start-Transcript -Path $transcript -Force | Out-Null

$results = New-Object System.Collections.Generic.List[object]
function Add-Result($name, $expect, $got, $pass) {
    $results.Add([pscustomobject][ordered]@{ name = $name; expect = $expect; got = "$got"; pass = [bool]$pass })
    $tag = if ($pass) { "PASS" } else { "FAIL" }
    Write-Host ("[{0}] {1} | expect: {2} | got: {3}" -f $tag, $name, $expect, $got)
}
function Save-Results {
    [System.IO.File]::WriteAllText($resultsPath, ($results | ConvertTo-Json -Depth 6), (New-Object System.Text.UTF8Encoding($false)))
}
function Short($s, $n) { $t = "$s" -replace '\s+', ' '; if ($t.Length -le $n) { return $t } return $t.Substring(0, $n) }
function Test-Port($p) {
    try { $c = New-Object System.Net.Sockets.TcpClient; $iar = $c.BeginConnect("127.0.0.1", $p, $null, $null)
        $ok = $iar.AsyncWaitHandle.WaitOne(800); if ($ok -and $c.Connected) { $c.EndConnect($iar); $c.Close(); return $true } $c.Close(); return $false
    } catch { return $false }
}
function Wait-Port($p, $sec) { $d = (Get-Date).AddSeconds($sec); while ((Get-Date) -lt $d) { if (Test-Port $p) { return $true }; Start-Sleep -Milliseconds 400 } return $false }
function Log-LineCount { return @(Get-Content $logFile -ErrorAction SilentlyContinue).Count }
function Log-Tail($since) { $all = @(Get-Content $logFile -ErrorAction SilentlyContinue); if ($all.Count -le $since) { return "" } return ($all[$since..($all.Count-1)] -join "`n") }
function Copy-Log { if (Test-Path $logFile) { Copy-Item $logFile (Join-Path $share "proxy-lfm.log") -Force -ErrorAction SilentlyContinue } }

# curl probe (schannel; --ssl-revoke-best-effort because our leaf has no CRL/OCSP).
function Invoke-Probe($name, $method, $url, $bodyJson, $maxTime) {
    $bodyFile = Join-Path $env:TEMP ("req-" + [guid]::NewGuid().ToString("N") + ".json")
    $outFile  = Join-Path $share ($name + "-body.json")
    $a = @("-sS", "--ssl-revoke-best-effort", "-X", $method, $url,
           "-H", "anthropic-version: 2023-06-01", "-H", "content-type: application/json",
           "-H", ("x-api-key: " + $fakeKey), "-o", $outFile, "-w", "%{http_code}", "--max-time", "$maxTime")
    if ($bodyJson) { [System.IO.File]::WriteAllText($bodyFile, $bodyJson, (New-Object System.Text.UTF8Encoding($false))); $a += @("--data-binary", "@$bodyFile") }
    $code = (& curl.exe @a 2>$null)
    $body = if (Test-Path $outFile) { Get-Content -Raw $outFile -ErrorAction SilentlyContinue } else { "" }
    Remove-Item $bodyFile -ErrorAction SilentlyContinue
    Write-Host ("  curl {0}: http={1} bodylen={2}" -f $name, "$code".Trim(), "$body".Length)
    return [pscustomobject]@{ Code = "$code".Trim(); Body = "$body" }
}

# claude -p in an isolated child (own env) with a hard timeout.
function Invoke-Claude($name, $claudeExe, $prompt, $timeoutSec = 240) {
    $job = Start-Job -ScriptBlock {
        param($exe, $prompt, $key, $caCert)
        $env:ANTHROPIC_API_KEY = $key
        $env:NODE_EXTRA_CA_CERTS = $caCert
        Remove-Item Env:ANTHROPIC_BASE_URL -ErrorAction SilentlyContinue
        Remove-Item Env:HTTPS_PROXY -ErrorAction SilentlyContinue
        $env:DISABLE_TELEMETRY = "1"; $env:DISABLE_AUTOUPDATER = "1"; $env:CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC = "1"
        & $exe -p $prompt 2>&1 | Out-String
    } -ArgumentList $claudeExe, $prompt, $fakeKey, $caCert
    $done = Wait-Job $job -Timeout $timeoutSec
    if ($done) { $out = (Receive-Job $job | Out-String) } else { Stop-Job $job -ErrorAction SilentlyContinue; $out = "(TIMED OUT after ${timeoutSec}s)" }
    Remove-Job $job -Force -ErrorAction SilentlyContinue
    [System.IO.File]::WriteAllText((Join-Path $share "$name.txt"), $out, (New-Object System.Text.UTF8Encoding($false)))
    Write-Host ("  claude[{0}] outlen={1}" -f $name, $out.Length)
    return $out
}

$tlsErrRe = "(?i)self.?signed|unable to (verify|get)|UNABLE_TO|SELF_SIGNED|ERR_TLS|CERT_|certificate chain|local issuer"
$authRe   = "(?i)401|authentication|api[ _-]?key|external api key"
$sidecar  = $null

try {
    Write-Host "===== ENVIRONMENT ====="
    $isAdmin = ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltinRole]::Administrator)
    Add-Result "env.elevated" "True" $isAdmin $isAdmin
    Save-Results

    # ----- Install proxy (transparent, REAL LFM config) -----
    Write-Host "===== STAGE + INSTALL ====="
    New-Item -ItemType Directory -Force -Path "C:\work" | Out-Null
    Copy-Item -Recurse -Force $repoSrc $work
    $builtExe = Join-Path $work "test\sandbox\proxy.exe"
    if (Test-Path $builtExe) { Copy-Item $builtExe (Join-Path $work "proxy.exe") -Force }
    Set-Location $work
    try { & "$work\install.ps1" -SkipBuild *>&1 | ForEach-Object { Write-Host "  $_" } } catch { Write-Host "install raised: $($_.Exception.Message)" }

    # Keep inference.type=llama_cpp_http (real LFM). Loosen CPU-sensitive timeouts.
    $c = Get-Content -Raw $cfgFile
    $c = $c -replace '(?m)^(\s*classify_timeout_ms:\s*)\d+', '${1}30000'
    $c = $c -replace '(?m)^(\s*health_timeout_ms:\s*)\d+', '${1}5000'
    [System.IO.File]::WriteAllText($cfgFile, $c, (New-Object System.Text.UTF8Encoding($false)))
    Add-Result "install.ca_in_system_store" "CA in LocalMachine\Root" "" ([bool](Get-ChildItem Cert:\LocalMachine\Root -EA SilentlyContinue | Where-Object { $_.Subject -like "*PromptGate CA*" }))
    Save-Results

    # ----- CPU llama.cpp sidecar + LFM model (cached) -----
    Write-Host "===== LLAMA.CPP SIDECAR (CPU) ====="
    $llamaServer = Join-Path $llamaDir "llama-server.exe"
    if (-not (Test-Path $llamaServer)) {
        Write-Host "Downloading llama.cpp CPU release..."
        try {
            $rel = Invoke-RestMethod -Uri "https://api.github.com/repos/ggml-org/llama.cpp/releases/latest" -Headers @{ "User-Agent" = "foxco-sbx" }
            $asset = $rel.assets | Where-Object { $_.name -match "bin-win-cpu-x64\.zip$" } | Select-Object -First 1
            if (-not $asset) { $asset = $rel.assets | Where-Object { $_.name -match "bin-win-avx2-x64\.zip$" } | Select-Object -First 1 }
            Write-Host ("  asset: {0}" -f $asset.name)
            $zip = Join-Path $cache $asset.name
            Invoke-WebRequest -Uri $asset.browser_download_url -OutFile $zip -UseBasicParsing
            New-Item -ItemType Directory -Force -Path $llamaDir | Out-Null
            Expand-Archive -Path $zip -DestinationPath $llamaDir -Force
            # release zips may nest the exe in a subfolder; flatten the search.
            if (-not (Test-Path $llamaServer)) {
                $found = Get-ChildItem -Recurse -Path $llamaDir -Filter "llama-server.exe" -EA SilentlyContinue | Select-Object -First 1
                if ($found) { $llamaServer = $found.FullName }
            }
        } catch { Write-Host "llama.cpp download failed: $($_.Exception.Message)" }
    } else { Write-Host "Reusing cached llama.cpp: $llamaServer" }
    Add-Result "sidecar.llama_installed" "llama-server.exe present" $llamaServer (Test-Path $llamaServer)

    if (Test-Path $llamaServer) {
        # llama.cpp Windows builds need the VC++ runtime (winget normally pulls it as
        # a dependency; our manual unzip does not). Without it llama-server.exe fails
        # at the loader and exits with no output.
        $vc = Join-Path $cache "vc_redist.x64.exe"
        if (-not (Test-Path $vc)) {
            try { Invoke-WebRequest -Uri "https://aka.ms/vs/17/release/vc_redist.x64.exe" -OutFile $vc -UseBasicParsing } catch { Write-Host "  vc_redist download failed: $($_.Exception.Message)" }
        }
        if (Test-Path $vc) { Write-Host "  installing VC++ redistributable..."; Start-Process -FilePath $vc -ArgumentList "/install", "/quiet", "/norestart" -Wait }
        $ver = (& $llamaServer --version 2>&1 | Out-String).Trim()
        Write-Host ("  llama-server --version -> {0}" -f (Short $ver 120))
        Add-Result "sidecar.llama_runs" "llama-server.exe loads (version prints)" (Short $ver 120) ([bool]($ver -match "version|build|llama"))

        $env:LLAMA_CACHE = $modelCache
        # Prefer a host-prestaged local GGUF (llama.cpp's own -hf downloader fails TLS
        # verification in the bare sandbox: no CA bundle). -m bypasses that entirely.
        $localGguf = Join-Path $modelCache "LFM2.5-1.2B-Instruct-Q4_K_M.gguf"
        if (Test-Path $localGguf) {
            Write-Host "  using local model: $localGguf"
            $llArgs = @("-m", $localGguf, "--host", "127.0.0.1", "--port", "8791", "--jinja", "-ngl", "0")
        } else {
            Write-Host "  no local model; falling back to -hf (may fail SSL in sandbox)"
            $llArgs = @("-hf", $modelRef, "--host", "127.0.0.1", "--port", "8791", "--jinja", "-ngl", "0")
        }
        Write-Host ("  starting: llama-server " + ($llArgs -join ' '))
        $sidecar = Start-Process -FilePath $llamaServer -ArgumentList $llArgs -PassThru -WindowStyle Hidden `
            -RedirectStandardOutput (Join-Path $share "llama-server.out.log") -RedirectStandardError (Join-Path $share "llama-server.err.log")
        # First run downloads ~1GB then loads on CPU; be patient.
        $healthy = $false; $deadline = (Get-Date).AddSeconds(900)
        while ((Get-Date) -lt $deadline) {
            if ($sidecar.HasExited) { Write-Host "  llama-server exited early (code $($sidecar.ExitCode))"; break }
            try { $h = Invoke-WebRequest -Uri "http://127.0.0.1:8791/health" -UseBasicParsing -TimeoutSec 3; if ($h.StatusCode -eq 200) { $healthy = $true; break } } catch {}
            Start-Sleep -Seconds 5
        }
        Add-Result "sidecar.health" "llama /health 200" $healthy $healthy
    }
    Copy-Log; Save-Results

    # ----- Start proxy (uses the real LFM) -----
    Write-Host "===== START PROXY ====="
    try { & "$work\proxyctl.ps1" start *>&1 | ForEach-Object { Write-Host "  $_" } } catch { Write-Host "start raised: $($_.Exception.Message)" }
    $port443 = Wait-Port 443 30
    ipconfig /flushdns | Out-Null
    $addrs = @(); try { $addrs = [System.Net.Dns]::GetHostAddresses("api.anthropic.com") | ForEach-Object { $_.IPAddressToString } } catch {}
    Add-Result "start.port443_listening" "127.0.0.1:443 accepts" $port443 $port443
    Add-Result "start.redirect_resolves" "api.anthropic.com -> 127.0.0.1" ($addrs -join ",") ($addrs -contains "127.0.0.1")
    Copy-Log; Save-Results

    $benign = '{"model":"claude-3-5-sonnet-20241022","max_tokens":16,"messages":[{"role":"user","content":"What is the capital of France? Reply in one word."}]}'
    $secret = '{"model":"claude-3-5-sonnet-20241022","max_tokens":16,"messages":[{"role":"user","content":"Store this for me: my password is hunter2 and my database is postgres://admin:p@ssw0rd@10.0.0.5:5432/prod. Keep it secret."}]}'

    if ($healthy -and $port443) {
        # (1) curl benign -> real LFM ALLOW -> upstream 401
        Write-Host "===== curl benign (real LFM ALLOW) ====="
        $n = Log-LineCount; $cb = Invoke-Probe "lfm-curl-benign" "POST" "$apiBase/v1/messages" $benign 120
        Start-Sleep -Milliseconds 800; $tb = Log-Tail $n
        Add-Result "curl.lfm_benign_allow_401" "LFM ALLOW -> real 401" ("http=" + $cb.Code + " log=" + (Short $tb 80)) (($cb.Code -eq "401") -and ($tb -match "result=ALLOW"))

        # (2) curl secret in the LIVE turn -> real LFM (or rule) BLOCK, no egress
        Write-Host "===== curl secret (real LFM BLOCK) ====="
        $n = Log-LineCount; $cs = Invoke-Probe "lfm-curl-secret" "POST" "$apiBase/v1/messages" $secret 120
        Start-Sleep -Milliseconds 800; $ts = Log-Tail $n
        Add-Result "curl.lfm_secret_blocked" "HTTP 200 local block (not 401)" $cs.Code (($cs.Code -eq "200") -and ($cs.Code -ne "401"))
        Add-Result "curl.lfm_secret_block_log" "log: BLOCK (not classifier_unavailable)" (Short $ts 100) (($ts -match "result=BLOCK") -and ($ts -notmatch "classifier_unavailable"))
        Add-Result "curl.lfm_secret_block_body" "block notice (LOCAL_DLP_NOTE) returned" (Short $cs.Body 80) ($cs.Body -match "LOCAL_DLP_NOTE")
    } else {
        Add-Result "sidecar.usable" "sidecar healthy + proxy listening" ("healthy=$healthy port443=$port443") $false
    }

    # ----- Real Claude Code with the real LFM -----
    Write-Host "===== INSTALL CLAUDE CODE ====="
    $installOut = & powershell -NoProfile -ExecutionPolicy Bypass -Command "irm https://claude.ai/install.ps1 | iex" 2>&1 | Out-String
    [System.IO.File]::WriteAllText((Join-Path $share "install-claude.txt"), $installOut, (New-Object System.Text.UTF8Encoding($false)))
    $claudeExe = Join-Path $env:USERPROFILE ".local\bin\claude.exe"
    if (-not (Test-Path $claudeExe)) { $cand = Get-Command claude.exe -EA SilentlyContinue; if ($cand) { $claudeExe = $cand.Source } }
    $claudeOk = Test-Path $claudeExe
    Add-Result "claude.installed" "claude.exe present" $claudeExe $claudeOk
    Copy-Log; Save-Results

    if ($claudeOk -and $healthy -and $port443) {
        # (3) Claude Code benign -> with the REAL LFM it should NOT over-block (unlike
        #     keyword), so it ALLOWs and forwards -> upstream 401.
        Write-Host "===== claude benign (real LFM, expect ALLOW) ====="
        $n = Log-LineCount; $o1 = Invoke-Claude "lfm-claude-benign" $claudeExe "What is the capital of France? Reply in one word." 240
        Start-Sleep -Milliseconds 1200; $t1 = Log-Tail $n
        Add-Result "claude.lfm_benign_reached" "request reached proxy (CA trusted)" (Short $t1 80) ([bool]($t1 -match "result=(ALLOW|BLOCK|PASSTHROUGH)"))
        Add-Result "claude.lfm_benign_allow" "real LFM ALLOWed Claude Code's benign turn (no over-block)" (Short $t1 80) ([bool]($t1 -match "result=ALLOW"))
        Add-Result "claude.lfm_benign_no_tls_err" "no TLS/cert error" (Short $o1 100) (-not ($o1 -match $tlsErrRe))

        # (4) Claude Code with a secret -> observe how the real LFM handles it.
        Write-Host "===== claude secret (real LFM, observe) ====="
        $n = Log-LineCount; $o2 = Invoke-Claude "lfm-claude-secret" $claudeExe "Store this for me and remember it: my password is hunter2 and my database is postgres://admin:p@ssw0rd@10.0.0.5:5432/prod." 240
        Start-Sleep -Milliseconds 1200; $t2 = Log-Tail $n
        $secretBlocked = [bool]($t2 -match "result=BLOCK" -and $t2 -notmatch "classifier_unavailable")
        $secretSanitized = [bool]($t2 -match "sanitize removed_units")
        Add-Result "claude.lfm_secret_protected" "secret BLOCKED or SANITIZED (not plainly forwarded)" ("block=$secretBlocked sanitize=$secretSanitized log=" + (Short $t2 80)) ($secretBlocked -or $secretSanitized)
        Add-Result "claude.lfm_secret_client_block" "block notice reached Claude Code (if hard block)" (Short $o2 80) ([bool]($o2 -match "LOCAL_DLP_NOTE"))
    }

    # ----- Uninstall -----
    Write-Host "===== UNINSTALL ====="
    try { & "$work\proxyctl.ps1" stop *>&1 | Out-Null } catch {}
    try { & "$work\uninstall.ps1" *>&1 | ForEach-Object { Write-Host "  $_" } } catch { Write-Host "uninstall raised: $($_.Exception.Message)" }
    ipconfig /flushdns | Out-Null
    Add-Result "uninstall.service_removed" "service gone" "" (-not (Get-Service -Name PromptGate -EA SilentlyContinue))
    Add-Result "uninstall.ca_removed" "CA removed from store" "" (-not (Get-ChildItem Cert:\LocalMachine\Root -EA SilentlyContinue | Where-Object { $_.Subject -like "*PromptGate CA*" }))
    Save-Results
}
catch {
    Write-Host "FATAL: $($_.Exception.Message)"
    Write-Host $_.ScriptStackTrace
}
finally {
    if ($sidecar -and -not $sidecar.HasExited) { try { Stop-Process -Id $sidecar.Id -Force -EA SilentlyContinue } catch {} }
    Copy-Log
    Save-Results
    $passCount = (@($results | Where-Object { $_.pass })).Count
    $total = $results.Count
    Write-Host ("===== DONE (lfm): {0}/{1} checks passed =====" -f $passCount, $total)
    Stop-Transcript | Out-Null
    Set-Content -Path $donePath -Value ("lfm {0}/{1}" -f $passCount, $total) -Encoding ascii
}

# Auto-dispose the VM (results already on the host share). A cosmetic "Windows
# Sandbox" error dialog may flash as the guest shuts down - harmless.
Start-Sleep -Seconds 3
shutdown /s /t 0
