import { z } from "zod";

// Server-side client for the Go admin API. This module must only be imported by
// route handlers (app/api/admin/*) — it carries the admin base URL and bearer
// token, neither of which may reach the browser. The browser only ever talks to
// the same-origin Next BFF, so there is no CORS and the Go API stays localhost.

const BASE = process.env.PROXY_ADMIN_BASE_URL ?? "http://127.0.0.1:8787";
const TOKEN = process.env.PROXY_ADMIN_TOKEN ?? "";

export class ProxyUnreachableError extends Error {}
export class ProxyContractError extends Error {}
export class ProxyStatusError extends Error {
  constructor(
    public status: number,
    message: string,
  ) {
    super(message);
  }
}

export async function getJSON<T>(
  path: string,
  params: URLSearchParams,
  schema: z.ZodType<T>,
): Promise<T> {
  const qs = params.toString();
  const url = `${BASE}${path}${qs ? `?${qs}` : ""}`;
  const ctrl = new AbortController();
  const timer = setTimeout(() => ctrl.abort(), 5000);
  let res: Response;
  try {
    res = await fetch(url, {
      cache: "no-store",
      signal: ctrl.signal,
      headers: TOKEN ? { Authorization: `Bearer ${TOKEN}` } : {},
    });
  } catch (e) {
    throw new ProxyUnreachableError((e as Error)?.message ?? "fetch failed");
  } finally {
    clearTimeout(timer);
  }

  if (!res.ok) {
    const body = await res.text().catch(() => "");
    throw new ProxyStatusError(res.status, body.slice(0, 200));
  }

  let json: unknown;
  try {
    json = await res.json();
  } catch {
    throw new ProxyContractError("upstream returned invalid JSON");
  }

  const parsed = schema.safeParse(json);
  if (!parsed.success) {
    throw new ProxyContractError(parsed.error.issues.map((i) => `${i.path.join(".")}: ${i.message}`).join("; "));
  }
  return parsed.data;
}

// errorResponse maps client errors to a clean JSON response for the browser.
export function errorResponse(e: unknown): Response {
  if (e instanceof ProxyUnreachableError) {
    return Response.json(
      { error: "proxy_unreachable", message: `Cannot reach the proxy on ${BASE}: ${e.message}` },
      { status: 502 },
    );
  }
  if (e instanceof ProxyStatusError) {
    if (e.status === 404) {
      return Response.json({ error: "not_found", message: "event not found" }, { status: 404 });
    }
    if (e.status === 401) {
      return Response.json(
        { error: "admin_token_rejected", message: "proxy rejected the admin token (check PROXY_ADMIN_TOKEN)" },
        { status: 502 },
      );
    }
    return Response.json({ error: "upstream_error", message: `proxy returned ${e.status}` }, { status: 502 });
  }
  if (e instanceof ProxyContractError) {
    return Response.json({ error: "contract_mismatch", message: e.message }, { status: 502 });
  }
  return Response.json({ error: "internal_error", message: String(e) }, { status: 500 });
}

// forward copies an allow-listed set of query params from an incoming request.
export function forward(src: URLSearchParams, keys: string[]): URLSearchParams {
  const out = new URLSearchParams();
  for (const k of keys) {
    const v = src.get(k);
    if (v !== null && v !== "") out.set(k, v);
  }
  return out;
}
