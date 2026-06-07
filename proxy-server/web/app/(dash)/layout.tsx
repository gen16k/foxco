import { Suspense } from "react";
import { RefreshProvider } from "@/components/shell/RefreshContext";
import { SoundProvider } from "@/components/shell/SoundContext";
import { NotificationProvider } from "@/components/notify/NotificationContext";
import { ToastViewport } from "@/components/notify/ToastViewport";
import { LiveDetectionWatcher } from "@/components/notify/LiveDetectionWatcher";
import { TopBar } from "@/components/shell/TopBar";

export const dynamic = "force-dynamic";

export default function DashLayout({ children }: { children: React.ReactNode }) {
  return (
    <RefreshProvider>
      <SoundProvider>
        <NotificationProvider>
          <Suspense fallback={<div className="h-[49px] border-b border-edge" />}>
            <TopBar />
          </Suspense>
          <main className="mx-auto max-w-[1600px] p-4">{children}</main>
          {/* Renders nothing; watches the recent-block poll and raises toasts. */}
          <LiveDetectionWatcher />
          <ToastViewport />
        </NotificationProvider>
      </SoundProvider>
    </RefreshProvider>
  );
}
