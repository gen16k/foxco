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

export function useMeta() {
  return useSWR<Meta>("meta", async () => jsonFetcher("/api/admin/meta") as Promise<Meta>, {
    refreshInterval: 15000,
    revalidateOnFocus: true,
  });
}
