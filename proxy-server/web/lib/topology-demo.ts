// Demo packet generator. When the proxy is idle (or unreachable), the Network
// Flow tab keeps showing motion by emitting synthetic packets so the view never
// goes static during a demo. Browser-only (uses Math.random); never imported on
// the server hot path.

import type { PacketSpawn } from "./topology-packets";

// Emit a demo packet only after this much idle time since the last REAL event,
// checked every DEMO_TICK_MS.
export const DEMO_IDLE_MS = 1200;
export const DEMO_TICK_MS = 600;
export const DEMO_RED_PROB = 0.2; // ~20% of demo packets are blocks (red)

// Plausible-but-fake labels shown on hover/legend. No real secrets — these mirror
// the canonical fake examples used elsewhere in the mock dataset.
const RED_REASONS = [
  "secret detected (aws_access_key)",
  "secret detected (github_token)",
  "personal email address",
  "internal hostname / private IP",
  "database credential in prompt",
];

let counter = 0;

function pick<T>(xs: readonly T[]): T {
  return xs[Math.floor(Math.random() * xs.length)];
}

// makeDemoPacket builds one synthetic packet stamped at `now` (performance.now()).
export function makeDemoPacket(now: number): PacketSpawn {
  const isRed = Math.random() < DEMO_RED_PROB;
  counter += 1;
  return {
    id: `demo_${Math.floor(now)}_${counter}`,
    kind: isRed ? "red" : "green",
    spawnTime: now,
    reason: isRed ? pick(RED_REASONS) : undefined,
    source: "demo",
  };
}
