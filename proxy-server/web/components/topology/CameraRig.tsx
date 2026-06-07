"use client";

import { OrbitControls } from "@react-three/drei";

// Default to a gentle auto-rotate for the ambient NIRVANA feel; the user can still
// drag to orbit and scroll to zoom. Polar angle is clamped so the vertical
// Client→PromptGate→Claude stack always reads top-to-bottom. Reduced-motion stops
// the auto-rotate (dragging still works).
export function CameraRig({ reducedMotion }: { reducedMotion: boolean }) {
  return (
    <OrbitControls
      makeDefault
      enablePan={false}
      enableZoom
      autoRotate={!reducedMotion}
      autoRotateSpeed={0.5}
      minPolarAngle={Math.PI * 0.28}
      maxPolarAngle={Math.PI * 0.72}
      minDistance={6}
      maxDistance={14}
    />
  );
}
