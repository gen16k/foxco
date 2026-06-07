"use client";

import { Line } from "@react-three/drei";
import * as THREE from "three";
import { NODE_Y } from "@/lib/topology-packets";

// A faint additive cylinder gives the link a soft glowing "conduit" body; the
// drei <Line> draws the crisp edge. Motion along the conduit is the packets' job.
function Conduit({ from, to }: { from: number; to: number }) {
  const mid = (from + to) / 2;
  const len = Math.abs(to - from);
  return (
    <mesh position={[0, mid, 0]}>
      <cylinderGeometry args={[0.06, 0.06, len, 16, 1, true]} />
      <meshBasicMaterial
        color="#3d8bfd"
        transparent
        opacity={0.07}
        blending={THREE.AdditiveBlending}
        depthWrite={false}
        side={THREE.DoubleSide}
        toneMapped={false}
      />
    </mesh>
  );
}

export function Links() {
  return (
    <>
      <Line
        points={[
          [0, NODE_Y.client, 0],
          [0, NODE_Y.gate, 0],
        ]}
        color="#3a4654"
        lineWidth={1.5}
        transparent
        opacity={0.7}
      />
      <Line
        points={[
          [0, NODE_Y.gate, 0],
          [0, NODE_Y.claude, 0],
        ]}
        color="#3a4654"
        lineWidth={1.5}
        transparent
        opacity={0.7}
      />
      <Conduit from={NODE_Y.client} to={NODE_Y.gate} />
      <Conduit from={NODE_Y.gate} to={NODE_Y.claude} />
    </>
  );
}
