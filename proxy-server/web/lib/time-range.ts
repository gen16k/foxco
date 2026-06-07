export type RangeKey = "15m" | "1h" | "6h" | "24h" | "7d" | "30d";

export const RANGE_PRESETS: { key: RangeKey; label: string; ms: number }[] = [
  { key: "15m", label: "Last 15m", ms: 15 * 60 * 1000 },
  { key: "1h", label: "Last 1h", ms: 60 * 60 * 1000 },
  { key: "6h", label: "Last 6h", ms: 6 * 60 * 60 * 1000 },
  { key: "24h", label: "Last 24h", ms: 24 * 60 * 60 * 1000 },
  { key: "7d", label: "Last 7d", ms: 7 * 24 * 60 * 60 * 1000 },
  { key: "30d", label: "Last 30d", ms: 30 * 24 * 60 * 60 * 1000 },
];

export const DEFAULT_RANGE: RangeKey = "24h";

export const REFRESH_OPTIONS: { key: string; label: string; ms: number }[] = [
  { key: "off", label: "Off", ms: 0 },
  { key: "5s", label: "5s", ms: 5000 },
  { key: "10s", label: "10s", ms: 10000 },
  { key: "30s", label: "30s", ms: 30000 },
  { key: "1m", label: "1m", ms: 60000 },
];

export interface RangeParams {
  range?: string | null;
  from?: string | null;
  to?: string | null;
}

export interface ResolvedRange {
  from: string;
  to: string;
  spanMs: number;
}

// RFC3339 without fractional seconds, matching how the Go proxy stores
// created_at (so string range comparisons line up cleanly).
function rfc3339(ms: number): string {
  return new Date(ms).toISOString().replace(/\.\d{3}Z$/, "Z");
}

// resolveRange converts URL params into absolute RFC3339 from/to. For a relative
// preset, `to` is "now" at call time so polling slides the window. A custom
// from/to (already absolute) is used verbatim.
export function resolveRange(p: RangeParams): ResolvedRange {
  if (p.from && p.to) {
    const span = Math.max(0, Date.parse(p.to) - Date.parse(p.from));
    return { from: p.from, to: p.to, spanMs: span };
  }
  const key = (p.range as RangeKey) || DEFAULT_RANGE;
  const preset = RANGE_PRESETS.find((x) => x.key === key) ?? RANGE_PRESETS[3];
  const now = Date.now();
  return { from: rfc3339(now - preset.ms), to: rfc3339(now), spanMs: preset.ms };
}

// localInputToRFC3339 converts a <input type="datetime-local"> value (local wall
// clock, no zone) into an absolute RFC3339 UTC instant.
export function localInputToRFC3339(local: string): string {
  const ms = Date.parse(local);
  if (isNaN(ms)) return "";
  return rfc3339(ms);
}

// rfc3339ToLocalInput formats an absolute instant back into a datetime-local value.
export function rfc3339ToLocalInput(iso: string): string {
  const d = new Date(iso);
  if (isNaN(d.getTime())) return "";
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

export function activeRangeLabel(p: RangeParams): string {
  if (p.from && p.to) return "Custom";
  const key = (p.range as RangeKey) || DEFAULT_RANGE;
  return RANGE_PRESETS.find((x) => x.key === key)?.label ?? "Last 24h";
}
