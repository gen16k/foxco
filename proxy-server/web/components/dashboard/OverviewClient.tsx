"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useDashboardParams } from "@/lib/use-dashboard-params";
import { useStats, useEvents } from "@/lib/swr";
import { resolveRange } from "@/lib/time-range";
import { Panel } from "@/components/common/Panel";
import { PanelState } from "@/components/common/states";
import { KpiRow } from "@/components/panels/KpiRow";
import { AllowBlockArea } from "@/components/charts/AllowBlockArea";
import { SourceDonut } from "@/components/charts/SourceDonut";
import { TopReasonsBars } from "@/components/charts/TopReasonsBars";
import { EventsTable } from "@/components/history/EventsTable";

export function OverviewClient() {
  const router = useRouter();
  const { sp } = useDashboardParams();
  const rangeParams = { range: sp.get("range"), from: sp.get("from"), to: sp.get("to") };
  const spanMs = resolveRange(rangeParams).spanMs;

  const { data, error, isLoading, mutate } = useStats(rangeParams);
  const recent = useEvents({ ...rangeParams, limit: 12 });

  return (
    <div className="space-y-4">
      <KpiRow stats={data} loading={isLoading && !data} />

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-3">
        <Panel title="Requests over time" subtitle="ALLOW vs BLOCK" className="lg:col-span-2">
          <PanelState
            loading={isLoading && !data}
            error={error}
            isEmpty={!!data && data.series.length === 0}
            onRetry={() => mutate()}
            skeleton={<div className="h-64 animate-pulse rounded bg-panelAlt" />}
            emptyMessage="No requests in this range."
          >
            {data && <AllowBlockArea series={data.series} spanMs={spanMs} />}
          </PanelState>
        </Panel>

        <Panel title="Blocks by source">
          <PanelState
            loading={isLoading && !data}
            error={error}
            isEmpty={false}
            onRetry={() => mutate()}
            skeleton={<div className="h-64 animate-pulse rounded bg-panelAlt" />}
          >
            {data && <SourceDonut bySource={data.bySource} />}
          </PanelState>
        </Panel>
      </div>

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-3">
        <Panel title="Top block reasons">
          <PanelState
            loading={isLoading && !data}
            error={error}
            isEmpty={false}
            onRetry={() => mutate()}
            skeleton={<div className="h-40 animate-pulse rounded bg-panelAlt" />}
          >
            {data && <TopReasonsBars reasons={data.topReasons} />}
          </PanelState>
        </Panel>

        <Panel
          title="Recent events"
          className="lg:col-span-2"
          right={
            <Link href="/history" className="text-xs text-accent hover:underline">
              View all →
            </Link>
          }
        >
          <PanelState
            loading={recent.isLoading && !recent.data}
            error={recent.error}
            isEmpty={!!recent.data && recent.data.events.length === 0}
            onRetry={() => recent.mutate()}
            emptyMessage="No events yet — send traffic through the proxy."
            skeleton={<div className="h-40 animate-pulse rounded bg-panelAlt" />}
          >
            {recent.data && (
              <EventsTable
                compact
                events={recent.data.events}
                onRowClick={(id) => router.push(`/history?event=${id}`)}
              />
            )}
          </PanelState>
        </Panel>
      </div>
    </div>
  );
}
