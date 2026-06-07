"use client";

import { type CSSProperties } from "react";

// CSS/DOM-only pseudo-3D fallback used when WebGL is unavailable (or the GL scene
// throws). A perspective tilt fakes depth; two CSS-animated dots show the green
// allow-flow and the red block bouncing at PromptGate — so the story still reads
// without a GPU. This is also the seed for a lighter pseudo-3D mode if the real
// 3D ever proves too heavy.

function Node({ className, color, title, sub }: { className: string; color: string; title: string; sub: string }) {
  return (
    <div className={`absolute left-1/2 -translate-x-1/2 ${className}`}>
      <div className="flex items-center gap-2">
        <span
          className="h-4 w-4 shrink-0 rounded-full"
          style={{ background: color, boxShadow: `0 0 16px ${color}` }}
        />
        <div className="whitespace-nowrap">
          <div className="text-xs font-semibold text-zinc-100">{title}</div>
          <div className="text-2xs text-zinc-500">{sub}</div>
        </div>
      </div>
    </div>
  );
}

function Dot({ color, style }: { color: string; style: CSSProperties }) {
  return (
    <span
      className="absolute left-1/2 h-3 w-3 -translate-x-1/2 rounded-full"
      style={{ background: color, boxShadow: `0 0 12px ${color}`, ...style }}
    />
  );
}

export function StaticFallback() {
  return (
    <div className="relative flex h-full w-full items-center justify-center bg-ink">
      <style>{`
        @keyframes nf-green {
          0% { top: 86%; opacity: 0 }
          10% { opacity: 1 }
          90% { opacity: 1 }
          100% { top: 6%; opacity: 0 }
        }
        @keyframes nf-red {
          0% { top: 86%; transform: translateX(-50%) scale(1); opacity: 0 }
          12% { opacity: 1 }
          48% { top: 50%; transform: translateX(-50%) scale(1); opacity: 1 }
          62% { top: 50%; transform: translateX(-50%) scale(2.4); opacity: .85 }
          78% { top: 50%; transform: translateX(-50%) scale(.2); opacity: 0 }
          100% { top: 50%; transform: translateX(-50%) scale(.2); opacity: 0 }
        }
        @media (prefers-reduced-motion: reduce) {
          .nf-dot { animation: none !important; opacity: .9 !important }
        }
      `}</style>
      <div style={{ perspective: "700px" }}>
        <div className="relative h-[360px] w-[260px]" style={{ transform: "rotateX(14deg)" }}>
          <div className="absolute left-1/2 top-[6%] h-[80%] w-px -translate-x-1/2 bg-edge" />
          <Node className="top-[6%]" color="#d97757" title="Claude" sub="Anthropic API" />
          <Node className="top-1/2 -translate-y-1/2" color="#34d399" title="PromptGate" sub="DLP 検査" />
          <Node className="top-[86%]" color="#3d8bfd" title="Client" sub="Claude Code" />
          <Dot color="#3fb950" style={{ animation: "nf-green 2.6s ease-in-out infinite" }} />
          <Dot color="#f85149" style={{ animation: "nf-red 3.4s ease-in-out infinite 1.3s" }} />
        </div>
      </div>
      <div className="absolute bottom-3 left-1/2 -translate-x-1/2 text-2xs text-zinc-500">
        WebGL を利用できないため簡易表示中
      </div>
    </div>
  );
}
