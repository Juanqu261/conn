# Design

## Concept: the chimera

The Kiroween "Frankenstein" category asks for seemingly incompatible pieces stitched into one working body. Conn Lab is that body. Each organ is borrowed from a tool teams already know, and Conn's record is the nervous system that makes them one:

| Organ             | Borrowed from                   | In Conn Lab                                                              | Spec                 |
| ----------------- | ------------------------------- | ------------------------------------------------------------------------ | -------------------- |
| Activity feed     | Slack channel stream            | Lightning feed of committed Project events                               | `web-lab-foundation` |
| Board             | Kanban                          | Intent board whose lanes are **derived** from Conn state, never dragged  | `web-lab-board`      |
| Pull-request page | GitHub                          | Intent "Specimen" page: claims, drift, evidence, review packet, timeline | `web-lab-board`      |
| Dependency graph  | Code-intelligence maps          | Stitch Map: Intents as body parts, collisions as stitches                | `web-lab-board`      |
| Threads           | Slack threads, PR conversations | Séance threads attached only to Conn records                             | `web-lab-seance`     |
| Integrations      | Slack/Discord webhooks          | Outbound bridges that post collisions and decisions where the team talks | `web-lab-seance`     |

The organs are deliberately not the products they come from. Conn's feature test (`.kiro/steering/product.md`) still applies: a Lab feature must reduce rework from concurrent changes or reduce the time to trust an agent's change. That is why lanes are derived rather than assigned, why threads exist only on records, and why nothing shows per-person metrics.

## Decision 0013 (accepted): web application architecture

Accepted on 2026-11-28 with the recommended options below; the record is [`docs/decisions/0013-web-application.md`](../../../docs/decisions/0013-web-application.md), which pins exact versions. The GitHub OAuth client already used by `conn login` serves the web flow too.

The architecture policy requires options, a recommendation, and owner approval before scaffolding. The constraints are the accepted ones: Go `net/http` backend, Node.js with TypeScript for browser-facing development, pnpm workspaces, Biome, Vitest, no new deployment unit unless justified, and the human-authority rules of decision 0001.

### UI framework and build

| Option                                                                               | Shape                                                                                               | For                                                                                                                                                                                                        | Against                                                                                                                                   |
| ------------------------------------------------------------------------------------ | --------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------- |
| **A. React + TypeScript SPA built by Vite, embedded in `conn server` (recommended)** | `apps/web` builds static assets; `internal/webui` embeds them with `go:embed`; same origin as `/v1` | No new runtime in production; same-origin cookies make the session simple; the largest ecosystem for accessible primitives, drag and drop, and graph rendering, all of which the board and Stitch Map need | A client bundle to keep small; SPA routing needs a server fallback route                                                                  |
| B. Next.js as a separate Node service                                                | Server-rendered app that proxies `/v1`                                                              | Server rendering, file routing                                                                                                                                                                             | A second deployment unit and a second session layer for an app that is entirely behind sign-in, where server rendering gains little       |
| C. Go `html/template` + htmx with TypeScript islands                                 | Server-rendered fragments from `conn server`                                                        | One toolchain, almost no client bundle                                                                                                                                                                     | The kanban board, live updates, and graph need substantial client code anyway; contradicts the TypeScript default for browser-facing work |

Recommended libraries for option A, to be pinned to exact versions in the decision record: React 19, Vite, TanStack Router (typed URLs for every view), TanStack Query (cache keyed by event cursor), Radix UI primitives (accessible dialogs, menus, tabs, tooltips), `cmdk` (command palette). The board's `dnd-kit` and the Stitch Map's graph library are chosen in `web-lab-board` and recorded as an amendment, so this milestone does not pull them in.

Styling: plain CSS with custom-property tokens and CSS modules. No utility framework, so the token file in this design is the single source of the 60/30/10 split.

### Browser session

| Option                                                                                                           | For                                                                                                        | Against                                                                                      |
| ---------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------- |
| **A. GitHub OAuth web authorization-code flow with PKCE; backend sets a `__Host-conn_web` cookie (recommended)** | Standard browser flow; the token never reaches page scripts; reuses the existing session table and hashing | Needs `CONN_GITHUB_CLIENT_SECRET` and a registered callback URL on the same GitHub OAuth app |
| B. Device flow inside the browser                                                                                | No client secret                                                                                           | Awkward copy-a-code step in a browser that can simply redirect                               |
| C. Bearer token kept in `localStorage`                                                                           | Simple                                                                                                     | Any XSS steals a 30-day credential; rejected                                                 |

