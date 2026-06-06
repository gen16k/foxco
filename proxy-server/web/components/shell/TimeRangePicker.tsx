"use client";

import { useState } from "react";
import clsx from "clsx";
import {
  RANGE_PRESETS,
  DEFAULT_RANGE,
  activeRangeLabel,
  localInputToRFC3339,
  rfc3339ToLocalInput,
} from "@/lib/time-range";
import { useDashboardParams } from "@/lib/use-dashboard-params";

export function TimeRangePicker() {
  const { sp, setParams } = useDashboardParams();
  const [open, setOpen] = useState(false);
  const range = sp.get("range");
  const from = sp.get("from");
  const to = sp.get("to");
  const isCustom = !!(from && to);
  const activeKey = isCustom ? null : range || DEFAULT_RANGE;

  const [fromLocal, setFromLocal] = useState(rfc3339ToLocalInput(from || ""));
  const [toLocal, setToLocal] = useState(rfc3339ToLocalInput(to || ""));

  return (
    <div className="relative">
      <button
        onClick={() => setOpen((o) => !o)}
        className="flex items-center gap-1.5 rounded-md border border-edge bg-panel px-3 py-1.5 text-sm text-zinc-300 hover:bg-panelAlt"
      >
        <span aria-hidden>🕓</span>
        {activeRangeLabel({ range, from, to })}
        <span className="text-zinc-600">▾</span>
      </button>

      {open && (
        <>
          <div className="fixed inset-0 z-10" onClick={() => setOpen(false)} />
          <div className="absolute right-0 z-20 mt-1 w-72 rounded-md border border-edge bg-panel p-2 shadow-2xl">
            <div className="grid grid-cols-2 gap-1">
              {RANGE_PRESETS.map((p) => (
                <button
                  key={p.key}
                  onClick={() => {
                    setParams({ range: p.key, from: null, to: null });
                    setOpen(false);
                  }}
                  className={clsx(
                    "rounded px-2 py-1.5 text-left text-xs transition",
                    activeKey === p.key ? "bg-accent/20 text-accent" : "text-zinc-300 hover:bg-panelAlt",
                  )}
                >
                  {p.label}
                </button>
              ))}
            </div>

            <div className="mt-2 border-t border-edge pt-2">
              <p className="mb-1 text-2xs uppercase tracking-wide text-zinc-500">Custom range</p>
              <label className="mb-1 block text-2xs text-zinc-500">From</label>
              <input
                type="datetime-local"
                value={fromLocal}
                onChange={(e) => setFromLocal(e.target.value)}
                className="mb-2 w-full rounded border border-edge bg-ink px-2 py-1 text-xs text-zinc-200"
              />
              <label className="mb-1 block text-2xs text-zinc-500">To</label>
              <input
                type="datetime-local"
                value={toLocal}
                onChange={(e) => setToLocal(e.target.value)}
                className="mb-2 w-full rounded border border-edge bg-ink px-2 py-1 text-xs text-zinc-200"
              />
              <button
                disabled={!fromLocal || !toLocal}
                onClick={() => {
                  setParams({
                    from: localInputToRFC3339(fromLocal),
                    to: localInputToRFC3339(toLocal),
                    range: null,
                  });
                  setOpen(false);
                }}
                className="w-full rounded bg-accent px-2 py-1.5 text-xs font-medium text-white disabled:opacity-40"
              >
                Apply
              </button>
            </div>
          </div>
        </>
      )}
    </div>
  );
}
