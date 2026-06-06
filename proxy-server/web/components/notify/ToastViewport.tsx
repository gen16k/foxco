"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { useNotifications, type Toast } from "./NotificationContext";
import { fmtRelative } from "@/lib/format";

const VISIBLE = 4;
const AUTO_DISMISS_MS = 8000;

// ToastViewport is the global, top-right alert stack. It shows the most recent
// few detections; older ones are aggregated into a "+N" chip. It mounts once in
// the dashboard layout so toasts appear on every page.
export function ToastViewport() {
  const { toasts, dismiss } = useNotifications();

  // Escape dismisses the newest toast.
  useEffect(() => {
    if (!toasts.length) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") dismiss(toasts[toasts.length - 1].id);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [toasts, dismiss]);

  if (!toasts.length) return null;
  const shown = toasts.slice(-VISIBLE);
  const hidden = toasts.length - shown.length;

  return (
    <div className="pointer-events-none fixed right-4 top-16 z-40 flex w-[22rem] max-w-[calc(100vw-2rem)] flex-col gap-2">
      {hidden > 0 && (
        <div className="self-end rounded bg-panelAlt px-2 py-0.5 text-2xs text-zinc-400">
          ＋{hidden} 件の検知
        </div>
      )}
      {shown.map((t) => (
        <ToastCard key={t.id} toast={t} onDismiss={() => dismiss(t.id)} />
      ))}
    </div>
  );
}

function ToastCard({ toast, onDismiss }: { toast: Toast; onDismiss: () => void }) {
  const router = useRouter();
  const [paused, setPaused] = useState(false);
  const { event } = toast;
  const detail = event.matchedSnippet || event.promptText || "";

  // Auto-dismiss after a delay, paused while hovered.
  useEffect(() => {
    if (paused) return;
    const id = setTimeout(onDismiss, AUTO_DISMISS_MS);
    return () => clearTimeout(id);
  }, [paused, onDismiss]);

  const open = () => router.push(`/history?event=${encodeURIComponent(event.eventId)}`);

  return (
    <div
      onMouseEnter={() => setPaused(true)}
      onMouseLeave={() => setPaused(false)}
      className="pointer-events-auto animate-toast-in overflow-hidden rounded-lg border border-block/60 bg-panel shadow-2xl motion-reduce:animate-none"
    >
      <div className="flex items-start gap-2 border-l-2 border-block px-3 py-2.5">
        <span aria-hidden className="mt-0.5 text-sm">
          ⛔
        </span>
        <button onClick={open} className="min-w-0 flex-1 text-left">
          <div className="flex items-center gap-2">
            <span className="text-xs font-semibold text-block">送信をブロックしました</span>
            <span className="text-2xs uppercase tracking-wide text-zinc-400">{event.source || "—"}</span>
          </div>
          <p className="mt-0.5 truncate text-xs text-zinc-300">{event.reason || "blocked"}</p>
          {detail && (
            <p className="mt-1 truncate rounded bg-block/10 px-1.5 py-0.5 font-mono text-2xs text-block">
              {detail}
            </p>
          )}
          <p className="mt-1 text-2xs text-zinc-400">{fmtRelative(event.createdAt)} · クリックで詳細 ↗</p>
        </button>
        <button
          onClick={onDismiss}
          aria-label="閉じる"
          className="rounded px-1 text-zinc-400 hover:bg-panelAlt hover:text-zinc-300"
        >
          ✕
        </button>
      </div>
    </div>
  );
}
