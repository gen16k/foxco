"use client";

import clsx from "clsx";
import { useMeta } from "@/lib/swr";

function Pill({ children }: { children: React.ReactNode }) {
  return <span className="rounded bg-panelAlt px-1.5 py-0.5 text-zinc-400">{children}</span>;
}

export function ProxyStatus() {
  const { data, error } = useMeta();
  const ok = !error && !!data;
  return (
    <div className="hidden items-center gap-2 text-xs text-zinc-500 lg:flex">
      <span className="flex items-center gap-1.5">
        <span className={clsx("h-2 w-2 rounded-full", ok ? "bg-allow" : "bg-block")} />
        {ok ? "proxy up" : "proxy down"}
      </span>
      {data && (
        <>
          <Pill>{data.backend || "—"}</Pill>
          <Pill>{data.model || "—"}</Pill>
          <span
            className={clsx(
              "rounded px-1.5 py-0.5",
              data.storeRawText ? "bg-allow/10 text-allow" : "bg-warn/10 text-warn",
            )}
            title="store_raw_text: whether prompt bodies are persisted"
          >
            raw-text: {data.storeRawText ? "on" : "off"}
          </span>
        </>
      )}
    </div>
  );
}
