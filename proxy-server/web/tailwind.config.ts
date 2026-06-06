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
      keyframes: {
        // Toast entry: slide in from the right and settle.
        "toast-in": {
          "0%": { opacity: "0", transform: "translateX(12px) scale(0.98)" },
          "100%": { opacity: "1", transform: "translateX(0) scale(1)" },
        },
      },
      animation: {
        "toast-in": "toast-in 220ms ease-out",
      },
    },
  },
  plugins: [],
};

export default config;
