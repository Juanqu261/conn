# Architecture

Conn is a coordination protocol and evidence layer. The backend owns authorization and records; local adapters present workflows without becoming authorities.

## Runtime shape

```text
Kiro ── stdio ── conn mcp ── HTTPS ──┐
conn CLI / conn app ──────── HTTPS ──┼── Go /v1 API ── application rules ── PostgreSQL
GitHub Device Flow ──────────────────┘

conn mcp ── bounded JSON ── local Node.js TypeScript analyzer
```

One Go executable provides `conn`, `conn app`, `conn mcp`, and `conn server`. CLI, TUI, and MCP adapters use the authenticated HTTP client and never access PostgreSQL directly. The analyzer receives no Conn token or database access.

Local development runs PostgreSQL through Docker Compose and runs `conn server` natively. Deployed clients require HTTPS connectivity to the authoritative backend.

## Domain and authority

A Project is the authorization and event-cursor boundary. MVP roles are `owner` and `member`.

- An Objective is an immutable Markdown-led revision with optional keyed outcomes, acceptance criteria, invariants, constraints, and questions.
- A Blueprint revision is a complete immutable set of Cell Charter snapshots for one Objective revision.
- Each assigned Human accepts or rejects their own Charter. A Blueprint becomes active only after every Charter is accepted.
- A new proposal does not replace the active Blueprint until accepted. An Objective has at most one Blueprint identity; planning changes append revisions.
- Intents are participant-owned, revisioned descriptions of planned work. Agents may publish, revise, release, analyze, and attach permitted evidence only for their authenticated participant.
- Semantic Claims are immutable records on one exact Intent revision. Deterministic rules produce advisory Collision Assessments; they do not block work or prove a defect.
- Resolution Proposals are non-binding. Binding decisions remain human-authoritative.

Objectives, Blueprints, and Intents use integer expected revisions. A stale write returns a conflict instead of overwriting current state. Project changes append gap-free `bigint` event cursors.

## Identity and sessions

`conn login` runs GitHub Device Flow locally and exchanges the temporary GitHub token once. Conn stores only a 256-bit opaque session token in the operating-system credential store; the backend stores its SHA-256 hash. Sessions have a 30-day absolute expiry and immediate revocation. `CONN_TOKEN` is an explicit non-persistent override. There is no plaintext fallback.

Every backend operation derives the Human from the session. MCP inputs never select a Human or Participant as authority.

## Kiro adapter

Kiro owns the lifetime of `conn mcp` over standard input and output. Standard output is protocol-only; diagnostics use standard error.

The adapter keeps only concurrency-safe process-local Project selection. It resolves Projects in this order:

1. explicit `project_id`;
2. process-local `select_project` choice;
3. `CONN_PROJECT`;
4. sole visible Project;
5. exact normalized local Git remote match;
6. bounded ambiguity response.

Backend membership is checked on every operation. Selection never grants access.

### MCP surface

Read-only tools cover session status, Projects, Objectives, Blueprints, own Charters, updates, focused work context, Radar, collisions, and conflict context. Mutating tools cover interactive account operations, process-local Project selection, participant-owned Intents, local analysis, reference-first evidence, and non-binding proposals.

`analyze_intent` verifies the exact active Intent revision and analysis-enabled Repository Connection, confines paths to the trusted worktree, runs the existing Node.js subprocess without credentials, validates its bounded report, attaches observed claims, reassesses collisions, and returns the post-commit cursor.

Prompts provide `/conn-start`, `/conn-sync`, `/conn-resolve`, `/conn-finish`, and `/conn-handoff`. Optional resource templates expose focused context where the client supports them; Kiro CLI workflows use focused tools and do not depend on resource discovery. Optional steering and hooks are installed only by `conn kiro integration install`.

Synchronization is pull-by-cursor during explicit turns. Conn has no MCP idle push, polling timer, sidecar, SSE, or WebSocket in the MVP.

### Capability proof

The pinned official MCP Go SDK supports prompts, resource templates, structured results with text fallback, cancellation, and form/URL elicitation over the existing transport. Protocol `2026-07-28` uses multi-round-trip `InputRequests`, echoed `InputResponses`, and opaque request state; handlers do not issue a nested elicitation request.

Interactive Kiro validation confirmed prompt discovery, structured fallback, form accept/decline/dismiss, URL opening, and cancellation. Kiro CLI 3.6.1 exposed no documented resource-template selector, so focused tools remain required. Headless operation has no mid-session input and must fail closed for elicitation.

## Human review boundary

Kiro tool permission is a client safety mechanism, not a domain signature. Ordinary MCP tools cannot accept Charters, approve Objectives or Blueprints, mutate membership, accept risk, or record binding Resolution Decisions.

Decision 0001 selects an authenticated browser review page for future binding actions. URL elicitation may open that page, but the browser independently authenticates the Human and submits directly to the backend. `conn app` and direct CLI review remain fallbacks until the web feature exists.

## Contracts, API, and persistence

JSON Schema Draft 2020-12 files under `contracts/v1` are canonical. Go and Node validators run the same accepted and rejected fixtures. Go protocol types are handwritten and held to the schemas by conformance tests.

The backend uses Go `net/http`, pgx, handwritten parameterized SQL, and Goose migrations. Application transactions enforce membership, authority, lifecycle, revision, and event rules. Successful Project responses use:

```json
{ "data": {}, "as_of_event": 42 }
```

Conn-owned migrations run only in development and deployment automation, never in customer repositories or public CLI/MCP operations.

## Local analysis and data minimization

The TypeScript analyzer uses its pinned TypeScript 5.7.3 compiler API dependency. It receives an explicit worktree, normalized remote, and optional relative paths. Output contains normalized subjects, bounded paths/hashes, provenance, and code-only diagnostics. Source, diffs, prompts, transcripts, raw logs, and credentials are rejected or omitted by default.

## Deferred

- production web application and authenticated browser binding review;
- additional coding-agent adapters;
- offline outbox, persistent sidecar, background synchronization, or idle push;
- hosted source analysis, source upload, embeddings, or model-inferred claims;
- GitHub App checks, pull-request automation, CI publishers, and merge policy;
- automatic assignment, execution supervision, and productivity scoring.

See [code structure](code-structure.md), [three-session trial](real-trial.md), and [accepted decisions](decisions/README.md).
