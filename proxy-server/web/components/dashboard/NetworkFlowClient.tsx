"use client";

import { Component, useEffect, useRef, useState, type ReactNode } from "react";
import dynamic from "next/dynamic";
import { useRecentEvents } from "@/lib/swr";
import { Panel } from "@/components/common/Panel";
import { StaticFallback } from "@/components/topology/StaticFallback";
import { hasWebGL } from "@/lib/webgl";
import { createPacketStore, type PacketStore, type PacketCounters } from "@/lib/topology-packets";
import { makeDemoPacket, DEMO_IDLE_MS, DEMO_TICK_MS } from "@/lib/topology-demo";
import type { TopologySceneProps } from "@/components/topology/TopologyScene";

// The WebGL scene is loaded client-side only — three.js/react-three-fiber touch
// `document`/WebGL and must never run during SSR. Lazy-loading also keeps three
// out of every other route's bundle.
const TopologyScene = dynamic<TopologySceneProps>(
  () => import("@/components/topology/TopologyScene").then((m) => m.TopologyScene),
  { ssr: false, loading: () => <div className="h-full w-full animate-pulse bg-panelAlt" /> },
);

// If GL context creation throws after mount, degrade to the static fallback
// instead of white-screening the tab.
class SceneBoundary extends Component<{ fallback: ReactNode; children: ReactNode }, { failed: boolean }> {
  state = { failed: false };
  static getDerivedStateFromError() {
    return { failed: true };
  }
  render() {
    return this.state.failed ? <>{this.props.fallback}</> : <>{this.props.children}</>;
  }
}

function Legend() {
  return (
    <div className="flex items-center gap-3 text-2xs text-zinc-400">
      <span className="flex items-center gap-1.5">
        <span className="h-2 w-2 rounded-full bg-allow" />
        allowed → Claude
      </span>
      <span className="flex items-center gap-1.5">
        <span className="h-2 w-2 rounded-full bg-block" />
        blocked at gate
      </span>
    </div>
  );
}

export function NetworkFlowClient() {
  // All-decision poll (3s). Each new event becomes one packet; demo fills the gaps.
  const { data, error } = useRecentEvents(30);

  // Renderer-agnostic packet store + tallies live in refs so the per-frame loop
  // never triggers a React re-render.
  const storeRef = useRef<PacketStore | null>(null);
  if (!storeRef.current) storeRef.current = createPacketStore();
  const store = storeRef.current;
  const counters = useRef<PacketCounters>({ green: 0, red: 0, active: 0 });

  const seen = useRef<Set<string>>(new Set());
  const initialized = useRef(false);
  const lastRealSpawn = useRef(0);

  const [reducedMotion, setReducedMotion] = useState(false);
  const [hidden, setHidden] = useState(false);
  const [gl, setGl] = useState<boolean | null>(null);
  const [stats, setStats] = useState<PacketCounters>({ green: 0, red: 0, active: 0 });

  useEffect(() => {
    setGl(hasWebGL());
  }, []);

  useEffect(() => {
    const mq = window.matchMedia("(prefers-reduced-motion: reduce)");
    setReducedMotion(mq.matches);
    const onChange = () => setReducedMotion(mq.matches);
    mq.addEventListener("change", onChange);
    return () => mq.removeEventListener("change", onChange);
  }, []);

  useEffect(() => {
    const onVis = () => setHidden(document.hidden);
    onVis();
    document.addEventListener("visibilitychange", onVis);
    return () => document.removeEventListener("visibilitychange", onVis);
  }, []);

  // Real events → packets. First poll only seeds the baseline so history isn't
  // replayed; later polls diff to find genuinely new events (oldest-first).
  useEffect(() => {
    const events = data?.events;
    if (!events) return;
    if (!initialized.current) {
      seen.current = new Set(events.map((e) => e.eventId));
      initialized.current = true;
      return;
    }
    const fresh = events.filter((e) => !seen.current.has(e.eventId));
    seen.current = new Set(events.map((e) => e.eventId));
    if (fresh.length === 0) return;
    const now = performance.now();
    for (let i = fresh.length - 1; i >= 0; i--) {
      const e = fresh[i];
      store.push({
        id: e.eventId,
        kind: e.decision === "BLOCK" ? "red" : "green",
        spawnTime: now,
        reason: e.reason || undefined,
        source: e.source || undefined,
      });
    }
    lastRealSpawn.current = now;
  }, [data, store]);

  // Demo fallback: when no real packet has spawned recently, emit a synthetic one
  // so the view keeps moving. Paused while the tab is hidden.
  useEffect(() => {
    if (hidden) return;
    const id = setInterval(() => {
      const now = performance.now();
      if (now - lastRealSpawn.current > DEMO_IDLE_MS && store.items.length < 18) {
        store.push(makeDemoPacket(now));
      }
    }, DEMO_TICK_MS);
    return () => clearInterval(id);
  }, [hidden, store]);

  // Flush tallies to UI ~2×/sec (never per frame).
  useEffect(() => {
    const id = setInterval(() => {
      const c = counters.current;
      setStats({ green: c.green, red: c.red, active: c.active });
    }, 500);
    return () => clearInterval(id);
  }, []);

  useEffect(() => () => store.clear(), [store]);

  const offline = !!error;

  return (
    <Panel
      title="Network Flow"
      subtitle="クライアント → PromptGate → Claude のパケットの流れ"
      right={<Legend />}
      bodyClassName="p-3"
    >
      <div className="relative h-[60vh] min-h-[420px] w-full overflow-hidden rounded-md bg-ink">
        {gl === null && <div className="h-full w-full animate-pulse bg-panelAlt" />}
        {gl === false && <StaticFallback />}
        {gl === true && (
          <SceneBoundary fallback={<StaticFallback />}>
            <TopologyScene
              store={store}
              counters={counters}
              paused={hidden}
              reducedMotion={reducedMotion}
            />
          </SceneBoundary>
        )}

        <div className="pointer-events-none absolute left-3 top-3 flex flex-col gap-1 text-2xs">
          <span className="text-zinc-400">
            in flight <span className="font-mono text-zinc-200">{stats.active}</span>
          </span>
          <span className="text-allow">
            allowed <span className="font-mono">{stats.green}</span>
          </span>
          <span className="text-block">
            blocked <span className="font-mono">{stats.red}</span>
          </span>
        </div>

        {offline && (
          <div className="absolute right-3 top-3 rounded border border-warn/40 bg-warn/10 px-2 py-1 text-2xs text-warn">
            proxy offline — demo data
          </div>
        )}
      </div>
    </Panel>
  );
}
