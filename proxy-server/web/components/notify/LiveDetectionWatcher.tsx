"use client";

import { useEffect, useRef } from "react";
import { useRecentBlocks } from "@/lib/swr";
import { useNotifications } from "./NotificationContext";
import { useSound } from "@/components/shell/SoundContext";
import { playBeep } from "@/lib/sound";

// LiveDetectionWatcher renders nothing. It subscribes to the shared recent-block
// poll and fires a toast (and an optional beep) for each BLOCK not seen before.
// The first fetch only seeds the baseline so historical blocks don't flood the
// screen on load. `seen` is rebuilt from the current page each tick, which both
// dedupes and bounds memory (the list is newest-first and monotonic).
export function LiveDetectionWatcher() {
  const { data } = useRecentBlocks();
  const { notify } = useNotifications();
  const { enabled } = useSound();

  const seen = useRef<Set<string>>(new Set());
  const initialized = useRef(false);
  const soundOn = useRef(enabled);
  soundOn.current = enabled;

  useEffect(() => {
    const events = data?.events;
    if (!events) return;

    if (!initialized.current) {
      seen.current = new Set(events.map((e) => e.eventId));
      initialized.current = true;
      return;
    }

    const fresh = events.filter((e) => !seen.current.has(e.eventId));
    seen.current = new Set(events.map((e) => e.eventId));
    if (fresh.length === 0) return;

    // Notify oldest-first so the newest detection lands on top of the stack.
    for (let i = fresh.length - 1; i >= 0; i--) notify(fresh[i]);
    if (soundOn.current) playBeep();
  }, [data, notify]);

  return null;
}