Session parameters: kind `browser`, 7-day absolute expiry, revocable, cookie `__Host-conn_web; HttpOnly; Secure; SameSite=Lax; Path=/`. Browsers treat `http://127.0.0.1` and `localhost` as secure contexts, so the same cookie works in local development.

CSRF: unsafe methods require an exact `Origin` match against `CONN_PUBLIC_URL` and an `X-Conn-CSRF` header equal to the session's CSRF token returned by `GET /v1/web/session`. The token is derived as an HMAC of a fixed label keyed by the session token, so it needs no storage and only the cookie holder can compute it. `SameSite=Lax` is defence in depth, not the defence.

### Deployment boundary

`conn server` serves `/v1/*` as today, plus:

- `GET /` and every non-`/v1` path → the embedded `index.html` (SPA fallback), hashed assets under `/assets/` with immutable caching;
- `GET /v1/auth/github/web/start`, `GET /v1/auth/github/web/callback`, `GET /v1/web/session`, `DELETE /v1/web/session`;
- `GET /v1/projects/{project_id}/events/stream`.

New configuration: `CONN_PUBLIC_URL` (the origin users type, used for the OAuth callback and the `Origin` check) and `CONN_GITHUB_CLIENT_SECRET`. Both are treated as unset when empty, as with other Conn variables.

The embed directory holds a committed placeholder `index.html` that explains how to build the Lab, so `go build ./...` works without a Node build and requirement 9.2 holds.

### Live updates

| Option                                                                                                          | For                                                                                                                                      | Against                                                                 |
| --------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------- |
| **A. Server-Sent Events from `conn server`, woken by PostgreSQL `LISTEN/NOTIFY` on event append (recommended)** | One-way stream fits a feed exactly; `Last-Event-ID` gives gap-free resume over the existing cursors; works through ordinary HTTP proxies | Long-lived connections per tab; needs connection caps                   |
| B. Browser polls `GET /events?after=` every few seconds                                                         | No new server machinery                                                                                                                  | Constant load and a visible delay; poor fit for the "it's alive" moment |
| C. WebSockets                                                                                                   | Bidirectional                                                                                                                            | The Lab sends nothing upstream on the stream; more protocol for no gain |

The accepted rule "no background polling timer" constrains local clients (`conn mcp`, hooks) and stays unchanged. The decision record must state that the browser stream is a server-side push to authenticated browsers only and changes nothing for local clients.

Stream contract: `text/event-stream`; each event is an unnamed message with `id: <cursor>` and `data: <event-envelope JSON>` (unnamed, so event types added later need no client change); a `: heartbeat` comment every 25 s; the server reads from `max(after, Last-Event-ID)`, sends any backlog in order, then waits on `NOTIFY conn_events, '<project_id>:<cursor>'`. Caps: 4 streams per browser session, a configurable total per instance (default 500), backlog of at most 1,000 events per connect; beyond that the client receives `event: resync` and reloads views from the read models.

## Lab design system

### Palette (60 / 30 / 10)

The Lab is a laboratory at night: the crypt is the room, formaldehyde-green glass is the apparatus, and voltage is the lightning that brings things to life.

