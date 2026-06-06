import { type ReactNode } from "react";
import clsx from "clsx";
import type { ApiFetchError } from "@/lib/swr";

export function Skeleton({ className }: { className?: string }) {
  return <div className={clsx("animate-pulse rounded bg-panelAlt", className)} />;
}

export function EmptyState({ message = "No data in this range." }: { message?: string }) {
  return (
    <div className="flex h-full min-h-[80px] items-center justify-center text-center text-sm text-zinc-600">
      {message}
    </div>
  );
}

export function ErrorState({ error, onRetry }: { error: unknown; onRetry?: () => void }) {
  const e = error as ApiFetchError | undefined;
  const unreachable = e?.code === "proxy_unreachable";
  return (
    <div className="flex h-full min-h-[80px] flex-col items-center justify-center gap-2 text-center">
      <p className="text-sm text-block">
        {unreachable
          ? "プロキシ (127.0.0.1:8787) に接続できません。プロキシを起動するか、USE_MOCK=1 でモックデータを表示できます。"
          : e?.message || "データの取得に失敗しました。"}
      </p>
      {onRetry && (
        <button
          onClick={onRetry}
          className="rounded border border-edge px-2 py-1 text-xs text-zinc-400 hover:bg-panelAlt"
        >
          再試行
        </button>
      )}
    </div>
  );
}

// PanelState renders loading/error/empty around content, used by data panels.
export function PanelState({
  loading,
  error,
  isEmpty,
  onRetry,
  emptyMessage,
  skeleton,
  children,
}: {
  loading: boolean;
  error: unknown;
  isEmpty: boolean;
  onRetry?: () => void;
  emptyMessage?: string;
  skeleton?: ReactNode;
  children: ReactNode;
}) {
  if (error) return <ErrorState error={error} onRetry={onRetry} />;
  if (loading) return <>{skeleton ?? <Skeleton className="h-24 w-full" />}</>;
  if (isEmpty) return <EmptyState message={emptyMessage} />;
  return <>{children}</>;
}
