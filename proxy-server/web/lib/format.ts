export function fmtNumber(n: number): string {
  return new Intl.NumberFormat().format(Math.round(n || 0));
}

export function fmtPercent(ratio: number): string {
  return `${((ratio || 0) * 100).toFixed(1)}%`;
}

export function fmtLatency(ms: number): string {
  if (!isFinite(ms) || ms <= 0) return "—";
  if (ms < 1000) return `${Math.round(ms)} ms`;
  return `${(ms / 1000).toFixed(2)} s`;
}

export function fmtLocalDateTime(iso: string): string {
  const d = new Date(iso);
  if (isNaN(d.getTime())) return iso;
  return d.toLocaleString();
}

// Compact relative age: "just now", "12s", "3m", "1h", "2d". Used by the live
// detection feed/toasts, which re-render on each poll so the value stays fresh.
export function fmtRelative(iso: string, nowMs: number = Date.now()): string {
  const t = new Date(iso).getTime();
  if (isNaN(t)) return iso;
  const s = Math.max(0, Math.round((nowMs - t) / 1000));
  if (s < 5) return "just now";
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h`;
  return `${Math.floor(h / 24)}d`;
}

export function fmtClock(iso: string): string {
  const d = new Date(iso);
  if (isNaN(d.getTime())) return iso;
  return d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit" });
}

// Axis tick label for a bucket timestamp, granularity-aware.
export function fmtAxis(iso: string, spanMs: number): string {
  const d = new Date(iso);
  if (isNaN(d.getTime())) return iso;
  if (spanMs <= 48 * 3600 * 1000) {
    return d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
  }
  return d.toLocaleDateString([], { month: "short", day: "numeric" });
}
