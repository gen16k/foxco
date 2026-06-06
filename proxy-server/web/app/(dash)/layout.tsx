import { Suspense } from "react";
import { RefreshProvider } from "@/components/shell/RefreshContext";
import { TopBar } from "@/components/shell/TopBar";

export const dynamic = "force-dynamic";

export default function DashLayout({ children }: { children: React.ReactNode }) {
  return (
    <RefreshProvider>
      <Suspense fallback={<div className="h-[49px] border-b border-edge" />}>
        <TopBar />
      </Suspense>
      <main className="mx-auto max-w-[1600px] p-4">{children}</main>
    </RefreshProvider>
  );
}
