"use client";

import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from "react";
import { primeAudio } from "@/lib/sound";

interface SoundState {
  enabled: boolean;
  toggle: () => void;
}

const SoundContext = createContext<SoundState>({ enabled: false, toggle: () => {} });

export function useSound() {
  return useContext(SoundContext);
}

const KEY = "foxco.sound";

// SoundProvider holds the opt-in alert-sound preference (default off), persisted
// to localStorage. It hydrates after mount to avoid an SSR mismatch, and resumes
// the AudioContext within the toggle click so later beeps are allowed to play.
export function SoundProvider({ children }: { children: ReactNode }) {
  const [enabled, setEnabled] = useState(false);

  useEffect(() => {
    try {
      if (localStorage.getItem(KEY) === "1") setEnabled(true);
    } catch {
      /* ignore */
    }
  }, []);

  const toggle = useCallback(() => {
    setEnabled((prev) => {
      const next = !prev;
      try {
        localStorage.setItem(KEY, next ? "1" : "0");
      } catch {
        /* ignore */
      }
      if (next) primeAudio();
      return next;
    });
  }, []);

  return <SoundContext.Provider value={{ enabled, toggle }}>{children}</SoundContext.Provider>;
}
