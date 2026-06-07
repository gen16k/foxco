"use client";

// Small inline logo marks for the topology nodes. Kept as SVG (no external font /
// asset fetch) so they render on a localhost/offline admin box. The fox at
// PromptGate uses the 🦊 emoji inline at the call site (matches the TopBar brand).

// Claude: an Anthropic-style radial "spark" sunburst in the clay accent.
export function ClaudeMark({ size = 20, color = "#d97757" }: { size?: number; color?: string }) {
  const rays = 12;
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" aria-hidden="true">
      {Array.from({ length: rays }).map((_, i) => (
        <rect
          key={i}
          x="11.1"
          y="2.2"
          width="1.8"
          height="8.4"
          rx="0.9"
          fill={color}
          transform={`rotate(${(360 / rays) * i} 12 12)`}
        />
      ))}
    </svg>
  );
}

// Client: a terminal/prompt glyph in the accent blue.
export function TerminalMark({ size = 18, color = "#3d8bfd" }: { size?: number; color?: string }) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke={color}
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <rect x="3" y="4" width="18" height="16" rx="2" />
      <path d="M7 9l2.5 3L7 15" />
      <path d="M13 15h4" />
    </svg>
  );
}
