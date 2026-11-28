# 0013: Web application (Conn Lab)

- Status: accepted on 2026-11-28 by the project owner, with the `web-lab-foundation` specification.
- Owner: project owner
- Amends: [0001](0001-product-model-and-human-authority.md) (records the web architecture that browser review requires), [0002](0002-runtime-and-interaction-surfaces.md) (the deferred web application)
- Satisfies: task 1 of `.kiro/specs/authenticated-browser-binding-review`

## Context

Decisions 0001 and 0002 deferred a web application until the protocol was validated. The protocol now carries the full work loop: Intents, claims, collisions, decisions, evidence, review packets, combination checks, and insights. What is missing is one place where a team sees that state live without a terminal, plus the authenticated browser page that decision 0001 requires for binding human actions.

The constraints are the accepted ones: a Go `net/http` backend, Node.js with TypeScript for browser-facing work, pnpm workspaces, Biome, Vitest, no new deployment unit unless justified, and the human-authority rules of decision 0001.

## Decision

1. **UI: a React and TypeScript single-page application built by Vite and embedded in `conn server`.**

   | Option                                                       | Advantages                                                                                                      | Disadvantages                                                               | Operational cost    |
   | ------------------------------------------------------------ | --------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------- | ------------------- |
   | React SPA built by Vite, embedded with `go:embed` (selected) | No new production runtime; same-origin cookies; accessible primitives, drag and drop, and graph libraries exist | Client bundle to keep small; SPA fallback route                             | One Node build step |
   | Next.js as a separate Node service                           | Server rendering, file routing                                                                                  | Second deployment unit and session layer for an app entirely behind sign-in | A second service    |
   | Go `html/template` with htmx and TypeScript islands          | One toolchain, small bundle                                                                                     | Board, live updates, and graph need substantial client code anyway          | None                |

   The package is `apps/web` (`@conn/web`); `internal/webui` embeds its build output. The embed directory keeps a committed placeholder `index.html`, so `go build ./...` works without a Node build and a server built that way serves the API unchanged.

   Pinned versions, each the newest release at least two weeks old on the decision date:

   | Package                               | Version  |
   | ------------------------------------- | -------- |
   | `react`, `react-dom`                  | 19.3.1   |
   | `@types/react`, `@types/react-dom`    | 19.3.0   |
   | `vite`                                | 8.3.8    |
   | `@vitejs/plugin-react`                | 6.3.0    |
   | `@tanstack/react-router`              | 1.170.46 |
   | `@tanstack/react-query`               | 5.107.1  |
   | `radix-ui`                            | 1.8.1    |
   | `cmdk`                                | 1.1.1    |
   | `@fontsource-variable/inter`          | 5.3.0    |
   | `@fontsource-variable/jetbrains-mono` | 5.3.0    |
   | `@fontsource/creepster`               | 5.3.0    |

   Routes are defined in code, so the router's build plugin is not used. Vite never inlines assets (`assetsInlineLimit: 0`), so fonts and images stay same-origin files the Content-Security-Policy allows. Workspace tooling stays as accepted: TypeScript 7.0.2, Biome, and Vitest. Drag and drop, graph, and Markdown libraries are not selected here; `web-lab-board` amends this record when it introduces them.

2. **Styling: plain CSS with custom-property tokens and CSS modules, no utility framework.** The Lab palette follows the 60/30/10 rule: Crypt neutrals (`#0F1214`, `#161B1E`, `#1E2529`; light `#F5F2E9`) about 60%, Formaldehyde green (`#2E4D43`, stitch `#5E8C7A`) about 30%, Voltage lime (`#C8FF3D`) 10% or less. State colours (Pumpkin, Bruise, Blood, Voltage) convey state only and always come with an icon and a label. A Vitest check keeps every pair in the design's contrast table at WCAG 2.2 AA. Fonts and icons are bundled; the Lab loads no third-party script, stylesheet, or font.

