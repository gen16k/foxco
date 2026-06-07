// Renderer-agnostic packet model + store for the Network Flow visualization.
// The store keeps the live packet list in a plain mutable array so the 3D scene's
// per-frame loop can read/mutate it WITHOUT triggering React re-renders. The same
// store could drive a CSS/SVG pseudo-3D renderer unchanged.

export type PacketKind = "green" | "red";
// rising: travelling up toward (green) Claude or (red) the gate.
// bursting: a red packet that hit PromptGate and is exploding/fading.
// done: finished; swept out of the list next frame.
export type PacketPhase = "rising" | "bursting" | "done";

export interface Packet {
  id: string;
  kind: PacketKind;
  spawnTime: number; // performance.now() at spawn (ms)
  reason?: string;
  source?: string;
  phase: PacketPhase;
  ox: number; // small lateral X offset for depth/spread
  oz: number; // small lateral Z offset for depth/spread
}

// Scene-space Y of each node. Single source of truth shared by Nodes, Links and
// Packets so the geometry never drifts out of sync.
export const NODE_Y = { client: -3, gate: 0, claude: 3 } as const;

// Hard cap on concurrent packets so a traffic burst can never exhaust the GPU or
// memory. New packets past the cap are dropped (see push()).
export const MAX_PACKETS = 120;

// Animation durations (ms).
export const GREEN_DURATION_MS = 2200; // client -> gate -> claude
export const RED_DURATION_MS = 1400; // client -> gate, then burst+fade
export const RED_RISE_FRACTION = 0.6; // fraction of RED_DURATION spent rising to the gate

export interface PacketSpawn {
  id: string;
  kind: PacketKind;
  spawnTime: number;
  reason?: string;
  source?: string;
}

// Session tallies, mutated in the scene's frame loop and flushed to React state
// at a low cadence (never per frame) for the on-screen counters.
export interface PacketCounters {
  green: number; // packets that reached Claude
  red: number; // packets blocked at PromptGate
  active: number; // currently in flight
}

export interface PacketStore {
  items: Packet[];
  /** Append a packet. Returns false (dropped) when at MAX_PACKETS. */
  push: (spawn: PacketSpawn) => boolean;
  /** Remove finished packets in place (no allocation churn). */
  sweep: () => void;
  /** Drop everything (used on unmount). */
  clear: () => void;
}

export function createPacketStore(): PacketStore {
  const items: Packet[] = [];
  const SPREAD = 0.9;
  return {
    items,
    push(spawn) {
      if (items.length >= MAX_PACKETS) return false;
      items.push({
        ...spawn,
        phase: "rising",
        ox: (Math.random() - 0.5) * SPREAD,
        oz: (Math.random() - 0.5) * SPREAD,
      });
      return true;
    },
    sweep() {
      for (let i = items.length - 1; i >= 0; i--) {
        if (items[i].phase === "done") items.splice(i, 1);
      }
    },
    clear() {
      items.length = 0;
    },
  };
}
