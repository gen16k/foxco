"use client";

import { useContext, useEffect } from "react";
import { useSWRConfig } from "swr";
import { REFRESH_OPTIONS } from "@/lib/time-range";
import { RefreshContext } from "./RefreshContext";
import { useDashboardParams } from "@/lib/use-dashboard-params";

export function RefreshControl() {
  const { setIntervalMs } = useContext(RefreshContext);
  const { sp, setParams } = useDashboardParams();
  const { mutate } = useSWRConfig();
  const current = sp.get("refresh") || "off";

  // Drive the shared polling interval from the URL so it's shareable/restorable.
  useEffect(() => {
    const opt = REFRESH_OPTIONS.find((o) => o.key === current) ?? REFRESH_OPTIONS[0];
    setIntervalMs(opt.ms);
  }, [current, setIntervalMs]);

  return (
    <div className="flex items-center gap-1">
      <button
        onClick={() => mutate(() => true)}
        title="Refresh now"
        className="rounded-md border border-edge bg-panel px-2.5 py-1.5 text-sm text-zinc-300 hover:bg-panelAlt"
      >
        ⟳
      </button>
      <select
        value={current}
        onChange={(e) => setParams({ refresh: e.target.value === "off" ? null : e.target.value })}
        className="rounded-md border border-edge bg-panel px-2 py-1.5 text-sm text-zinc-300 outline-none"
        title="Auto-refresh interval"
      >
        {REFRESH_OPTIONS.map((o) => (
          <option key={o.key} value={o.key}>
            {o.key === "off" ? "Off" : o.label}
          </option>
        ))}
      </select>
    </div>
  );
}
