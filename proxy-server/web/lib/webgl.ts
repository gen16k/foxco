// hasWebGL probes whether the browser can create a WebGL context. SSR-safe: it
// returns false when there is no `document` (server render), so callers can pick
// a non-GL fallback before attempting to mount the react-three-fiber <Canvas>.
export function hasWebGL(): boolean {
  if (typeof document === "undefined" || typeof window === "undefined") return false;
  try {
    const canvas = document.createElement("canvas");
    const gl =
      canvas.getContext("webgl2") ||
      canvas.getContext("webgl") ||
      canvas.getContext("experimental-webgl");
    return !!gl;
  } catch {
    return false;
  }
}
