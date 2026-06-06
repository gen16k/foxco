import type { Stats, EventPage, EventRow, Meta, ReasonCount, SeriesPoint } from "./schemas";

// Mock mode lets the admin console run WITHOUT a proxy backend, so you can preview
// the UI without starting (and routing Claude Code through) the proxy. Enable it
// with USE_MOCK=1 (env or web/.env.local). meta reports backend "mock". Block
// prompts show the FULL text (no masking — the proxy itself never masks) using
// canonical, well-known fake example values (e.g. AWS docs' AKIA...EXAMPLE key),
// so the detection highlight is visible without any real secret in the dataset.
export function mockEnabled(): boolean {
  return process.env.USE_MOCK === "1";
}

const WINDOW_MS = 24 * 60 * 60 * 1000;
const TOTAL = 80;

const ALLOW_PROMPTS = [
  "please refactor this function to be more readable",
  "explain how a hashmap works in Go",
  "write a unit test for the add() helper",
  "what's the time complexity of quicksort?",
  "help me debug this for loop that never terminates",
  "summarize the difference between TCP and UDP",
  "convert this callback code to async/await",
  "what does this regular expression match?",
];

interface BlockKind {
  reason: string;
  source: string;
  prompt: string; // full prompt text (unmasked, as the proxy stores it)
  snippet: string; // the offending span the proxy flagged; must be a substring of prompt
}
// Each prompt embeds a canonical fake credential and `snippet` is the exact
// offending span, so the detail drawer can highlight it inside the full text.
const BLOCK_KINDS: BlockKind[] = [
  {
    reason: "secret detected (aws_access_key)",
    source: "rule",
    prompt: "deploy the staging stack with AWS key AKIAIOSFODNN7EXAMPLE to the artifacts bucket, then restart the workers",
    snippet: "AKIAIOSFODNN7EXAMPLE",
  },
  {
    reason: "secret detected (anthropic_key)",
    source: "rule",
    prompt: "set ANTHROPIC_API_KEY=sk-ant-api03-EXAMPLEexampleEXAMPLEexample0000 and rerun the eval suite",
    snippet: "sk-ant-api03-EXAMPLEexampleEXAMPLEexample0000",
  },
  {
    reason: "secret detected (github_token)",
    source: "rule",
    prompt: "CI keeps failing to push — the token is ghp_EXAMPLEexample0123456789ABCDEFexample01, can you check the scopes?",
    snippet: "ghp_EXAMPLEexample0123456789ABCDEFexample01",
  },
  {
    reason: "database credential in prompt",
    source: "lfm",
    prompt: "write a migration for postgres://admin:Sup3rS3cret!@10.0.4.12:5432/prod that adds an index on orders(created_at)",
    snippet: "postgres://admin:Sup3rS3cret!@10.0.4.12:5432/prod",
  },
  {
    reason: "personal email address",
    source: "lfm",
    prompt: "draft a reply and send the signed contract to john.doe@example.com before end of day",
    snippet: "john.doe@example.com",
  },
  {
    reason: "internal hostname / private IP",
    source: "lfm",
    prompt: "ssh into the prod box at 10.0.12.47 and tail the gateway logs for the last hour",
    snippet: "10.0.12.47",
  },
  {
    reason: "classifier unavailable",
    source: "classifier_unavailable",
    prompt: "(blocked: classifier timed out — fail closed; the live turn was not classified)",
    snippet: "",
  },
];

function rfc3339(ms: number): string {
  return new Date(ms).toISOString().replace(/\.\d{3}Z$/, "Z");
}

// buildDataset returns a deterministic set of events spanning the last 24h,
// newest first, relative to `now`.
function buildDataset(now: number): EventRow[] {
  const rows: EventRow[] = [];
  for (let i = 0; i < TOTAL; i++) {
    const createdAt = rfc3339(now - Math.floor((i / TOTAL) * WINDOW_MS) - ((i * 137) % 60000));
    const isBlock = i % 10 < 3; // ~30% blocks, deterministic
    const countTokens = i % 7 === 0;
    const path = countTokens ? "/v1/messages/count_tokens" : "/v1/messages";
    const id = `mock_${String(i).padStart(4, "0")}`;
    if (isBlock) {
      const k = BLOCK_KINDS[i % BLOCK_KINDS.length];
      rows.push({
        eventId: id,
        createdAt,
        decision: "BLOCK",
        source: k.source,
        reason: k.reason,
        latencyMs: 30 + ((i * 17) % 140),
        modelName: "mock-data",
        backend: "mock",
        upstreamCalled: false,
        path,
        promptText: k.prompt,
        matchedSnippet: k.snippet || null,
      });
    } else {
      rows.push({
        eventId: id,
        createdAt,
        decision: "ALLOW",
        source: "",
        reason: "",
        latencyMs: 80 + ((i * 53) % 900),
        modelName: "mock-data",
        backend: "mock",
        upstreamCalled: true,
        path,
        promptText: ALLOW_PROMPTS[i % ALLOW_PROMPTS.length],
        matchedSnippet: null,
      });
    }
  }
  return rows;
}

function inRange(rows: EventRow[], from: string | null, to: string | null): EventRow[] {
  let out = rows;
  if (from) out = out.filter((r) => r.createdAt >= from);
  if (to) out = out.filter((r) => r.createdAt <= to);
  return out;
}

