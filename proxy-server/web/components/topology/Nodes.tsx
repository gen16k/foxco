"use client";

import { useRef, type ReactNode, type MutableRefObject } from "react";
import { useFrame } from "@react-three/fiber";
import { Float, Html } from "@react-three/drei";
import * as THREE from "three";
import { NODE_Y } from "@/lib/topology-packets";
import { ClaudeMark, TerminalMark } from "./Marks";

// Node accent colors. Claude = Anthropic clay, Client = dashboard accent blue,
// PromptGate = emerald (focal) that pulses red when it blocks a packet.
const CLAUDE = "#d97757";
const CLIENT = "#3d8bfd";
const GATE = "#34d399";

const gateBase = new THREE.Color(GATE);
const gateHot = new THREE.Color("#f85149");

// HTML labels (instead of drei <Text>) keep typography crisp and — crucially for
// a localhost/offline admin tool — need no web font from a CDN. Each label is a
// logo + text on a dark pill so it stays readable over the bloom glow. The label
// is anchored at the node center (which sits on the Y axis, so it holds a stable
// screen position while the camera auto-rotates) and pushed to the side in
// screen space so it never overlaps the node or swings around it.
function NodeLabel({ icon, title, sub }: { icon: ReactNode; title: string; sub: string }) {
  return (
    <Html position={[0, 0, 0]} center zIndexRange={[20, 0]}>
      <div className="pointer-events-none select-none" style={{ transform: "translateX(66px)" }}>
        <div className="flex items-center gap-2 rounded-md border border-edge/70 bg-ink/75 px-2.5 py-1.5 backdrop-blur-sm">
          <span className="grid h-6 w-6 shrink-0 place-items-center text-xl leading-none">{icon}</span>
          <div className="whitespace-nowrap leading-tight">
            <div className="text-sm font-semibold text-zinc-50">{title}</div>
            <div className="text-2xs text-zinc-400">{sub}</div>
          </div>
        </div>
      </div>
    </Html>
  );
}

function GlowNode({
  y,
  color,
  radius = 0.55,
  children,
}: {
  y: number;
  color: string;
  radius?: number;
  children?: ReactNode;
}) {
  return (
    <group position={[0, y, 0]}>
      <Float speed={1.4} rotationIntensity={0.3} floatIntensity={0.4}>
        <mesh>
          <icosahedronGeometry args={[radius, 2]} />
          <meshStandardMaterial
            color={color}
            emissive={color}
            emissiveIntensity={1.3}
            roughness={0.35}
            metalness={0.15}
            toneMapped={false}
          />
        </mesh>
        <mesh scale={1.3}>
          <icosahedronGeometry args={[radius, 1]} />
          <meshBasicMaterial color={color} wireframe transparent opacity={0.18} toneMapped={false} />
        </mesh>
      </Float>
      {children}
    </group>
  );
}

// The gate reacts to blocks: gatePulse is kicked to 1 by Packets when a red packet
// bursts, then decays here, lerping the emissive toward red and swelling the mesh.
function GateNode({ gatePulse }: { gatePulse: MutableRefObject<number> }) {
  const matRef = useRef<THREE.MeshStandardMaterial>(null);
  const swellRef = useRef<THREE.Group>(null);

  useFrame(() => {
    const p = gatePulse.current;
    if (matRef.current) {
      matRef.current.emissive.copy(gateBase).lerp(gateHot, p);
      matRef.current.emissiveIntensity = 1.3 + p * 2.4;
    }
    if (swellRef.current) swellRef.current.scale.setScalar(1 + p * 0.35);
    gatePulse.current = p > 0.001 ? p * 0.9 : 0;
  });

  return (
    <group position={[0, NODE_Y.gate, 0]}>
      <group ref={swellRef}>
        <Float speed={1.2} rotationIntensity={0.4} floatIntensity={0.3}>
          <mesh>
            <icosahedronGeometry args={[0.64, 2]} />
            <meshStandardMaterial
              ref={matRef}
              color={GATE}
              emissive={gateBase}
              emissiveIntensity={1.3}
              roughness={0.3}
              metalness={0.2}
              toneMapped={false}
            />
          </mesh>
          <mesh scale={1.35}>
            <icosahedronGeometry args={[0.64, 1]} />
            <meshBasicMaterial color={GATE} wireframe transparent opacity={0.22} toneMapped={false} />
          </mesh>
        </Float>
      </group>
      <NodeLabel icon={<span className="text-2xl leading-none">🦊</span>} title="PromptGate" sub="DLP 検査" />
    </group>
  );
}

export function Nodes({ gatePulse }: { gatePulse: MutableRefObject<number> }) {
  return (
    <>
      <GlowNode y={NODE_Y.claude} color={CLAUDE} radius={0.62}>
        <NodeLabel icon={<ClaudeMark />} title="Claude" sub="Anthropic API" />
      </GlowNode>
      <GateNode gatePulse={gatePulse} />
      <GlowNode y={NODE_Y.client} color={CLIENT} radius={0.55}>
        <NodeLabel icon={<TerminalMark />} title="Client" sub="Claude Code" />
      </GlowNode>
    </>
  );
}
