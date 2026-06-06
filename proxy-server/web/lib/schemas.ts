import { z } from "zod";

// These zod schemas are the single source of truth for the admin API contract
// (camelCase, mirroring the Go JSON tags). They both validate responses at the
// BFF boundary and produce the TypeScript types used across the UI. Numeric and
// collection fields default so an empty database yields zeros, not crashes.

export const SeriesPointSchema = z.object({
  ts: z.string(),
  allow: z.number().default(0),
  block: z.number().default(0),
});

export const ReasonCountSchema = z.object({
  reason: z.string(),
  count: z.number().default(0),
});

export const StatsSchema = z.object({
  total: z.number().default(0),
  blocked: z.number().default(0),
  allowed: z.number().default(0),
  blockRate: z.number().default(0),
  upstreamCalled: z.number().default(0),
  bySource: z.record(z.string(), z.number()).default({}),
  topReasons: z.array(ReasonCountSchema).default([]),
  avgLatencyMs: z.number().default(0),
  p95LatencyMs: z.number().default(0),
  series: z.array(SeriesPointSchema).default([]),
});

export const EventRowSchema = z.object({
  eventId: z.string(),
  createdAt: z.string(),
  // Tolerate any decision string; the UI normalizes (BLOCK vs everything else).
  decision: z.string().default(""),
  source: z.string().default(""),
  reason: z.string().default(""),
  latencyMs: z.number().default(0),
  modelName: z.string().default(""),
  backend: z.string().default(""),
  upstreamCalled: z.boolean().default(false),
  path: z.string().default(""),
  promptText: z.string().nullable().default(null),
  matchedSnippet: z.string().nullable().default(null),
});

export const EventPageSchema = z.object({
  total: z.number().default(0),
  events: z.array(EventRowSchema).default([]),
});

export const MetaSchema = z.object({
  storeRawText: z.boolean().default(false),
  retentionDays: z.number().default(0),
  model: z.string().default(""),
  backend: z.string().default(""),
  listenAddr: z.string().default(""),
  startedAt: z.string().default(""),
});

export type SeriesPoint = z.infer<typeof SeriesPointSchema>;
export type ReasonCount = z.infer<typeof ReasonCountSchema>;
export type Stats = z.infer<typeof StatsSchema>;
export type EventRow = z.infer<typeof EventRowSchema>;
export type EventPage = z.infer<typeof EventPageSchema>;
export type Meta = z.infer<typeof MetaSchema>;

export interface ApiError {
  error: string;
  message: string;
}
