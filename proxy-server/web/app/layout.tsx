import "./globals.css";
import type { Metadata } from "next";

export const metadata: Metadata = {
  title: "FoxCo DLP — Admin",
  description: "Local LFM DLP Proxy — detections & prompt history",
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="ja" className="dark">
      <body className="min-h-screen bg-ink text-zinc-200 antialiased">{children}</body>
    </html>
  );
}
