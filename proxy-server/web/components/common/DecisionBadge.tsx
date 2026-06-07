import clsx from "clsx";

// Per-decision styling. ALLOW=green, BLOCK=rose, BYPASS=amber (the user
// explicitly overrode DLP for that turn), PASSTHROUGH=blue (a non-message path
// that is forwarded without DLP inspection). Unknown decisions fall back to the
// ALLOW style. Class strings are full literals so Tailwind's JIT picks them up.
const STYLES: Record<string, { label: string; bg: string; text: string; dot: string }> = {
  BLOCK: { label: "BLOCK", bg: "bg-block/15", text: "text-block", dot: "bg-block" },
  BYPASS: { label: "BYPASS", bg: "bg-warn/15", text: "text-warn", dot: "bg-warn" },
  PASSTHROUGH: { label: "PASSTHRU", bg: "bg-accent/15", text: "text-accent", dot: "bg-accent" },
  ALLOW: { label: "ALLOW", bg: "bg-allow/15", text: "text-allow", dot: "bg-allow" },
};

export function DecisionBadge({ decision }: { decision: string }) {
  const s = STYLES[decision] ?? STYLES.ALLOW;
  return (
    <span
      className={clsx(
        "inline-flex items-center gap-1.5 rounded px-1.5 py-0.5 text-2xs font-semibold",
        s.bg,
        s.text,
      )}
    >
      <span className={clsx("h-1.5 w-1.5 rounded-full", s.dot)} />
      {s.label}
    </span>
  );
}
