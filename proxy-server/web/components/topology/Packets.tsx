"use client";

import { useEffect, useMemo, useRef, type MutableRefObject } from "react";
import { useFrame } from "@react-three/fiber";
import * as THREE from "three";
import {
  MAX_PACKETS,
  NODE_Y,
  GREEN_DURATION_MS,
  RED_DURATION_MS,
  RED_RISE_FRACTION,
  type PacketStore,
  type PacketCounters,
} from "@/lib/topology-packets";

const GREEN = new THREE.Color("#3fb950");
const RED = new THREE.Color("#f85149");

// smoothstep ease — eases in/out so packets accelerate off the node and settle.
function smooth(t: number): number {
  const x = Math.min(1, Math.max(0, t));
  return x * x * (3 - 2 * x);
}

interface PacketsProps {
  store: PacketStore;
  gatePulse: MutableRefObject<number>;
  counters: MutableRefObject<PacketCounters>;
  reducedMotion: boolean;
  // Fired once, when a red packet starts bursting at the gate, so the parent can
  // raise a floating "blocked genre" popup.
  onBlock?: (reason?: string) => void;
}

// All packets render from ONE instanced mesh (a single draw call). Every frame we
// recompute each live packet's matrix + color from the renderer-agnostic store;
// React never re-renders for this. Unused instances are scaled to 0 (hidden).
export function Packets({ store, gatePulse, counters, reducedMotion, onBlock }: PacketsProps) {
  const meshRef = useRef<THREE.InstancedMesh>(null);
  const dummy = useMemo(() => new THREE.Object3D(), []);
  const tmpColor = useMemo(() => new THREE.Color(), []);

  const durMul = reducedMotion ? 1.6 : 1;

  // Hide every instance on mount so there's no first-frame flash of MAX_PACKETS
  // unit-scale spheres stacked at the origin.
  useEffect(() => {
    const mesh = meshRef.current;
    if (!mesh) return;
    dummy.position.set(0, 0, 0);
    dummy.scale.setScalar(0);
    dummy.updateMatrix();
    for (let i = 0; i < MAX_PACKETS; i++) mesh.setMatrixAt(i, dummy.matrix);
    mesh.instanceMatrix.needsUpdate = true;
  }, [dummy]);

  useFrame(() => {
    const mesh = meshRef.current;
    if (!mesh) return;
    const now = performance.now();
    const items = store.items;
    const base = 1;

    for (let i = 0; i < items.length; i++) {
      const p = items[i];
      let x = p.ox;
      let z = p.oz;
      let y: number = NODE_Y.client;
      let scale = base;

      if (p.kind === "green") {
        const t = (now - p.spawnTime) / (GREEN_DURATION_MS * durMul);
        if (t >= 1) {
          p.phase = "done";
          counters.current.green += 1;
          scale = 0;
        } else {
          const e = smooth(t);
          y = NODE_Y.client + (NODE_Y.claude - NODE_Y.client) * e;
          // gentle breathing; flat when reduced-motion.
          scale = base * (reducedMotion ? 1 : 0.8 + 0.25 * Math.sin(t * Math.PI));
        }
      } else {
        const t = (now - p.spawnTime) / (RED_DURATION_MS * durMul);
        if (t >= 1) {
          p.phase = "done";
          counters.current.red += 1;
          scale = 0;
        } else if (t < RED_RISE_FRACTION) {
          // rising from the client up to the gate
          const e = smooth(t / RED_RISE_FRACTION);
          y = NODE_Y.client + (NODE_Y.gate - NODE_Y.client) * e;
        } else {
          // bursting at the gate — never proceeds to Claude
          if (p.phase !== "bursting") {
            p.phase = "bursting";
            gatePulse.current = 1; // kick the gate's red pulse once
            onBlock?.(p.reason); // raise the floating genre popup once
          }
          const bt = (t - RED_RISE_FRACTION) / (1 - RED_RISE_FRACTION); // 0..1
          const burst = Math.sin(bt * Math.PI); // 0 -> 1 -> 0
          y = NODE_Y.gate;
          scale = base * (1 + burst * (reducedMotion ? 1 : 2.4));
          // fling the shard outward as it explodes
          const spread = 1 + bt * (reducedMotion ? 1.5 : 3.5);
          x = p.ox * spread;
          z = p.oz * spread;
        }
      }

      dummy.position.set(x, y, z);
      dummy.scale.setScalar(Math.max(0, scale));
      dummy.updateMatrix();
      mesh.setMatrixAt(i, dummy.matrix);
      tmpColor.copy(p.kind === "green" ? GREEN : RED);
      mesh.setColorAt(i, tmpColor);
    }

    // Hide instances beyond the live count.
    dummy.scale.setScalar(0);
    dummy.updateMatrix();
    for (let i = items.length; i < MAX_PACKETS; i++) mesh.setMatrixAt(i, dummy.matrix);

    mesh.instanceMatrix.needsUpdate = true;
    if (mesh.instanceColor) mesh.instanceColor.needsUpdate = true;

    counters.current.active = items.length;
    store.sweep(); // drop the ones marked done this frame
  });

  return (
    <instancedMesh ref={meshRef} args={[undefined, undefined, MAX_PACKETS]} frustumCulled={false}>
      <sphereGeometry args={[0.14, 16, 16]} />
      <meshBasicMaterial
        transparent
        depthWrite={false}
        blending={THREE.AdditiveBlending}
        toneMapped={false}
      />
    </instancedMesh>
  );
}