3. **Browser session: the GitHub OAuth web flow on the existing OAuth client, and a `browser` session kind.**

   | Option                                                          | Advantages                                                 | Disadvantages                              |
   | --------------------------------------------------------------- | ---------------------------------------------------------- | ------------------------------------------ |
   | Authorization-code flow with PKCE, server-set cookie (selected) | Token never reaches page scripts; reuses the session model | Needs the client secret and a callback URL |
   | Device flow in the browser                                      | No client secret                                           | Copy-a-code step where a redirect suffices |
   | Bearer token in `localStorage`                                  | Simple                                                     | Any XSS steals the credential; rejected    |
   - The web flow uses the same GitHub client as `conn login` (`CONN_GITHUB_CLIENT_ID`). The server adds `CONN_GITHUB_CLIENT_SECRET`, held only by `conn server`, and `CONN_PUBLIC_URL`, the origin users open. The client's callback URL is `<CONN_PUBLIC_URL>/v1/auth/github/web/callback`. GitHub accepts a different port on a loopback callback, so one registration covers local development on `127.0.0.1`. A team backend on another origin registers its own callback, or its own client when the GitHub client allows only one callback URL. Empty values are treated as unset, and the sign-in page names a missing setting.
   - The callback exchanges the code for a GitHub token, passes it to the same identity exchange `conn login` uses, and discards it. The browser and the CLI therefore map to the same Conn Human.
   - `sessions` gains a `kind` column (`cli` for existing and CLI sessions, `browser`) with a check that browser sessions expire within 7 days. Browser session tokens carry the `conn_web_` prefix, so the bearer path refuses them before any lookup. Browser sessions expire after at most 7 days and keep the existing hashing and immediate revocation. The cookie is `__Host-conn_web; HttpOnly; Secure; SameSite=Lax; Path=/`.
   - Cookie authentication accepts only `browser` sessions, and bearer authentication refuses them. CI and MCP routes never accept a cookie.
   - Unsafe methods on cookie-authenticated requests require an exact `Origin` match with `CONN_PUBLIC_URL` and an `X-Conn-CSRF` header equal to the session's CSRF token, which `GET /v1/web/session` returns. The CSRF token is an HMAC-SHA-256 of a fixed label keyed by the session token: only a holder of the HttpOnly cookie can compute it, it changes with every session, and nothing extra is stored.
   - In this version a browser session reaches only `GET` and `HEAD` on `/v1` routes; every other method returns `browser_read_only`, except sign-out (`DELETE /v1/web/session`), which passes the `Origin` and CSRF checks first. `web-lab-board` lifts this for its reviewed decisions.

4. **Deployment: same origin as `/v1`, served by `conn server`.** Non-`/v1` paths fall back to the embedded `index.html`; hashed assets under `/assets/` are immutable. The Content-Security-Policy is `default-src 'self'; img-src 'self' https://avatars.githubusercontent.com; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self' https://github.com`. GitHub avatars load directly: every member already signs in with GitHub, and proxying adds a cache for no authority benefit. Local development runs the Vite dev server with a proxy to a native `conn server`.

5. **Live updates: Server-Sent Events woken by PostgreSQL `LISTEN/NOTIFY`.**

   | Option                       | Advantages                                         | Disadvantages                       |
   | ---------------------------- | -------------------------------------------------- | ----------------------------------- |
   | SSE with `NOTIFY` (selected) | One-way feed; `Last-Event-ID` resumes over cursors | Long-lived connections need caps    |
   | Browser polling of `/events` | No new server machinery                            | Constant load and visible delay     |
   | WebSockets                   | Bidirectional                                      | The Lab sends nothing on the stream |

   `GET /v1/projects/{project_id}/events/stream` sends each committed event as an unnamed message with its cursor as the SSE `id` and the event envelope as `data`, so event types added later need no client change, a heartbeat every 25 seconds, and at most 1,000 backlog events per connect; a larger backlog gets one named `resync` message (the `web-stream-resync` contract) instead, and the browser reloads its read models. It allows 4 streams per browser session and a configurable instance total (default 500). The event-append transaction issues `NOTIFY conn_events`; one `LISTEN` connection per instance fans out. When the stream fails, the Lab falls back to cursor pulls while the tab is visible.

   This is a server push to authenticated browsers only. `conn mcp`, hooks, the CLI, and `conn app` keep pull-by-cursor with no background polling, as decision 0002 requires.

6. **Testing: Vitest only on the browser side.** No browser end-to-end framework is added. Keyboard, narrow-screen, reduced-motion, theme, and real-trial passes are manual and recorded in `docs/real-trial.md`, per decision 0004.

## Consequences

- Any Project member can see the Project's state live from a URL, with no installation, through the same membership rules as `/v1`.
- `conn server` gains a build-time dependency on the web build for a full release, but `go build` alone still produces a working API server.
- Operators of a team backend configure a GitHub client secret, a callback URL, and the public origin.
- The first Lab milestone writes nothing but sessions. Binding actions arrive with `web-lab-board` and go to existing endpoints with expected-revision preconditions. Agent-initiated browser review stays in `authenticated-browser-binding-review`, which now has its page architecture.

## Validation

Parameterized Go tests cover cookie and bearer separation, CSRF and `Origin` refusal without side effects, sign-out revocation, the sign-in-only response for unauthenticated Lab routes, and gap-free stream resume with `resync` beyond the backlog cap. Vitest covers one feed sentence per event type from shared fixtures, the name fallbacks, and the palette contrast table. A real trial has two members watching the Lab while a Kiro participant publishes a colliding Intent.
