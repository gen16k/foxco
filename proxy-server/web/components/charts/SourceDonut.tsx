"use client";

import { PieChart, Pie, Cell, ResponsiveContainer, Tooltip } from "recharts";
import { useRouter } from "next/navigation";
import { EmptyState } from "@/components/common/states";

const COLORS: Record<string, string> = {
  rule: "#f85149",
  lfm: "#d29922",
  classifier_unavailable: "#bb8009",
  proxy: "#8957e5",
  sanitizer: "#3d8bfd",
  other: "#6e7681",
};

const tooltipStyle = {
  background: "#181b1f",
  border: "1px solid #2a2f36",
  borderRadius: 6,
  fontSize: 12,
};

export function SourceDonut({ bySource }: { bySource: Record<string, number> }) {
  const router = useRouter();
  const data = Object.entries(bySource)
    .map(([name, value]) => ({ name, value }))
    .sort((a, b) => b.value - a.value);

  if (data.length === 0) return <EmptyState message="No blocks in this range." />;

  const total = data.reduce((a, b) => a + b.value, 0);

  return (
    <div>
      <div className="relative h-52">
        <ResponsiveContainer width="100%" height="100%">
          <PieChart>
            <Pie
              data={data}
              dataKey="value"
              nameKey="name"
              innerRadius="60%"
              outerRadius="85%"
              paddingAngle={2}
              stroke="none"
              onClick={(d: { name?: string }) =>
                d?.name && router.push(`/history?decision=BLOCK&source=${encodeURIComponent(d.name)}`)
              }
            >
              {data.map((d) => (
                <Cell key={d.name} fill={COLORS[d.name] ?? "#6e7681"} className="cursor-pointer outline-none" />
              ))}
            </Pie>
            <Tooltip contentStyle={tooltipStyle} />
          </PieChart>
        </ResponsiveContainer>
        <div className="pointer-events-none absolute inset-0 flex flex-col items-center justify-center">
          <span className="text-2xl font-semibold text-zinc-100">{total}</span>
          <span className="text-xs text-zinc-500">blocks</span>
        </div>
      </div>
      <ul className="mt-2 flex flex-wrap gap-x-3 gap-y-1 text-xs">
        {data.map((d) => (
          <li key={d.name} className="flex items-center gap-1.5 text-zinc-400">
            <span className="h-2 w-2 rounded-full" style={{ background: COLORS[d.name] ?? "#6e7681" }} />
            {d.name} <span className="tabular-nums text-zinc-500">({d.value})</span>
          </li>
        ))}
      </ul>
      <p className="mt-2 text-[11px] text-zinc-600">スライスをクリックで履歴を絞り込み</p>
    </div>
  );
}
