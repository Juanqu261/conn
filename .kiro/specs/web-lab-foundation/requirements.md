# Requirements

Milestone W1 of the Conn Lab web application. Acceptance goal: an authenticated Project member opens one browser page and sees, live and by name, what the Project is building, where in-progress work collides, and what is proven, without installing anything and without the page gaining any authority the CLI and `conn app` do not already have.

Conn Lab is the web application deferred by decisions 0001 and 0002. It is a window onto the Conn record, not a development tool and not a replacement for the team's IDE, Git host, CI, or chat. Its theme follows the Kiroween "Frankenstein" category: Conn Lab stitches together a Git host's pull-request page, a kanban board, a team activity feed, and Conn's own collision and evidence model into one app.

The Lab ships as three specifications in this order:

1. `web-lab-foundation` (this spec): architecture decision, browser sign-in, app shell, the Lab design system, the Project overview, and the live feed. It is read-only apart from the browser session.
2. `web-lab-board`: the derived Intent board, the collision board with human decisions, the Intent "Specimen" page, the Stitch Map, Blueprints, and Pulse.
3. `web-lab-seance`: threads attached to Conn records, mentions, the notification inbox, and outbound Slack and Discord bridges. This needs a product-direction amendment.

This milestone depends on the accepted backend `/v1` API, Project event cursors, the radar view, Project insights, and GitHub OAuth sign-in.

## 1. Architecture gate

**User story:** As the project owner, I want the web stack, browser session, deployment boundary, and live-update mechanism chosen and recorded before any browser code exists, so that the Lab follows the same decision discipline as the rest of Conn.

### Acceptance criteria

1. THE SYSTEM SHALL NOT contain browser application code, browser dependencies, or web routes in `conn server` until decision 0013 is accepted
2. Decision 0013 SHALL select the UI framework and build tool, the styling approach, the browser session mechanism, the CSRF defence, the deployment boundary, and the live-update transport, each with exact pinned versions where a dependency is introduced
3. Decision 0013 SHALL record that it satisfies task 1 of `authenticated-browser-binding-review`
4. WHEN decision 0013 is accepted
   THE SYSTEM SHALL update `docs/decisions/README.md`, `.kiro/steering/tech.md`, and the delivery order in `.kiro/steering/product.md` in the same change

## 2. Browser sign-in

**User story:** As a Project member, I want to sign in to Conn Lab with my GitHub account in the browser so that I see exactly what my Conn identity is authorized to see.

### Acceptance criteria

1. WHEN an unauthenticated visitor opens any Lab page
   THE SYSTEM SHALL show only the sign-in page and SHALL NOT disclose any Project, Human, or record data
2. WHEN the visitor chooses "Sign in with GitHub"
   THE SYSTEM SHALL run the GitHub OAuth web authorization-code flow with PKCE and a single-use `state` value bound to the browser, and SHALL map the GitHub account to the same Conn Human that `conn login` maps it to
3. THE SYSTEM SHALL hold the browser's Conn session only in an `HttpOnly`, `Secure`, `SameSite=Lax`, host-only cookie, and SHALL NOT expose it to page scripts, `localStorage`, `sessionStorage`, URLs, or logs
4. THE SYSTEM SHALL store browser sessions as SHA-256 hashes with the session kind `browser`, an absolute expiry selected by decision 0013, and immediate revocation, reusing the existing session model with an added session kind
5. WHEN a browser request uses an unsafe HTTP method
   THE SYSTEM SHALL require both a matching `Origin` and the session's CSRF token, and SHALL reject the request otherwise without side effects
6. WHEN the member chooses "Sign out"
   THE SYSTEM SHALL revoke the browser session server-side and clear the cookie
7. THE SYSTEM SHALL keep the GitHub access token only for the duration of the exchange, as `conn login` does, and SHALL NOT persist it
8. WHEN the GitHub web flow is not configured on the backend
   THE SYSTEM SHALL say so on the sign-in page and name the missing setting, rather than failing silently

## 3. App shell and navigation

**User story:** As a member of several Projects, I want one consistent shell to move between Projects and views quickly from the keyboard or a phone.

### Acceptance criteria

1. THE SYSTEM SHALL provide a Project switcher that lists only Projects the signed-in Human belongs to, and SHALL remember the last Project per browser
2. THE SYSTEM SHALL provide a navigation rail with the Lab views available in the current milestone, and SHALL hide, not disable, views a later milestone delivers
3. THE SYSTEM SHALL provide a command palette opened with `Ctrl+K` or `⌘K` that jumps to any view, Objective, Intent, or collision by name
4. THE SYSTEM SHALL provide `g`-prefixed navigation shortcuts listed on a `?` help sheet, and SHALL disable single-key shortcuts while a text field has focus
5. THE SYSTEM SHALL render every view without horizontal page scroll from 360 px wide upwards, collapsing the rail into a bottom bar below 768 px
6. THE SYSTEM SHALL give every view a stable URL, so that a link pasted into a pull request or chat opens the same view for any authorized member

## 4. The Lab design system

**User story:** As a member, I want the Lab to look like a Kiroween laboratory without the theme ever getting in the way of reading the state of the Project.

### Acceptance criteria

