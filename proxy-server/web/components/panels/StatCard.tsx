import clsx from "clsx";

export function StatCard({
  label,
  value,
  accent,
  hint,
}: {
  label: string;
  value: string;
  accent?: string;
  hint?: string;
}) {
  return (
    <div className="rounded-lg border border-edge bg-panel px-4 py-3">
      <p className="text-xs text-zinc-500">{label}</p>
      <p className={clsx("mt-1 text-2xl font-semibold tabular-nums", accent ?? "text-zinc-100")}>{value}</p>
      {hint && <p className="mt-0.5 text-2xs text-zinc-600">{hint}</p>}
    </div>
  );
}
