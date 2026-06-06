import { Suspense } from "react";
import { HistoryClient } from "@/components/dashboard/HistoryClient";

export const dynamic = "force-dynamic";

export default function HistoryPage() {
  return (
    <Suspense fallback={<div className="text-sm text-zinc-500">Loading…</div>}>
      <HistoryClient />
    </Suspense>
  );
}
