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
      // Type scale bumped ~1-2px over Tailwind defaults — the dashboard was
      // reading too small. `2xs` replaces the old arbitrary text-[10px]/[11px]
      // micro-labels so they scale with the rest.
      fontSize: {
        "2xs": ["0.75rem", { lineHeight: "1rem" }], // 12px
        xs: ["0.8125rem", { lineHeight: "1.125rem" }], // 13px (was 12)
        sm: ["0.9375rem", { lineHeight: "1.375rem" }], // 15px (was 14)
        base: ["1.0625rem", { lineHeight: "1.625rem" }], // 17px (was 16)
        lg: ["1.1875rem", { lineHeight: "1.75rem" }], // 19px (was 18)
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
