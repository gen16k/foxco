import type { Config } from "tailwindcss";

// Grafana-inspired dark palette. Surfaces are near-black with subtle borders;
// green = ALLOW, rose = BLOCK, amber = degraded/raw-text-off.
const config: Config = {
  darkMode: "class",
  content: ["./app/**/*.{ts,tsx}", "./components/**/*.{ts,tsx}"],
  theme: {
    extend: {
      colors: {
        ink: "#0b0c0e", // app background
        panel: "#181b1f", // card background
        panelAlt: "#1f242a", // hover / nested
        edge: "#2a2f36", // borders
        allow: "#3fb950",
        block: "#f85149",
        warn: "#d29922",
        accent: "#3d8bfd",
      },
      fontFamily: {
        mono: ["ui-monospace", "SFMono-Regular", "Menlo", "Consolas", "monospace"],
      },
    },
  },
  plugins: [],
};

export default config;
