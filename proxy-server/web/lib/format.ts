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
