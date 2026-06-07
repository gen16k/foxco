import { Suspense } from "react";
import { NetworkFlowClient } from "@/components/dashboard/NetworkFlowClient";

export const dynamic = "force-dynamic";

export default function NetworkPage() {
  return (
    <Suspense fallback={<div className="text-sm text-zinc-400">Loading…</div>}>
      <NetworkFlowClient />
    </Suspense>
  );
}
