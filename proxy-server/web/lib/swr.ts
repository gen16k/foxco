"use client";

import useSWR from "swr";
import { useContext } from "react";
import { RefreshContext } from "@/components/shell/RefreshContext";
import { resolveRange, type RangeParams } from "@/lib/time-range";
import type { Stats, EventPage, EventRow, Meta } from "@/lib/schemas";

export interface ApiFetchError extends Error {
  status?: number;
  code?: string;
}

async function jsonFetcher(url: string): Promise<unknown> {
  const res = await fetch(url, { cache: "no-store" });
  if (!res.ok) {
    const body = (await res.json().catch(() => ({}))) as { message?: string; error?: string };
    const err = new Error(body.message || `request failed (${res.status})`) as ApiFetchError;
    err.status = res.status;
    err.code = body.error;
    throw err;
  }
  return res.json();
}

export interface EventQueryParams extends RangeParams {
  decision?: string | null;
  source?: string | null;
  q?: string | null;
  limit?: number;
  offset?: number;
}

export function useStats(params: RangeParams) {
  const { intervalMs } = useContext(RefreshContext);
  const key = ["stats", params.range ?? "", params.from ?? "", params.to ?? ""];
  return useSWR<Stats>(
    key,
    async () => {
      const { from, to } = resolveRange(params);
      const qs = new URLSearchParams({ from, to });
      return jsonFetcher(`/api/admin/stats?${qs.toString()}`) as Promise<Stats>;
    },
    { refreshInterval: intervalMs, keepPreviousData: true, revalidateOnFocus: true, dedupingInterval: 2000 },
  );
}

export function useEvents(params: EventQueryParams) {
  const { intervalMs } = useContext(RefreshContext);
  const key = [
    "events",
    params.range ?? "",
    params.from ?? "",
    params.to ?? "",
    params.decision ?? "",
    params.source ?? "",
    params.q ?? "",
    params.limit ?? 50,
    params.offset ?? 0,
  ];
  return useSWR<EventPage>(
    key,
    async () => {
      const { from, to } = resolveRange(params);
      const qs = new URLSearchParams({ from, to });
      if (params.decision) qs.set("decision", params.decision);
      if (params.source) qs.set("source", params.source);
      if (params.q) qs.set("q", params.q);
      qs.set("limit", String(params.limit ?? 50));
      qs.set("offset", String(params.offset ?? 0));
      return jsonFetcher(`/api/admin/events?${qs.toString()}`) as Promise<EventPage>;
    },
    { refreshInterval: intervalMs, keepPreviousData: true, revalidateOnFocus: true, dedupingInterval: 2000 },
  );
}

export function useEventDetail(id: string | null) {
  return useSWR<EventRow>(
    id ? ["event", id] : null,
    async () => jsonFetcher(`/api/admin/events/${encodeURIComponent(id as string)}`) as Promise<EventRow>,
  );
}

// useRecentBlocks polls the newest BLOCK events on a fixed 5s cadence,
// independent of the dashboard time range and the manual refresh interval, so
// the live-detection alert (toasts + Live detections panel) fires even when
// auto-refresh is "off". The watcher and the panel share this SWR key, so they
// drive a single request, not two.
export function useRecentBlocks(limit = 20) {
  return useSWR<EventPage>(
    ["recent-blocks", limit],
    async () => {
      const qs = new URLSearchParams({ decision: "BLOCK", limit: String(limit) });
      return jsonFetcher(`/api/admin/events?${qs.toString()}`) as Promise<EventPage>;
    },
    { refreshInterval: 5000, keepPreviousData: true, revalidateOnFocus: true, dedupingInterval: 2000 },
  );
}

// useRecentEvents polls the newest events of ALL decisions on a fixed 3s cadence,
// independent of the dashboard time range / manual refresh interval. The Network
// Flow tab diffs successive pages (seen-set) to spawn one packet per new event —
// green for anything that reached upstream, red for a BLOCK. No from/to is sent,
// so the BFF returns the latest page (and, in mock mode, the rolling live block).
export function useRecentEvents(limit = 30) {
  return useSWR<EventPage>(
    ["recent-events", limit],
    async () => {
      const qs = new URLSearchParams({ limit: String(limit) });
      return jsonFetcher(`/api/admin/events?${qs.toString()}`) as Promise<EventPage>;
    },
    { refreshInterval: 3000, keepPreviousData: true, revalidateOnFocus: true, dedupingInterval: 1500 },
  );
}

export function useMeta() {
  return useSWR<Meta>("meta", async () => jsonFetcher("/api/admin/meta") as Promise<Meta>, {
    refreshInterval: 15000,
    revalidateOnFocus: true,
  });
}
