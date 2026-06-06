"use client";

import { useRouter, usePathname, useSearchParams } from "next/navigation";
import { useCallback } from "react";

// Centralizes reading/writing the URL search params that drive the dashboard
// (time range, refresh, filters, open drawer). Filter tweaks use replace() so
// they don't spam the back button; opening the drawer uses push() so Back closes it.
export function useDashboardParams() {
  const router = useRouter();
  const pathname = usePathname();
  const sp = useSearchParams();

  const setParams = useCallback(
    (updates: Record<string, string | null>, opts?: { push?: boolean }) => {
      const next = new URLSearchParams(sp.toString());
      for (const [k, v] of Object.entries(updates)) {
        if (v === null || v === "") next.delete(k);
        else next.set(k, v);
      }
      const qs = next.toString();
      const url = qs ? `${pathname}?${qs}` : pathname;
      if (opts?.push) router.push(url);
      else router.replace(url, { scroll: false });
    },
    [sp, pathname, router],
  );

  return {
    sp,
    get: (k: string) => sp.get(k),
    setParams,
  };
}
