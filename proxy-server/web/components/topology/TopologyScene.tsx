"use client";

import { useRef, type MutableRefObject } from "react";
import { Canvas } from "@react-three/fiber";
import { Nodes } from "./Nodes";
import { Links } from "./Links";
import { Packets } from "./Packets";
import { Effects } from "./Effects";
import { CameraRig } from "./CameraRig";
import type { PacketStore, PacketCounters } from "@/lib/topology-packets";

export interface TopologySceneProps {
  store: PacketStore;
  counters: MutableRefObject<PacketCounters>;
  // When the tab is hidden we stop the render loop entirely to spare the GPU.
  paused: boolean;
  reducedMotion: boolean;
  onBlock?: (reason?: string) => void;
}

export function TopologyScene({ store, counters, paused, reducedMotion, onBlock }: TopologySceneProps) {
  // Shared scalar the gate node reads to flash red when a packet is blocked.
  const gatePulse = useRef(0);

  return (
    <Canvas
      flat
      gl={{ alpha: true, antialias: true, powerPreference: "high-performance" }}
      camera={{ position: [3.6, 1.6, 9], fov: 45 }}
      dpr={[1, 1.75]}
      frameloop={paused ? "never" : "always"}
    >
      <ambientLight intensity={0.6} />
      <pointLight position={[6, 8, 8]} intensity={2.4} decay={0} />
      <pointLight position={[-6, -5, -6]} intensity={1.1} decay={0} color="#3d8bfd" />
      <Nodes gatePulse={gatePulse} />
      <Links />
      <Packets
        store={store}
        gatePulse={gatePulse}
        counters={counters}
        reducedMotion={reducedMotion}
        onBlock={onBlock}
      />
      <Effects reducedMotion={reducedMotion} />
      <CameraRig reducedMotion={reducedMotion} />
    </Canvas>
  );
}
