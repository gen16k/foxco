"use client";

import { useRouter } from "next/navigation";
import type { ReasonCount } from "@/lib/schemas";
import { EmptyState } from "@/components/common/states";

export function TopReasonsBars({ reasons }: { reasons: ReasonCount[] }) {
  const router = useRouter();
  if (!reasons.length) return <EmptyState message="No blocks in this range." />;
  const max = Math.max(...reasons.map((r) => r.count), 1);

  return (
    <ul className="space-y-2">
      {reasons.slice(0, 8).map((r) => (
        <li key={r.reason}>
          <button
            onClick={() => router.push(`/history?decision=BLOCK&q=${encodeURIComponent(r.reason)}`)}
            className="group block w-full text-left"
            title="クリックで該当ブロックの履歴を表示"
          >
            <div className="flex items-center justify-between gap-2 text-xs">
              <span className="truncate text-zinc-300 group-hover:text-zinc-100">{r.reason}</span>
              <span className="shrink-0 tabular-nums text-zinc-500">{r.count}</span>
            </div>
            <div className="mt-1 h-1.5 w-full overflow-hidden rounded bg-panelAlt">
              <div
                className="h-full rounded bg-block/70 transition-all group-hover:bg-block"
                style={{ width: `${(r.count / max) * 100}%` }}
              />
            </div>
          </button>
        </li>
      ))}
    </ul>
  );
}
