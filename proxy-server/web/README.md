# FoxCo Admin UI

A Grafana-style, read-only admin dashboard for the Local LFM DLP Proxy. It shows
detection (BLOCK) counts and contents plus **all prompt history**, served from the
proxy's local SQLite audit log.

- **Stack:** Next.js 14 (App Router, TypeScript), Tailwind CSS, Recharts, SWR,
  iron-session.
- **Data path:** the browser only talks to this app (same-origin). Next.js
  **Route Handlers** (`app/api/admin/*`) fetch the Go proxy's read-only admin API
  **server-side** (`PROXY_ADMIN_BASE_URL`, default `http://127.0.0.1:8787`), so
  there is no CORS and the Go API stays localhost-only.
- **Auth:** a basic ID/PW login (env-configured) backed by a signed, httpOnly
  session cookie. `middleware.ts` guards every page and `/api/admin/*`.

> The proxy persists raw prompt bodies only when started with
> `storage.store_raw_text: true`. When it is off, the dashboard still shows
> counts/decisions/reasons/latency, and the detail drawer notes that the body was
> not stored.

## Requirements

- Node.js 18.18+ (tested on Node 24).
- The proxy running with `admin.enabled: true` (default). For prompt history,
  start it with `storage.store_raw_text: true`.

## Setup

```powershell
cd proxy-server/web
npm install
copy .env.example .env.local   # then edit .env.local
```

`.env.local`:

| var | meaning |
|-----|---------|
| `PROXY_ADMIN_BASE_URL` | Go admin API base (default `http://127.0.0.1:8787`) |
| `PROXY_ADMIN_TOKEN` | must match the proxy's `admin.auth_token` (empty if none) |
| `ADMIN_USERNAME` / `ADMIN_PASSWORD` | dashboard login — **change these** |
| `SESSION_SECRET` | 32+ char secret for signing the session cookie — **change this** |

## Run

The easiest path is the proxy launcher, which starts the proxy **and** this UI
together (loopback only) and wires the admin token for you:

```powershell
cd ..              # proxy-server
.\start.ps1                         # proxy + admin UI on http://127.0.0.1:3939
.\start.ps1 -NoWeb                  # proxy only
```

Or run the UI standalone against an already-running proxy:

```powershell
npm run dev          # http://127.0.0.1:3939 (bound to loopback)
```

The dev/start scripts bind `127.0.0.1` so the UI is reachable only from this
machine. Open http://127.0.0.1:3939, sign in, and use the time-range picker + auto-refresh.
`/` is the overview (KPIs, ALLOW/BLOCK time series, blocks-by-source, top reasons,
recent events); `/history` is the full, filterable prompt history with a detail
drawer.

## Data flow

```
browser ──(same-origin, session cookie)──▶ Next.js BFF (/api/admin/*)
                                            └─(server-side, optional bearer)─▶ Go /admin/* (127.0.0.1:8787)
                                                                                 └─ reads SQLite audit log
```

## Notes

- This app lives inside the Go module directory. The committed tree has no `.go`
  files under `web/`, so a fresh checkout / CI runs `go build ./...` cleanly. Note
  that after `npm install`, `node_modules` may contain a stray vendored `.go` file
  (e.g. the `flatted` package), which `go ./...` will harmlessly traverse locally;
  `node_modules` is gitignored so this never reaches CI. Never add your own `.go`
  file under `web/`.
- Build: `npm run build`; type-check only: `npm run typecheck`.
