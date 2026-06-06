"use client";

import { createContext, useCallback, useContext, useRef, useState, type ReactNode } from "react";
import type { EventRow } from "@/lib/schemas";

export interface Toast {
  id: string; // viewport-local id (not the event id)
  event: EventRow;
}

interface NotificationState {
  toasts: Toast[];
  notify: (event: EventRow) => void;
  dismiss: (id: string) => void;
}

const NotificationContext = createContext<NotificationState>({
  toasts: [],
  notify: () => {},
  dismiss: () => {},
});

export function useNotifications() {
  return useContext(NotificationContext);
}

// Cap retained toasts so a burst can't grow unbounded; the viewport shows the
// most recent few and aggregates the rest as "+N".
const MAX_TOASTS = 8;

export function NotificationProvider({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<Toast[]>([]);
  const seq = useRef(0);

  const notify = useCallback((event: EventRow) => {
    seq.current += 1;
    const id = `t${seq.current}`;
    setToasts((prev) => [...prev, { id, event }].slice(-MAX_TOASTS));
  }, []);

  const dismiss = useCallback((id: string) => {
    setToasts((prev) => prev.filter((t) => t.id !== id));
  }, []);

  return (
    <NotificationContext.Provider value={{ toasts, notify, dismiss }}>
      {children}
    </NotificationContext.Provider>
  );
}
