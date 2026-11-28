# Design

## Scope and relationship to existing specs

This milestone turns the Lab from a window into a workbench for the two human-authoritative acts that already exist: deciding a collision and accepting or rejecting one's own Charter. It changes no domain rule. Every write goes to an existing `/v1` endpoint, gated by the browser session, the CSRF defence, and an expected-revision precondition.

Relationship to `authenticated-browser-binding-review`: that spec covers a binding review **initiated by an agent** (URL elicitation from Kiro), with a challenge record. This milestone builds the page that spec needs, with the human navigating to it themselves. The direct path has the same authority as `conn app`, which decision 0001 already accepts as the human-authoritative fallback. The challenge flow then becomes a thin addition: a challenge URL that opens this same dialog, plus status polling from the adapter. It stays a separate spec. The Charter dialog is the first slice that spec recommends. This milestone also delivers collision decisions, because they are the core of the board, and records that choice in the decision amendment (task 2).

## Decision 0013 amendment

| Need                                                  | Recommended                                        | Alternatives considered                                                                                                                  |
| ----------------------------------------------------- | -------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------- |
| Drag and drop with keyboard and screen-reader support | `dnd-kit` (core + sortable), pinned                | `react-dnd` (weaker keyboard story), native HTML drag (no keyboard, poor touch)                                                          |
| Graph rendering for the Stitch Map                    | `Cytoscape.js` with the `fcose` layout, pinned     | `d3-force` (more custom code for selection, zoom, and clustering); React Flow (oriented to editable flowcharts, not a read-only network) |
| Charts for Pulse                                      | Plain SVG components on the Lab tokens, no library | A charting library would add weight for six small charts                                                                                 |

## Board read model

### Lane rules (Go, `internal/board`)

```text
lane(intent, now, window):
  if intent.status == superseded                         → excluded
  if intent.status == released:
      return released_at >= now - window ? BURIED : excluded
  rev := intent.current_revision
  if any collision c on rev with c.state in {open, under_review} → STITCHING(c...)
  if rev.head_revision_ref empty or no observed analysis on rev  → ASSEMBLING(reason)
  keys := acceptance_criterion keys of rev.objective_revision
  if keys empty                                          → ALIVE(note: "no acceptance criteria")
  missing := [k in keys where no evidence e for intent with
              e.acceptance_key == k, e.state == passing, and e not stale
              against rev.head_revision_ref]
  if missing non-empty                                   → CHARGING(missing, passing=len(keys)-len(missing))
  return ALIVE
```

"Stale" uses the same definition the review packet uses (`internal/review`), called from there, so the board and the packet cannot disagree. The lane function is pure over a loaded snapshot and gets one parameterized table test.

### `board-view` contract (new, `contracts/v1/board-view.schema.json`)

```json
{
  "project_id": "…",
  "as_of_event": 1284,
  "window_days": 14,
  "lanes": [
    {
      "lane": "stitching",
      "count": 2,
      "cards": [
        {
          "intent_id": "…",
          "intent_revision": 3,
          "status": "active",
          "summary_line": "Retry failed payments",
          "owner_human_id": "…",
          "objective_id": "…",
          "objective_revision": 2,
          "repository_id": "…",
          "branch_ref": "feat/retries",
          "open_collision_ids": ["…"],
          "acceptance": { "required": 5, "passing": 2, "missing_keys": ["fr-003"] },
          "latest_evidence_at": "…",
          "evidence_stale": false,
          "reason": { "rule": "open_collision", "facts_markdown": "Collides with …" }
        }
      ]
    }
  ]
}
```

Names are resolved by the client from the name directory, which keeps the contract consistent with the other read models. Route: `GET /v1/projects/{project_id}/board?window_days=14`. It is read-only, served to any member, and under the same 2,000-claim bound as radar.

### Live behaviour

The event-to-query map from the foundation invalidates `board` on `intent.*`, `claims.observed`, `collision.*`, `resolution.decided`, `evidence.attached`, and `objective.revision_created`. After refetch the client diffs lanes by `intent_id`. A card that moved into `alive` within this page session, at a revision not yet celebrated, triggers the "It's alive!" effect. The set of celebrated `intent_id@revision` pairs is held in memory only.

## Collision decisions from the browser

### Precondition

`resolution-decision-input` gains two optional fields, `expected_left_intent_revision` and `expected_right_intent_revision`, plus `expected_state`. When present, the backend compares them inside the decision transaction and returns `409 conflict` with the current collision when any differs. The browser always sends all three, and sets `binding` exactly as `conn app` does for the same kind (the implementation reads that mapping from `internal/tui` rather than redefining it); existing CLI and TUI callers are unaffected. This is an additive, versioned schema change with accepted and rejected fixtures.

### Decision surface provenance

The decision record already captures the deciding Human and Participant. The backend adds the session kind (`cli` for CLI and `conn app` sessions, `browser`) as the decision surface in the event payload. This is audit data and grants no authority.

### Column → kind mapping in the dialog

| Drop target  | Offered kinds                                                     | Resulting state |
| ------------ | ----------------------------------------------------------------- | --------------- |
| Under review | `revise_objective`, `revise_blueprint`, `other`                   | `under_review`  |
| Resolved     | `dependency`, `sequencing`, `contract_agreement`, `accepted_risk` | `resolved`      |
| Dismissed    | `false_positive`                                                  | `dismissed`     |