| Share   | Role                        | Token             | Dark ("Lights out", default) | Light ("Lights on")   | Used for                                                             |
| ------- | --------------------------- | ----------------- | ---------------------------- | --------------------- | -------------------------------------------------------------------- |
| **60%** | Dominant neutral: **Crypt** | `--crypt-bg`      | `#0F1214`                    | `#F5F2E9` (bone)      | Page background                                                      |
|         |                             | `--crypt-surface` | `#161B1E`                    | `#FFFDF8`             | Panels, lane bodies                                                  |
|         |                             | `--crypt-raised`  | `#1E2529`                    | `#FFFFFF`             | Cards, dialogs                                                       |
|         |                             | `--ink`           | `#E9E6DC` (bone)             | `#1A1F1C`             | Primary text                                                         |
|         |                             | `--ink-muted`     | `#A3A69C`                    | `#55605A`             | Secondary text                                                       |
| **30%** | Secondary: **Formaldehyde** | `--formal-700`    | `#2E4D43`                    | `#2E4D43`             | Navigation rail, lane headers, table headers, selected rows          |
|         |                             | `--formal-ink`    | `#B9C7BE`                    | `#F5F2E9`             | Text on formaldehyde                                                 |
|         |                             | `--stitch`        | `#5E8C7A`                    | `#4F7A6A`             | Borders, dividers, the dashed "stitch" lines                         |
| **10%** | Accent: **Voltage**         | `--voltage`       | `#C8FF3D`                    | `#C8FF3D` (fill only) | Primary button, focus ring, live indicator, "Alive" lane, brand bolt |
|         |                             | `--voltage-ink`   | `#C8FF3D`                    | `#3F6B00`             | Accent text and links                                                |

State colours sit outside the 60/30/10 budget and appear only on state badges, lane markers, and graph edges, always with an icon and a label:

| State                   | Name    | Dark      | Light ink |
| ----------------------- | ------- | --------- | --------- |
| Open collision, warning | Pumpkin | `#FF8A3D` | `#B34A00` |
| Under review            | Bruise  | `#B49CFF` | `#6A4BD1` |
| Failing, stale, refused | Blood   | `#FF6B6E` | `#C0262D` |
| Passing, alive          | Voltage | `#C8FF3D` | `#3F6B00` |

Measured WCAG contrast (text needs 4.5:1, graphics 3:1): bone on crypt 15.1, muted on surface 7.0, text on formaldehyde 7.5 (5.3 for muted), crypt on voltage 16.0, pumpkin/bruise/blood on raised 6.6/6.8/5.6; light theme ink 14.9, muted 6.4, voltage-ink link 5.7, state inks 5.3 to 5.8, stitch graphics 4.3 to 4.6. A token test (below) keeps these from regressing.

How the ratio holds in practice: the crypt neutrals fill the page and card bodies; formaldehyde is the rail, headers, and every border; voltage is reserved for at most one primary action per view, focus, and "alive" moments. Two primary buttons on one screen is a design bug.

### Typography and motifs

- UI text: Inter (self-hosted, variable). References, subjects, branches: JetBrains Mono. Wordmark and empty-state headings only: a display face with a hand-stitched feel (candidates: Creepster or Butcherman, both SIL OFL), never for data.
- **Stitches**: dashed `--stitch` borders, used for the Lab's one structural metaphor: things that are joined. Collision pairs are stitched together; a resolved stitch becomes a solid line.
- **Bolts**: two small neck-bolt dots on a card's edge mark an Intent that has an open collision.
- **Lightning**: the live indicator and the "It's alive!" moment (in `web-lab-board`). A single 400 ms flash at most, never repeating, replaced by an instant change under `prefers-reduced-motion`.
- **Tombstones**: released Intents ("Buried").
- **Specimen jars**: evidence.
- Empty states speak in the theme ("The slab is empty. No Intent is being assembled yet.") and always end with the plain next step.

### Themed and plain names

| Themed           | Plain caption (and accessible name) |
| ---------------- | ----------------------------------- |
| The Lab          | Overview                            |
| Lightning feed   | Activity                            |
| Board / The Slab | Work board                          |
| Specimen         | Intent                              |
| Stitch           | Collision                           |
| Stitch Map       | Collision graph                     |
| Séance           | Discussion                          |
| Bell tower       | Notifications                       |
| Pulse            | Project insights                    |

The "Plain labels" preference (per browser, `localStorage` with a safe fallback) replaces themed names entirely.

## Information architecture

