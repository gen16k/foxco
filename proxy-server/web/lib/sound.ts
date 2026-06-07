// A minimal WebAudio "blip" for the live-detection alert — no audio asset
// needed. The AudioContext is created lazily and must be resumed from inside a
// user gesture (the sound toggle calls primeAudio()), after which programmatic
// beeps are allowed to play.

let ctx: AudioContext | null = null;

function audioContext(): AudioContext | null {
  if (typeof window === "undefined") return null;
  if (!ctx) {
    const AC = window.AudioContext || (window as unknown as { webkitAudioContext?: typeof AudioContext }).webkitAudioContext;
    if (!AC) return null;
    ctx = new AC();
  }
  return ctx;
}

// primeAudio resumes a suspended context from within a user gesture so later
// programmatic beeps are permitted by the browser autoplay policy.
export function primeAudio(): void {
  const c = audioContext();
  if (c && c.state === "suspended") void c.resume();
}

// playBeep emits a short, low-volume two-tone blip — a discreet alert, not an
// alarm. Safe to call when sound is enabled; it no-ops without WebAudio.
export function playBeep(): void {
  const c = audioContext();
  if (!c) return;
  if (c.state === "suspended") void c.resume();
  const now = c.currentTime;
  const osc = c.createOscillator();
  const gain = c.createGain();
  osc.type = "sine";
  osc.frequency.setValueAtTime(880, now);
  osc.frequency.setValueAtTime(660, now + 0.07);
  gain.gain.setValueAtTime(0.0001, now);
  gain.gain.exponentialRampToValueAtTime(0.12, now + 0.01);
  gain.gain.exponentialRampToValueAtTime(0.0001, now + 0.18);
  osc.connect(gain).connect(c.destination);
  osc.start(now);
  osc.stop(now + 0.2);
}
