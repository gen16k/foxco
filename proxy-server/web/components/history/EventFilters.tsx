"use client";

import { useEffect, useState } from "react";
import { useDashboardParams } from "@/lib/use-dashboard-params";

const SOURCES = ["", "rule", "lfm", "classifier_unavailable", "proxy", "sanitizer", "other"];

export function EventFilters() {
  const { sp, setParams } = useDashboardParams();
  const decision = sp.get("decision") || "";
  const source = sp.get("source") || "";
  const limit = sp.get("limit") || "50";
  const urlQ = sp.get("q") || "";
  const [q, setQ] = useState(urlQ);

  // Keep the local input in sync when q is changed externally (e.g. a drill-down).
  useEffect(() => {
    setQ(urlQ);
  }, [urlQ]);

  // Debounce the free-text search into the URL, resetting pagination.
  useEffect(() => {
    const id = setTimeout(() => {
      if (q !== (sp.get("q") || "")) setParams({ q: q || null, offset: null });
    }, 350);
    return () => clearTimeout(id);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [q]);

  const sel = "rounded-md border border-edge bg-panel px-2 py-1.5 text-sm text-zinc-300 outline-none";

  return (
    <div className="flex flex-wrap items-center gap-2">
      <select
        value={decision}
        onChange={(e) => setParams({ decision: e.target.value || null, offset: null })}
        className={sel}
      >
        <option value="">All decisions</option>
        <option value="BLOCK">BLOCK</option>
        <option value="ALLOW">ALLOW</option>
        <option value="BYPASS">BYPASS</option>
        <option value="PASSTHROUGH">PASSTHROUGH</option>
      </select>
      <select
        value={source}
        onChange={(e) => setParams({ source: e.target.value || null, offset: null })}
        className={sel}
      >
        {SOURCES.map((s) => (
          <option key={s} value={s}>
            {s ? s : "All sources"}
          </option>
        ))}
      </select>
      <input
        value={q}
        onChange={(e) => setQ(e.target.value)}
        placeholder="Search reason / prompt…"
        className="min-w-[200px] flex-1 rounded-md border border-edge bg-panel px-3 py-1.5 text-sm text-zinc-200 outline-none focus:border-accent"
      />
      <select value={limit} onChange={(e) => setParams({ limit: e.target.value, offset: null })} className={sel}>
        {["25", "50", "100", "200"].map((n) => (
          <option key={n} value={n}>
            {n}/page
          </option>
        ))}
      </select>
      {(decision || source || q) && (
        <button
          onClick={() => {
            setQ("");
            setParams({ decision: null, source: null, q: null, offset: null });
          }}
          className="rounded-md border border-edge px-2 py-1.5 text-xs text-zinc-400 hover:bg-panelAlt"
        >
          Clear
        </button>
      )}
    </div>
  );
}
