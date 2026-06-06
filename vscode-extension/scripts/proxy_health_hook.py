#!/usr/bin/env python3
"""
Claude Code UserPromptSubmit hook.
Checks whether the PromptGate Go proxy is running before each prompt.
If the proxy is down AND ANTHROPIC_BASE_URL points at it, warn the user.

This script does NOT block traffic itself — traffic interception is done by
the Go proxy via ANTHROPIC_BASE_URL=http://127.0.0.1:8787. This hook is
a UX safety net that warns when the guard is unexpectedly offline.
"""

import json
import os
import sys
import urllib.request

PROXY_URL = os.environ.get("ANTHROPIC_BASE_URL", "")
HEALTH_URL = "http://127.0.0.1:8787/admin/meta"
WARN_ONLY = os.environ.get("PROMPTGATE_WARN_ONLY", "1") == "1"


def proxy_is_routing() -> bool:
    """True if ANTHROPIC_BASE_URL is pointing at the local PromptGate proxy."""
    return "127.0.0.1:8787" in PROXY_URL or "localhost:8787" in PROXY_URL


def proxy_is_healthy() -> bool:
    req = urllib.request.Request(HEALTH_URL, method="GET")
    try:
        with urllib.request.urlopen(req, timeout=2) as resp:
            return resp.status == 200
    except Exception:
        return False


def main():
    raw = sys.stdin.read()
    try:
        data = json.loads(raw)
    except json.JSONDecodeError:
        sys.exit(0)

    if not data.get("prompt"):
        sys.exit(0)

    if not proxy_is_routing():
        print(
            "\n[PromptGate] 警告: ANTHROPIC_BASE_URL がプロキシを向いていません。\n"
            "ガードをバイパスしてCloudへ直接送信されます。\n"
            "PromptGateを有効にするには:\n"
            "  $env:ANTHROPIC_BASE_URL = \"http://127.0.0.1:8787\"\n"
            "  .\\proxy-server\\start.ps1",
            file=sys.stderr,
        )
        if not WARN_ONLY:
            sys.exit(1)
        sys.exit(0)

    if not proxy_is_healthy():
        print(
            "\n[PromptGate] 警告: プロキシ (127.0.0.1:8787) が応答しません。\n"
            "PromptGateを起動してください:\n"
            "  cd proxy-server && .\\start.ps1\n"
            "または VSCode の PromptGate サイドバーから [サーバ起動] を押してください。",
            file=sys.stderr,
        )
        if not WARN_ONLY:
            sys.exit(1)
        sys.exit(0)

    sys.exit(0)


if __name__ == "__main__":
    main()
