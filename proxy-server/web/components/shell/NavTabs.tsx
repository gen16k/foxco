"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import clsx from "clsx";

const TABS = [
  { href: "/", label: "Overview" },
  { href: "/history", label: "Prompt History" },
];

export function NavTabs() {
  const pathname = usePathname();
  return (
    <nav className="flex gap-1">
      {TABS.map((t) => {
        const active = pathname === t.href;
        return (
          <Link
            key={t.href}
            href={t.href}
            className={clsx(
              "rounded-md px-3 py-1.5 text-sm transition",
              active ? "bg-panelAlt text-zinc-100" : "text-zinc-400 hover:text-zinc-200",
            )}
          >
            {t.label}
          </Link>
        );
      })}
    </nav>
  );
}
