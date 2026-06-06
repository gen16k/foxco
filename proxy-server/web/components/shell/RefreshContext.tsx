"use client";

import { createContext, useState, type ReactNode } from "react";

interface RefreshState {
  intervalMs: number;
  setIntervalMs: (ms: number) => void;
}

export const RefreshContext = createContext<RefreshState>({
  intervalMs: 0,
  setIntervalMs: () => {},
});

export function RefreshProvider({ children }: { children: ReactNode }) {
  const [intervalMs, setIntervalMs] = useState(0);
  return (
    <RefreshContext.Provider value={{ intervalMs, setIntervalMs }}>
      {children}
    </RefreshContext.Provider>
  );
}
