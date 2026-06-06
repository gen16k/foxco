"use client";

import { useSound } from "./SoundContext";

// SoundToggle flips the opt-in alert sound. It lives in the TopBar (shell) so it
// is reachable from every dashboard page, since toasts are global.
export function SoundToggle() {
  const { enabled, toggle } = useSound();
  return (
    <button
      onClick={toggle}
      aria-pressed={enabled}
      title={enabled ? "通知音: ON（クリックでミュート）" : "通知音: OFF（クリックで有効化）"}
      className="rounded-md border border-edge px-2 py-1.5 text-sm text-zinc-400 hover:bg-panelAlt"
    >
      <span aria-hidden>{enabled ? "🔔" : "🔕"}</span>
      <span className="sr-only">通知音 {enabled ? "ON" : "OFF"}</span>
    </button>
  );
}
