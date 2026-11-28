# Tasks

- [x] 1. Review and approve the Conn Lab scope
  - Confirm the acceptance goal and the three-spec split (`web-lab-foundation`, `web-lab-board`, `web-lab-seance`).
  - Confirm that this milestone is read-only apart from the browser session, and that the feature test in `.kiro/steering/product.md` still governs every Lab view.
  - Confirm the 60/30/10 palette (Crypt, Formaldehyde, Voltage) or choose replacements.
  - Do not begin implementation until the specification and decision gate are approved. Requirements: 1, 4, 8.

- [x] 2. Record decision 0013
  - Add `docs/decisions/0013-web-application.md` with the selected UI stack and pinned versions, styling, browser session and CSRF design, deployment boundary, live-update transport, and the statement that local clients keep pull-by-cursor.
  - Mark task 1 of `authenticated-browser-binding-review` complete by reference.
  - Update `docs/decisions/README.md`, `.kiro/steering/tech.md`, and the delivery order in `.kiro/steering/product.md`. Requirements: 1.

- [x] 3. Define the contracts and migration
  - Add the `web-session` schema with accepted and rejected fixtures and Go types.
  - Add the migration that adds `sessions.kind` with the 7-day browser bound; the CSRF token is derived, not stored. Requirements: 2.

- [x] 4. Implement browser sign-in
  - Web authorization-code flow with PKCE and single-use `state`, Human mapping shared with `conn login`, hashed `browser` sessions, the cookie, sign-out revocation, `CONN_PUBLIC_URL` and `CONN_GITHUB_CLIENT_SECRET` configuration with the "not configured" message.
  - Cookie-only authentication path, CSRF middleware, and refusal of cross-use with bearer credentials.
  - Add the parameterized authentication test from the validation plan. Requirements: 2, 8.

- [x] 5. Implement the event stream
  - `NOTIFY` on event append, one `LISTEN` fan-out per instance, the SSE handler with `Last-Event-ID` resume, heartbeat, caps, and `resync`.
  - Add the resume test from the validation plan. Requirements: 7.

- [x] 6. Serve the embedded Lab
  - Add `internal/webui` with the committed placeholder page, SPA fallback, cache headers, and CSP.
  - Add `apps/web` to the pnpm workspace with Vite, the dev proxy to `conn server`, Biome, typecheck, and Vitest wired into the root scripts.
  - Report the Lab URL from `conn doctor`. Requirements: 8, 9.

- [x] 7. Build the Lab design system
  - Token file for both themes, self-hosted fonts and icons, theme and "Plain labels" preferences, reduced-motion handling, stitch, bolt, and lightning primitives.
  - Add the token contrast test. Requirements: 4.

- [x] 8. Build the app shell
  - Sign-in page, Project switcher, navigation rail and bottom bar, command palette, `g` shortcuts and help sheet, Account page with the only visible identifiers.
  - Name directory with event patching and labelled fallbacks. Requirements: 3, 5.

- [x] 9. Build the Lab overview and the Lightning feed
  - Overview figures from status, radar, and insights, each linked, with the as-of cursor and the empty state.
  - Feed panel with one sentence per event type, family filters, delayed-feed fallback, and query invalidation from the stream.
  - Add the feed sentence fixtures. Requirements: 6, 7.

- [x] 10. Update documentation
  - README section "Conn Lab" (build, configure the GitHub OAuth callback and secret, run locally), architecture diagram update, and `docs/code-structure.md` entries for `apps/web` and `internal/webui`. Requirements: 9.

- [ ] 11. Run the real trial
  - Two members in the Lab, one participant in Kiro publishing a colliding Intent; confirm the feed sentence arrives live, the overview updates without refresh, and a stream drop resumes with no gap.
  - Do keyboard-only, 360 px, reduced-motion, and both-theme passes, and record findings in `docs/real-trial.md`. Requirements: 3, 4, 6, 7.
