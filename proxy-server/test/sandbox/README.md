# Windows Sandbox integration-test harness

End-to-end verification of **transparent HTTPS interception** in a disposable,
NAT-isolated **Windows Sandbox** VM, so it never touches the host's hosts file,
trust store, port 443, or any live Claude session. All dangerous mutations
(`install.ps1` → CA into `LocalMachine\Root`, hosts redirect, `:443`) happen only
inside the throwaway VM; the host repo is mapped **read-only**.

## How it works

`run-sandbox.ps1` (host) builds `proxy.exe`, prepares a results share + a persistent
cache, generates a `.wsb` (paths derived from its own location — nothing
machine-specific is committed), and launches the sandbox. The VM auto-runs the
chosen in-VM runner at logon, which writes `results-*.json` + a `DONE` sentinel to
the host-visible share, then shuts the VM down.

```
.\run-sandbox.ps1 -SkipBuild                                  # default: run-tests.ps1 (curl matrix)
.\run-sandbox.ps1 -SkipBuild -Runner run-claude-tests.ps1     # real Claude Code + keyword classifier
.\run-sandbox.ps1 -SkipBuild -Runner run-claude-lfm-tests.ps1 # real Claude Code + real LFM (CPU llama.cpp)
```

- Share (results): `%TEMP%\foxco-dlp-sandbox-share` — `results-*.json`, `transcript-*.txt`, response bodies, `proxy-*.log`, `DONE`.
- Cache (persistent, reused across runs): `%LOCALAPPDATA%\foxco-sbx-cache` — llama.cpp build, `vc_redist.x64.exe`, the GGUF model.

> **Launch must come from an interactive desktop.** Windows Sandbox is a GUI app;
> launching it from a non-foreground/background context silently fails to bring the
> VM up. Run `run-sandbox.ps1` from a normal terminal on the (RDP) desktop.

## Runners

| Runner | What it verifies | Notes |
|--------|------------------|-------|
| `run-tests.ps1` | Core mechanism via `curl.exe`: install → CA trust → hosts redirect → `:443` TLS → DLP (warming / ALLOW / BLOCK) → `1.1.1.1` bypass reaching the REAL api.anthropic.com (benign 401) → passthrough audit → full uninstall. | 32/32. Uses the keyword classifier (no GPU). |
| `run-claude-tests.ps1` | Real Claude Code CLI is transparently intercepted; CA trusted via the Windows system store **and** `NODE_EXTRA_CA_CERTS`. | keyword classifier over-triggers on Claude Code's own system prompt → sanitize path (use the LFM runner for representative BLOCK). |
| `run-claude-lfm-tests.ps1` | Real LFM classifier: curl secret → hard BLOCK (`source=lfm`); curl/Claude benign → ALLOW; real Claude Code secret → sanitized (no egress). | Downloads a CPU llama.cpp + the LFM2.5-1.2B GGUF (cached). CPU inference is slow. |

## Findings (see `docs/records/20260606/`)

- Claude Code (≥2.1.167) trusts the **Windows system store** by default
  (`CLAUDE_CODE_CERT_STORE="bundled,system"`), so `install.ps1`'s CA install is
  sufficient — no `NODE_EXTRA_CA_CERTS` required (it also works if set).
- Our minted leaf/CA carry no CRL/OCSP, so schannel clients (Windows `curl`
  default) need `--ssl-revoke-best-effort`; Node/Claude Code does not check
  revocation by default.
- llama.cpp on bare Windows needs the **VC++ redistributable**, and its `-hf`
  downloader fails TLS verification in the sandbox (no CA bundle) — pre-stage the
  GGUF host-side and start with `-m <local>`.
- A secret in a single live turn → hard BLOCK; a secret embedded in Claude Code's
  richer request → sanitize (unit removed). Both prevent egress.
