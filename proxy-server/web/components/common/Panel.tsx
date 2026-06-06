import { type ReactNode } from "react";
import { twMerge } from "tailwind-merge";

export function Panel({
  title,
  subtitle,
  right,
  className,
  bodyClassName,
  children,
}: {
  title?: string;
  subtitle?: string;
  right?: ReactNode;
  className?: string;
  bodyClassName?: string;
  children: ReactNode;
}) {
  return (
    <section className={twMerge("rounded-lg border border-edge bg-panel", className)}>
      {(title || right) && (
        <header className="flex items-center justify-between gap-2 border-b border-edge px-4 py-2.5">
          <div className="min-w-0">
            {title && <h2 className="truncate text-sm font-medium text-zinc-200">{title}</h2>}
            {subtitle && <p className="truncate text-xs text-zinc-500">{subtitle}</p>}
          </div>
          {right}
        </header>
      )}
      <div className={twMerge("p-4", bodyClassName)}>{children}</div>
    </section>
  );
}