function percentile(xs: number[], p: number): number {
  if (!xs.length) return 0;
  const s = [...xs].sort((a, b) => a - b);
  const idx = Math.min(s.length - 1, Math.max(0, Math.ceil(p * s.length) - 1));
  return s[idx];
}

export function mockMeta(): Meta {
  return {
    storeRawText: true,
    retentionDays: 30,
    model: "mock-data",
    backend: "mock",
    listenAddr: "mock (no proxy)",
    startedAt: rfc3339(Date.now()),
  };
}

// A synthetic "just-detected" block whose id rolls over every 20s, so the live
// alert (toast + panel + optional beep) visibly fires in mock mode without a
// real proxy. It is added only to the events feed/detail, NOT to mockStats, so
// the KPI numbers don't flicker — the point is to demo the alert, not the counts.
function liveBucket(now: number): number {
  return Math.floor(now / 20000);
}

function liveBlockFor(bucket: number): EventRow {
  const k = BLOCK_KINDS[bucket % BLOCK_KINDS.length];
  return {
    eventId: `mock_live_${bucket}`,
    createdAt: rfc3339(bucket * 20000),
    decision: "BLOCK",
    source: k.source,
    reason: k.reason,
    latencyMs: 40 + (bucket % 120),
    modelName: "mock-data",
    backend: "mock",
    upstreamCalled: false,
    path: "/v1/messages",
    promptText: k.prompt,
    matchedSnippet: k.snippet || null,
  };
}

export function mockEvents(params: URLSearchParams): EventPage {
  const now = Date.now();
  const decision = params.get("decision");
  let dataset = buildDataset(now);
  if (!decision || decision === "BLOCK") {
    dataset = [liveBlockFor(liveBucket(now)), ...dataset];
  }
  let rows = inRange(dataset, params.get("from"), params.get("to"));
  const source = params.get("source");
  const q = (params.get("q") || "").toLowerCase();
  if (decision) rows = rows.filter((r) => r.decision === decision);
  if (source) rows = rows.filter((r) => r.source === source);
  if (q) rows = rows.filter((r) => (r.reason + " " + (r.promptText || "")).toLowerCase().includes(q));

  const limit = Math.min(500, Math.max(1, parseInt(params.get("limit") || "50", 10) || 50));
  const offset = Math.max(0, parseInt(params.get("offset") || "0", 10) || 0);
  return { total: rows.length, events: rows.slice(offset, offset + limit) };
}

export function mockEvent(id: string): EventRow | null {
  if (id.startsWith("mock_live_")) {
    const bucket = parseInt(id.slice("mock_live_".length), 10);
    return isNaN(bucket) ? null : liveBlockFor(bucket);
  }
  return buildDataset(Date.now()).find((r) => r.eventId === id) ?? null;
}

export function mockStats(params: URLSearchParams): Stats {
  const now = Date.now();
  const from = params.get("from");
  const to = params.get("to");
  const rows = inRange(buildDataset(now), from, to);

  let blocked = 0;
  let upstreamCalled = 0;
  const bySource: Record<string, number> = {};
  const reasonMap: Record<string, number> = {};
  const latencies: number[] = [];
  for (const r of rows) {
    latencies.push(r.latencyMs);
    if (r.upstreamCalled) upstreamCalled++;
    if (r.decision === "BLOCK") {
      blocked++;
      const s = r.source || "other";
      bySource[s] = (bySource[s] || 0) + 1;
      if (r.reason) reasonMap[r.reason] = (reasonMap[r.reason] || 0) + 1;
    }
  }
  const total = rows.length;
  const topReasons: ReasonCount[] = Object.entries(reasonMap)
    .map(([reason, count]) => ({ reason, count }))
    .sort((a, b) => b.count - a.count)
    .slice(0, 15);
  const avgLatencyMs = latencies.length ? latencies.reduce((a, b) => a + b, 0) / latencies.length : 0;

  return {
    total,
    blocked,
    allowed: total - blocked,
    blockRate: total ? blocked / total : 0,
    upstreamCalled,
    bySource,
    topReasons,
    avgLatencyMs,
    p95LatencyMs: percentile(latencies, 0.95),
    series: buildSeries(rows, from, to, now),
  };
}

function buildSeries(rows: EventRow[], from: string | null, to: string | null, now: number): SeriesPoint[] {
  const hi = to ? Date.parse(to) : now;
  const lo = from ? Date.parse(from) : now - WINDOW_MS;
  if (!isFinite(hi) || !isFinite(lo) || hi <= lo) return [];
  const bucketMs = hi - lo <= 48 * 3600 * 1000 ? 3600 * 1000 : 24 * 3600 * 1000;
  const buckets = new Map<number, { allow: number; block: number }>();
  for (const r of rows) {
    const t = Date.parse(r.createdAt);
    if (!isFinite(t)) continue;
    const key = Math.floor(t / bucketMs) * bucketMs;
    const b = buckets.get(key) || { allow: 0, block: 0 };
    if (r.decision === "BLOCK") b.block++;
    else b.allow++;
    buckets.set(key, b);
  }
  const out: SeriesPoint[] = [];
  const start = Math.floor(lo / bucketMs) * bucketMs;
  const end = Math.floor(hi / bucketMs) * bucketMs;
  for (let t = start; t <= end; t += bucketMs) {
    const b = buckets.get(t) || { allow: 0, block: 0 };
    out.push({ ts: rfc3339(t), allow: b.allow, block: b.block });
  }
  return out;
}
