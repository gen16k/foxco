import clsx from "clsx";

export function DecisionBadge({ decision }: { decision: string }) {
  const block = decision === "BLOCK";
  return (
    <span
      className={clsx(
        "inline-flex items-center gap-1.5 rounded px-1.5 py-0.5 text-[11px] font-semibold",
        block ? "bg-block/15 text-block" : "bg-allow/15 text-allow",
      )}
    >
      <span className={clsx("h-1.5 w-1.5 rounded-full", block ? "bg-block" : "bg-allow")} />
      {block ? "BLOCK" : "ALLOW"}
    </span>
  );
}