1. THE SYSTEM SHALL define all colours as design tokens and SHALL distribute them by the 60/30/10 rule: about 60% of the visible area in the dominant neutral, 30% in the secondary colour, and 10% or less in the accent
2. THE SYSTEM SHALL use state colours (open, under review, failing, passing) only to convey state, never as decoration, and SHALL always pair a state colour with an icon and a text label
3. THE SYSTEM SHALL meet WCAG 2.2 AA contrast for text and essential graphics in both the dark default ("Lights out") and the light ("Lights on") themes
4. THE SYSTEM SHALL follow the operating-system colour scheme until the member picks a theme, and SHALL remember the choice per browser
5. EVERY themed name (for example "Stitching") SHALL be shown with a plain caption (for example "Needs a decision"), SHALL use the plain wording for accessible names, and SHALL be replaceable by the plain wording alone through a "Plain labels" preference
6. WHEN the operating system requests reduced motion
   THE SYSTEM SHALL replace every animation, including lightning and flicker effects, with an instant state change, and SHALL never flash more than three times per second in any mode
7. THE SYSTEM SHALL self-host every font and icon and SHALL load no third-party script, stylesheet, font, analytics, or tracking resource

## 5. Names, not identifiers

**User story:** As a human reader, I want records named the way people know them, because UUIDs tell me nothing.

### Acceptance criteria

1. THE SYSTEM SHALL present Projects by name, Humans by `@login`, Objectives and Cells by title, Intents by the first line of their summary, repositories by remote, and collisions by rule plus the two Intent names
2. THE SYSTEM SHALL show identifiers only on the Account page and through an explicit "Copy ID" menu action
3. WHEN a name cannot be resolved
   THE SYSTEM SHALL show a short labelled fallback (for example "Unknown Intent") and SHALL NOT show the raw identifier inline

## 6. The Lab overview

**User story:** As a member, I want one page that tells me the state of the Project in under a minute.

### Acceptance criteria

1. THE SYSTEM SHALL show, for the selected Project, the current Objectives with their evidence coverage, the number of active Intents, open and under-review collisions, combination checks that are failing or incomplete, open questions, and known unknowns from the radar view
2. THE SYSTEM SHALL state the event cursor the page reflects and how long ago it was read
3. EACH figure SHALL link to the view that explains it
4. THE SYSTEM SHALL NOT show any per-person count, ranking, activity score, or online status
5. WHEN the Project has no active Intents
   THE SYSTEM SHALL show an empty state that explains how work enters Conn (`/conn-start` or `conn start`) instead of an empty grid

## 7. Lightning feed

**User story:** As a member, I want a live feed of what just happened in the Project so that I learn about a new collision or decision without asking.

### Acceptance criteria

1. THE SYSTEM SHALL stream committed Project events to the browser in cursor order, starting after the cursor the page last rendered
2. WHEN the stream reconnects
   THE SYSTEM SHALL resume after the last delivered cursor, and SHALL never skip or reorder events
3. WHEN the stream is unavailable
   THE SYSTEM SHALL fall back to cursor pulls of the existing events endpoint while the tab is visible, and SHALL show that the feed is delayed
4. THE SYSTEM SHALL render each event as one human sentence using names (for example "@ana's “Retry failed payments” now collides with @luis's “Refund flow” on `changes contract:PaymentGateway`")
5. THE SYSTEM SHALL let a member filter the feed by event family (Intents, collisions and decisions, evidence, planning)
6. THE SYSTEM SHALL update every open view from the same stream, so that no view needs a manual refresh to show a committed change
7. THE SYSTEM SHALL bound open streams per browser session and per backend instance, and SHALL send a heartbeat often enough to keep idle proxies from closing the stream

## 8. Authority and data boundaries

**User story:** As a Project owner, I want the web app to add visibility, not new ways for an agent or a stolen link to act.

### Acceptance criteria

1. THE SYSTEM SHALL authorize every Lab read with the same Project membership rules as the `/v1` API, server-side
2. In this milestone THE SYSTEM SHALL perform no write other than creating and revoking the browser session
3. THE SYSTEM SHALL NOT accept a Conn CLI, MCP, or CI credential as a browser session, and SHALL NOT accept a browser session cookie as a bearer credential
4. THE SYSTEM SHALL show no source code, diff, prompt, transcript, check log, or credential, because the backend holds none
5. THE SYSTEM SHALL send a Content-Security-Policy that allows scripts, styles, fonts, and connections only from the Lab's own origin

## 9. Delivery and operation

**User story:** As someone running `conn server`, I want the Lab to come with the backend I already run, with no extra service to deploy.

### Acceptance criteria

1. THE SYSTEM SHALL serve the Lab from `conn server` on the same origin as `/v1`, as decision 0013 selects
2. WHEN `conn server` was built without the web assets
   THE SYSTEM SHALL serve the API unchanged and SHALL answer Lab routes with a short page explaining how to build the Lab
3. THE SYSTEM SHALL provide a local development mode with live reload against a natively running `conn server`
4. `pnpm check`, `pnpm test`, and `pnpm build` SHALL include the Lab
5. `conn doctor` SHALL report the Lab URL when the configured backend serves it
