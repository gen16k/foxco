import type { Stats } from "@/lib/schemas";
import { fmtNumber, fmtPercent, fmtLatency } from "@/lib/format";
import { StatCard } from "./StatCard";
import { Skeleton } from "@/components/common/states";

export function KpiRow({ stats, loading }: { stats?: Stats; loading: boolean }) {
  if (loading && !stats) {
    return (
      <div className="grid grid-cols-2 gap-3 md:grid-cols-4 xl:grid-cols-7">
        {Array.from({ length: 7 }).map((_, i) => (
          <Skeleton key={i} className="h-[76px]" />
        ))}
      </div>
    );
  }
  const s = stats ?? {
    total: 0,
    blocked: 0,
    allowed: 0,
    blockRate: 0,
    avgLatencyMs: 0,
    p95LatencyMs: 0,
    upstreamCalled: 0,
    bySource: {},
    topReasons: [],
    series: [],
  };
  const highBlock = s.blockRate >= 0.25;
  return (
    <div className="grid grid-cols-2 gap-3 md:grid-cols-4 xl:grid-cols-7">
      <StatCard label="Total requests" value={fmtNumber(s.total)} />
      <StatCard label="Blocked" value={fmtNumber(s.blocked)} accent="text-block" />
      <StatCard
        label="Block rate"
        value={fmtPercent(s.blockRate)}
        accent={highBlock ? "text-block" : "text-zinc-100"}
      />
      <StatCard label="Allowed" value={fmtNumber(s.allowed)} accent="text-allow" />
      <StatCard label="Avg latency" value={fmtLatency(s.avgLatencyMs)} />
      <StatCard label="p95 latency" value={fmtLatency(s.p95LatencyMs)} />
      <StatCard
        label="Upstream egress"
        value={fmtNumber(s.upstreamCalled)}
        hint="forwarded to Anthropic"
      />
    </div>
  );
}
