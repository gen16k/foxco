"use client";

import {
  AreaChart,
  Area,
  XAxis,
  YAxis,
  Tooltip,
  CartesianGrid,
  ResponsiveContainer,
  Legend,
} from "recharts";
import type { SeriesPoint } from "@/lib/schemas";
import { fmtAxis } from "@/lib/format";

const tooltipStyle = {
  background: "#181b1f",
  border: "1px solid #2a2f36",
  borderRadius: 6,
  fontSize: 12,
};

export function AllowBlockArea({ series, spanMs }: { series: SeriesPoint[]; spanMs: number }) {
  const data = series.map((s) => ({ ...s, label: fmtAxis(s.ts, spanMs) }));
  return (
    <div className="h-64 w-full">
      <ResponsiveContainer width="100%" height="100%">
        <AreaChart data={data} margin={{ top: 8, right: 8, left: -18, bottom: 0 }}>
          <CartesianGrid strokeDasharray="3 3" stroke="#23282e" vertical={false} />
          <XAxis
            dataKey="label"
            tick={{ fill: "#71717a", fontSize: 11 }}
            stroke="#2a2f36"
            minTickGap={28}
          />
          <YAxis tick={{ fill: "#71717a", fontSize: 11 }} stroke="#2a2f36" allowDecimals={false} width={42} />
          <Tooltip contentStyle={tooltipStyle} labelStyle={{ color: "#a1a1aa" }} />
          <Legend wrapperStyle={{ fontSize: 12 }} />
          <Area type="monotone" dataKey="allow" stackId="1" stroke="#3fb950" fill="#3fb95033" name="ALLOW" />
          <Area type="monotone" dataKey="block" stackId="1" stroke="#f85149" fill="#f8514944" name="BLOCK" />
        </AreaChart>
      </ResponsiveContainer>
    </div>
  );
}
