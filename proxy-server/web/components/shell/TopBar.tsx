"use client";

import { useRouter } from "next/navigation";
import { NavTabs } from "./NavTabs";
import { ProxyStatus } from "./ProxyStatus";
import { TimeRangePicker } from "./TimeRangePicker";
import { RefreshControl } from "./RefreshControl";
import { SoundToggle } from "./SoundToggle";

export function TopBar() {
  const router = useRouter();

  async function logout() {
    await fetch("/api/auth/logout", { method: "POST" });
    router.replace("/login");
    router.refresh();
  }

  return (
    <header className="sticky top-0 z-10 border-b border-edge bg-ink/95 backdrop-blur">
      <div className="flex flex-wrap items-center justify-between gap-3 px-4 py-2.5">
        <div className="flex items-center gap-4">
          <div className="flex items-center gap-2">
            <span className="text-lg" aria-hidden>
              🦊
            </span>
            <span className="font-semibold text-zinc-100">PromptGate</span>
          </div>
          <NavTabs />
        </div>
        <div className="flex items-center gap-3">
          <ProxyStatus />
          <TimeRangePicker />
          <RefreshControl />
          <SoundToggle />
          <button
            onClick={logout}
            className="rounded-md border border-edge px-2.5 py-1.5 text-sm text-zinc-400 hover:bg-panelAlt"
          >
            ログアウト
          </button>
        </div>
      </div>
    </header>
  );
}
