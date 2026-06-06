import clsx from "clsx";

export function Pagination({
  total,
  limit,
  offset,
  onPage,
}: {
  total: number;
  limit: number;
  offset: number;
  onPage: (offset: number) => void;
}) {
  const from = total === 0 ? 0 : offset + 1;
  const to = Math.min(offset + limit, total);
  const canPrev = offset > 0;
  const canNext = offset + limit < total;
  const btn = "rounded border border-edge px-2 py-1 text-xs disabled:opacity-40";
  return (
    <div className="flex items-center justify-between text-xs text-zinc-400">
      <span>
        {from}–{to} / {total}
      </span>
      <div className="flex gap-2">
        <button
          className={clsx(btn, canPrev && "hover:bg-panelAlt")}
          disabled={!canPrev}
          onClick={() => onPage(Math.max(0, offset - limit))}
        >
          ← Prev
        </button>
        <button
          className={clsx(btn, canNext && "hover:bg-panelAlt")}
          disabled={!canNext}
          onClick={() => onPage(offset + limit)}
        >
          Next →
        </button>
      </div>
    </div>
  );
}
