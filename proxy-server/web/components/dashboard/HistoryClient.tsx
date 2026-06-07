"use client";

import { useDashboardParams } from "@/lib/use-dashboard-params";
import { useEvents } from "@/lib/swr";
import { Panel } from "@/components/common/Panel";
import { PanelState } from "@/components/common/states";
import { EventFilters } from "@/components/history/EventFilters";
import { EventsTable } from "@/components/history/EventsTable";
import { EventDetailDrawer } from "@/components/history/EventDetailDrawer";
import { Pagination } from "@/components/common/Pagination";

export function HistoryClient() {
  const { sp, setParams } = useDashboardParams();
  const limit = parseInt(sp.get("limit") || "50", 10) || 50;
  const offset = parseInt(sp.get("offset") || "0", 10) || 0;

  const params = {
    range: sp.get("range"),
    from: sp.get("from"),
    to: sp.get("to"),
    decision: sp.get("decision"),
    source: sp.get("source"),
    q: sp.get("q"),
    limit,
    offset,
  };
  const { data, error, isLoading, mutate } = useEvents(params);

  return (
    <div className="space-y-4">
      <Panel title="Prompt history" subtitle="すべてのプロンプト（ALLOW / BLOCK）">
        <div className="mb-3">
          <EventFilters />
        </div>
        <PanelState
          loading={isLoading && !data}
          error={error}
          isEmpty={!!data && data.events.length === 0}
          onRetry={() => mutate()}
          emptyMessage="No events match these filters."
          skeleton={<div className="h-64 animate-pulse rounded bg-panelAlt" />}
        >
          {data && (
            <>
              <EventsTable
                events={data.events}
                onRowClick={(id) => setParams({ event: id }, { push: true })}
              />
              <div className="mt-3">
                <Pagination
                  total={data.total}
                  limit={limit}
                  offset={offset}
                  onPage={(o) => setParams({ offset: o ? String(o) : null })}
                />
              </div>
            </>
          )}
        </PanelState>
      </Panel>

      <EventDetailDrawer />
    </div>
  );
}
