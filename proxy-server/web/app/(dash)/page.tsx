import { Suspense } from "react";
import { OverviewClient } from "@/components/dashboard/OverviewClient";

export const dynamic = "force-dynamic";

export default function OverviewPage() {
  return (
    <Suspense fallback={<div className="text-sm text-zinc-400">Loading…</div>}>
      <OverviewClient />
    </Suspense>
  );
}
