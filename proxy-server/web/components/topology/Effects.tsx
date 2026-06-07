"use client";

import { EffectComposer, Bloom } from "@react-three/postprocessing";

// Bloom gives emissive nodes/packets the NIRVANA-style glow. mipmapBlur keeps it
// cheap on the integrated GPU; the threshold is tuned so only the bright emissive
// surfaces bloom, not the dim links/background.
export function Effects({ reducedMotion }: { reducedMotion: boolean }) {
  return (
    <EffectComposer>
      <Bloom
        mipmapBlur
        intensity={reducedMotion ? 0.5 : 1.15}
        luminanceThreshold={0.2}
        luminanceSmoothing={0.9}
      />
    </EffectComposer>
  );
}