```text
┌───────┬──────────────────────────────────────────────────────────┬───────────────┐
│ ⚡conn │  Payments Revamp ▾          ⌘K Search…           @ana ◐  │               │
│  lab  ├──────────────────────────────────────────────────────────┤  LIGHTNING    │
│       │  THE LAB · Overview              as of event 1,284 · 12s │  FEED         │
│ ▣ Lab │                                                          │  ● live       │
│ ▦ Board│ ┌ Objectives ───────────────┐ ┌ Stitches ──────────────┐│               │
│ ✶ Map │ │ Checkout retries   7/9 ▰▰▰▱│ │ ● 3 open  ◆ 1 review   ││ ⚡ @luis's    │
│ ♥ Pulse│ │ Refund flow        2/6 ▰▱▱▱│ │ ✖ 1 pair check failing ││ “Refund flow” │
│ 🔔     │ └───────────────────────────┘ └────────────────────────┘│ now collides… │
│       │ ┌ Specimens on the slab ────┐ ┌ Known unknowns ────────┐│               │
│       │ │ 6 active · 2 alive        │ │ 2 Intents not analyzed ││ ✔ evidence    │
│       │ └───────────────────────────┘ └────────────────────────┘│ passing on …  │
└───────┴──────────────────────────────────────────────────────────┴───────────────┘
 rail = formaldehyde (30%)     page = crypt (60%)       voltage = ⚡, live dot, focus
```

The feed is a right-hand panel on wide screens, a tab on narrow ones. Routes (typed in TanStack Router):

- `/` → last Project, `/p/{projectSlug}` overview, `/p/{projectSlug}/feed`, `/account`, `/sign-in`.
- Later milestones add `/board`, `/stitches`, `/specimens/{intent}`, `/map`, `/blueprints`, `/pulse`, `/inbox`.

URLs carry IDs (they must be stable) but the page never prints them; the Project segment is the ID with a cosmetic name prefix (`payments-revamp~0190…`), and the router ignores the prefix.

## Data flow

```text
Browser ──cookie──▶ conn server ──▶ /v1 read models (status, radar, insights, members, intents)
   ▲                    │
   └── SSE stream ◀─────┘◀── NOTIFY conn_events ◀── event append (same transaction as today)
```

- TanStack Query caches each read model keyed by Project and the cursor it reflects. A streamed event invalidates the queries whose read model it affects (a static map from event type to query keys); there is no client-side re-derivation of domain state.
- A name directory per Project (members, Objectives, Intents, repositories) is loaded once and patched from events, the web counterpart of `internal/tui/names.go`.
- The feed sentence for each event type is one pure TypeScript function per type, with a fixture per type.

## Backend changes

- `internal/auth`: web flow start and callback, PKCE verifier and `state` in a short-lived signed cookie, session kind `browser`, CSRF token per session.
- `internal/httpapi`: cookie authentication path that accepts only `browser` sessions, CSRF middleware on unsafe methods for cookie-authenticated requests, the stream handler, and refusal of browser cookies on bearer-only routes (CI and MCP).
- `internal/postgres`: `NOTIFY` in the event-append transaction; one `LISTEN` connection per instance fanning out to streams.
- `internal/webui`: embedded assets, SPA fallback, cache headers, CSP: `default-src 'self'; img-src 'self' https://avatars.githubusercontent.com; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self' https://github.com`.
- Contracts: `web-session` schema (Human, CSRF token, expiry). Existing read schemas are reused unchanged.
- Migration: add `sessions.kind` (`cli` default for existing rows, or `browser`), with a check that `browser` sessions expire within 7 days; the existing 30-day bound still applies to `cli`.
- Identity: the web callback exchanges the code for a GitHub token and passes it to the same `Auth.Exchange` the `POST /v1/auth/github/exchange` route uses, then discards it.

GitHub avatars are the one external image source. The decision record should confirm this, or proxy them through `conn server`.

## Validation plan

Per decision 0004, a few tests on public guarantees only:

- Go, parameterized: browser cookie accepted only on cookie routes; bearer tokens refused as cookies and the reverse; unsafe request without matching `Origin` and CSRF token has no effect; sign-out revokes; unauthenticated Lab routes leak nothing but the sign-in page.
- Go: stream resumes after `Last-Event-ID` with no gap or reorder across a reconnect, and sends `resync` beyond the backlog cap.
- Vitest: feed sentence per event type from the shared fixtures, names-not-IDs fallback, token contrast check that fails if any pair in the palette table drops below its threshold.
- Manual: keyboard-only pass, 360 px pass, reduced-motion pass, and one real trial with two members watching the feed while a third publishes a colliding Intent from Kiro.
