"use client";

import { useRouter } from "next/navigation";
import { useRecentBlocks } from "@/lib/swr";
import { Panel } from "@/components/common/Panel";
import { PanelState } from "@/components/common/states";
import { EventsTable } from "@/components/history/EventsTable";

const MAX_ROWS = 8;

// LiveDetectionsPanel is the always-visible feed of recent blocks on the
// Overview. It shares the recent-block poll with the toast watcher (one request)
// and renders the SAME table as Recent events for a consistent, low-load layout.
// The detected string itself is intentionally not shown here — it lives in the
// detail drawer (and the Recent events row's body indicator), reachable by click.
export function LiveDetectionsPanel() {
  const router = useRouter();
  const { data, error, isLoading, mutate } = useRecentBlocks();
  const events = (data?.events ?? []).slice(0, MAX_ROWS);

  return (
    <Panel
      title="Live detections"
      subtitle="直近のブロック（自動更新）"
      right={
        <span className="flex items-center gap-1.5 text-[11px] text-zinc-500">
          <span className="relative flex h-2 w-2">
            <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-block opacity-60 motion-reduce:hidden" />
            <span className="relative inline-flex h-2 w-2 rounded-full bg-block" />
          </span>
          live
        </span>
      }
    >
      <PanelState
        loading={isLoading && !data}
        error={error}
        isEmpty={!!data && events.length === 0}
        onRetry={() => mutate()}
        emptyMessage="まだ検知はありません。"
        skeleton={<div className="h-32 animate-pulse rounded bg-panelAlt" />}
      >
        <EventsTable compact events={events} onRowClick={(id) => router.push(`/history?event=${id}`)} />
      </PanelState>
    </Panel>
  );
}
