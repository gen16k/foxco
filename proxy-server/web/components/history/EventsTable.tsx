"use client";

import type { EventRow } from "@/lib/schemas";
import { DecisionBadge } from "@/components/common/DecisionBadge";
import { EmptyState } from "@/components/common/states";
import { fmtClock, fmtLocalDateTime, fmtLatency } from "@/lib/format";

export function EventsTable({
  events,
  onRowClick,
  compact,
}: {
  events: EventRow[];
  onRowClick: (id: string) => void;
  compact?: boolean;
}) {
  if (!events.length) return <EmptyState message="No events match these filters." />;

  return (
    <div className="overflow-x-auto">
      <table className="w-full border-collapse text-sm">
        <thead>
          <tr className="border-b border-edge text-left text-xs text-zinc-400">
            <th className="py-2 pr-3 font-medium">Time</th>
            <th className="py-2 pr-3 font-medium">Decision</th>
            <th className="py-2 pr-3 font-medium">Source</th>
            <th className="py-2 pr-3 font-medium">Reason</th>
            <th className="py-2 pr-3 text-right font-medium">Latency</th>
            {!compact && <th className="py-2 pr-3 font-medium">Model</th>}
            <th className="py-2 font-medium">Body</th>
          </tr>
        </thead>
        <tbody>
          {events.map((e) => (
            <tr
              key={e.eventId}
              onClick={() => onRowClick(e.eventId)}
              className="cursor-pointer border-b border-edge/40 hover:bg-panelAlt/60"
            >
              <td className="whitespace-nowrap py-2 pr-3 text-zinc-400" title={e.createdAt}>
                {compact ? fmtClock(e.createdAt) : fmtLocalDateTime(e.createdAt)}
              </td>
              <td className="py-2 pr-3">
                <DecisionBadge decision={e.decision} />
              </td>
              <td className="py-2 pr-3 text-zinc-400">{e.source || "—"}</td>
              <td className="max-w-[320px] truncate py-2 pr-3 text-zinc-300">
                {e.reason || (e.decision === "ALLOW" ? "allowed" : "—")}
              </td>
              <td className="py-2 pr-3 text-right tabular-nums text-zinc-400">{fmtLatency(e.latencyMs)}</td>
              {!compact && <td className="py-2 pr-3 text-zinc-400">{e.modelName || "—"}</td>}
              <td className="py-2 text-zinc-400" title={e.promptText ? "prompt stored" : "no prompt stored"}>
                {e.promptText ? "📄" : "—"}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
