"use client";

import { useEffect } from "react";
import type { EventRow } from "@/lib/schemas";
import { useEventDetail, type ApiFetchError } from "@/lib/swr";
import { useDashboardParams } from "@/lib/use-dashboard-params";
import { DecisionBadge } from "@/components/common/DecisionBadge";
import { Skeleton } from "@/components/common/states";
import { fmtLocalDateTime, fmtLatency } from "@/lib/format";

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div>
      <dt className="text-2xs uppercase tracking-wide text-zinc-500">{label}</dt>
      <dd className="mt-0.5 text-sm text-zinc-200">{children}</dd>
    </div>
  );
}

// Highlighted renders text with every (case-sensitive, exact) occurrence of
// `snippet` wrapped in a red mark, so the sensitive span the proxy detected
// stands out inside the full prompt. When the snippet is empty or not found
// verbatim (e.g. an LFM-flagged segment that was normalized), the text renders
// plainly — the snippet is still shown separately above.
function Highlighted({ text, snippet }: { text: string; snippet: string | null }) {
  if (!snippet || !text.includes(snippet)) return <>{text}</>;
  const parts: React.ReactNode[] = [];
  let i = 0;
  let k = 0;
  for (let idx = text.indexOf(snippet); idx !== -1; idx = text.indexOf(snippet, i)) {
    if (idx > i) parts.push(text.slice(i, idx));
    parts.push(
      <mark key={k++} className="rounded-sm bg-block/30 px-0.5 font-semibold text-block">
        {snippet}
      </mark>,
    );
    i = idx + snippet.length;
  }
  if (i < text.length) parts.push(text.slice(i));
  return <>{parts}</>;
}

function Detail({ row }: { row: EventRow }) {
  return (
    <div className="space-y-4">
      <dl className="grid grid-cols-2 gap-4">
        <Field label="Decision">
          <DecisionBadge decision={row.decision} />
        </Field>
        <Field label="Source">{row.source || "—"}</Field>
        <Field label="Time">
          <span title={row.createdAt}>{fmtLocalDateTime(row.createdAt)}</span>
        </Field>
        <Field label="Latency">{fmtLatency(row.latencyMs)}</Field>
        <Field label="Model">{row.modelName || "—"}</Field>
        <Field label="Backend">{row.backend || "—"}</Field>
        <Field label="Channel">{row.path || "—"}</Field>
        <Field label="Upstream called">{row.upstreamCalled ? "yes" : "no"}</Field>
      </dl>

      <div>
        <dt className="mb-1 text-2xs uppercase tracking-wide text-zinc-500">Reason</dt>
        <p className="rounded-md border border-edge bg-ink px-3 py-2 text-sm text-zinc-200">
          {row.reason || "—"}
        </p>
      </div>

      {row.matchedSnippet && (
        <div>
          <dt className="mb-1 text-2xs uppercase tracking-wide text-zinc-500">
            検知された箇所（センシティブ判定）
          </dt>
          <pre className="overflow-x-auto rounded-md border border-block/40 bg-block/5 px-3 py-2 font-mono text-xs text-block">
            {row.matchedSnippet}
          </pre>
        </div>
      )}

      <div>
        <dt className="mb-1 text-2xs uppercase tracking-wide text-zinc-500">Prompt (live turn)</dt>
        {row.promptText !== null ? (
          <pre className="max-h-[40vh] overflow-auto whitespace-pre-wrap break-words rounded-md border border-edge bg-ink px-3 py-2 font-mono text-xs text-zinc-200">
            <Highlighted text={row.promptText} snippet={row.matchedSnippet} />
          </pre>
        ) : (
          <p className="rounded-md border border-warn/40 bg-warn/5 px-3 py-2 text-xs text-warn">
            本文は保存されていません（store_raw_text=false）。プロキシで store_raw_text を有効にすると
            ここにプロンプト全文が表示されます。
          </p>
        )}
      </div>
    </div>
  );
}

export function EventDetailDrawer() {
  const { sp, setParams } = useDashboardParams();
  const id = sp.get("event");
  const { data, error, isLoading } = useEventDetail(id);

  const close = () => setParams({ event: null });

  // Close on Escape.
  useEffect(() => {
    if (!id) return;
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && close();
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [id]);

  if (!id) return null;
  const notFound = (error as ApiFetchError | undefined)?.status === 404;

  return (
    <div className="fixed inset-0 z-30">
      <div className="absolute inset-0 bg-black/50" onClick={close} />
      <aside className="absolute right-0 top-0 flex h-full w-full max-w-xl flex-col border-l border-edge bg-panel shadow-2xl">
        <div className="flex items-center justify-between border-b border-edge px-4 py-3">
          <h2 className="text-sm font-medium text-zinc-200">Event detail</h2>
          <button onClick={close} className="rounded px-2 py-1 text-zinc-400 hover:bg-panelAlt">
            ✕
          </button>
        </div>
        <div className="flex-1 overflow-y-auto p-4">
          {notFound ? (
            <p className="text-sm text-zinc-500">
              このイベントは存在しません（保持期間で削除された可能性があります）。
            </p>
          ) : error ? (
            <p className="text-sm text-block">詳細の取得に失敗しました。</p>
          ) : isLoading && !data ? (
            <div className="space-y-3">
              <Skeleton className="h-24 w-full" />
              <Skeleton className="h-40 w-full" />
            </div>
          ) : data ? (
            <Detail row={data} />
          ) : null}
        </div>
      </aside>
    </div>
  );
}