The dialog never preselects a kind. When a proposal exists, the member may choose "Base on this proposal", which fills `selected_proposal_id` and shows the proposal text; the member still picks the kind and writes the rationale.

```text
┌ Decide this stitch ─────────────────────────────────────────────── ✕ ┐
│ contract_producer_consumer · severity high · confidence 0.92        │
│ ┌ @ana · Retry failed payments · rev 3 ┐ ┄┄┄ ┌ @luis · Refund flow · rev 5 ┐│
│ │ changes contract:PaymentGateway      │┄┄┄┄│ consumes contract:PaymentGat… ││
│ └──────────────────────────────────────┘     └──────────────────────────────┘│
│ Pair check: ✖ failing (2 new diagnostics) · 14 min ago                   │
│ Proposals: 1 · agent-authored via Kiro by @ana  [Base on this proposal]  │
│ Decision ○ dependency ○ sequencing ○ contract agreement ○ accepted risk  │
│ Consequence: the stitch closes as Resolved. A later revision that adds   │
│ a matched subject this decision does not cover reopens it.               │
│ Rationale (required) [_______________________________________________]  │
│                                       [Cancel]  [⚡ Record decision]     │
└──────────────────────────────────────────────────────────────────────────┘
```

Keyboard: on a focused collision card, `d` opens "Decide…", and the target column is chosen inside the dialog. Drag and drop is a shortcut, never the only way.

## Charter decisions from the browser

The dialog shows the immutable Charter snapshot (overview, responsibilities, exclusions, deliverables, evidence obligations, dependencies by Cell name), the Blueprint revision, the other Cells' acceptance status, and the consequence: "If you accept and every other Cell is accepted, this Blueprint revision becomes active and replaces revision N." It posts to the existing `…/cells/{cell_id}/accept` or `…/reject` endpoints with the exact revision in the path. Rejection requires a rationale. The backend's own-Charter rule is the enforcement; the client hides the actions only for clarity.

## Specimen page

```text
┌ ⚙ Retry failed payments  ◆ Stitching — needs a decision ────────────────────┐
│ @ana · Checkout retries rev 2 · acme/shop · feat/retries · base a1b2 → head c3d4 │
│ [Overview] [Claims] [Stitches 1] [Evidence 6] [Review packet] [Séance]   rev 3 ▾│
├──────────────────────────────────────────────────────────────────────────────┤
│ Acceptance criteria                                   Specimen jars            │
│ ✔ fr-001 Retries are capped at 3      observed · c3d4 ▣▣▣▣▢  4 of 5 passing    │
│ ✔ fr-002 Backoff is exponential       CI · c3d4                                │
│ ✖ fr-003 Idempotency key reused       failing · observed · c3d4                │
│ ○ fr-004 Metrics emitted              missing                                  │
└──────────────────────────────────────────────────────────────────────────────┘
```

Data: `GET intents/{id}` (with revision), `…/evidence`, the review packet generated by `internal/review` (route `GET /v1/projects/{project_id}/intents/{intent_id}/review-packet?format=markdown|json` if it is not yet exposed over HTTP; the CLI path already builds it from the same loader), and collisions filtered from radar. The "View on GitHub" link is `https://github.com/<owner>/<repo>/tree/<branch>` when the remote is a GitHub remote, and is omitted otherwise. Conn makes no GitHub API call in this milestone.

## Stitch Map

- Source: the radar view (active Intents, claims, open collisions) plus resolved and dismissed collisions from the same snapshot. No new endpoint unless radar lacks decided collisions; if it does, add `include_decided=true`.
- Nodes: Intents as rounded "body part" nodes in the owner's avatar ring, clustered by Objective (compound nodes); optional subject nodes in JetBrains Mono.
- Edges: open = dashed pumpkin stitch, under review = dotted bruise, resolved = solid stitch green, dismissed = 30% opacity. An edge with a failing pair check carries a small blood marker.
- Interaction: focus mode (one Intent plus its first-degree neighbours), zoom, fit, keyboard traversal over a sorted node list with arrow keys, `Enter` to open. A table tab lists the same edges.
- Bounds: radar's limits; when items are omitted, a banner says how many.

## Pulse

Six small SVG charts on the Lab tokens, one per insights measure: collisions opened per rule (bars), decisions per kind (bars), false-positive share (single gauge), median hours to first decision (single figure with window), acceptance criteria by provenance (stacked bar), combination checks by outcome (bars). An "insufficient" measure renders the empty state; nothing is extrapolated. The only filter is the window.

## Validation plan

- Go, parameterized: the lane function over a table covering each rule, precedence between rules, no-acceptance-criteria Objectives, stale evidence, the release window, and superseded exclusion.
- Go: decision with matching preconditions succeeds and records `browser` as the surface; any mismatched precondition returns `409` and writes nothing; a browser session cannot decide another Human's Charter.
- Contract fixtures for `board-view` and the widened `resolution-decision-input`.
- Vitest: an Intent card refuses to move on drop and shows the reason; the decision dialog offers only the kinds valid for the target and never preselects one; the "It's alive!" effect fires once per revision and never on first load.
- Manual: drag, keyboard, and screen-reader decision passes; a stale-decision race between two browsers; real trial below.
