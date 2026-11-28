# Architecture

Conn tells a coding agent, before it edits, when other in-progress work changes the same behavior, contract, or data, and gives the reviewer proof of what each change intended, touched, and verified ([decision 0005](decisions/0005-collision-warnings-and-review-proof-core.md)). It is a coordination protocol and evidence layer. The backend owns authorization and records; local adapters present workflows without becoming authorities.

## Runtime shape

```text
Kiro ── stdio ── conn mcp ── HTTPS ──┐
Kiro hooks ── conn hook ─── HTTPS ───┼── Go /v1 API ── application rules ── PostgreSQL
conn CLI / conn app ──────── HTTPS ──┤
GitHub Device Flow ──────────────────┤
Browser (Conn Lab) ─ cookie, same origin ─┘   (static Lab served by conn server; GitHub web flow)

conn mcp ── bounded JSON ── local Node.js TypeScript analyzer ── read-only git
conn mcp, conn hook ── focus and index files under <git dir>/conn
conn evidence run ── argument array, no shell, no CONN_ variables ── reviewed check from .conn/checks.json
git commit ── commit-msg ── conn trailer ── focus record (no network)
conn verify-pair ── temporary worktrees ── merge, diagnostics, default-branch pair checks ── combination check
GitHub Actions ── conn ci check ── CI credential (read allowlist + collision preview) ── HTTPS
event append ── NOTIFY conn_events ── LISTEN (one per conn server) ── Server-Sent Events ── Conn Lab tabs
```

Conn Lab ([decision 0013](decisions/0013-web-application.md)) is a React single-page application embedded in `conn server` and served on the API's origin. Browsers sign in with GitHub's web flow on the same OAuth client as `conn login` and hold a `browser` session only in an `HttpOnly` cookie; bearer and browser sessions are never interchangeable. In this version a browser session reads only, and sign-out requires a matching `Origin` and CSRF token. Committed events wake browser streams through PostgreSQL notifications; the stream rereads by cursor, so a missed notification delays and never loses an event. Local clients keep pull-by-cursor.

One Go executable provides `conn`, `conn app`, `conn mcp`, `conn hook`, and `conn server`. CLI, TUI, MCP, and hook adapters use the authenticated HTTP client and never access PostgreSQL directly. The analyzer and check commands receive no Conn token or database access.

Local development runs PostgreSQL through Docker Compose and runs `conn server` natively. Deployed clients require HTTPS connectivity to the authoritative backend.

## Domain and authority

A Project is the authorization and event-cursor boundary. MVP roles are `owner` and `member`.

- An Objective is an immutable Markdown-led revision with optional keyed outcomes, acceptance criteria, invariants, constraints, and questions. Every Intent references one exact Objective revision.
- Blueprints and Cell Charters are optional planning layers. A Blueprint revision is a complete immutable set of Cell Charter snapshots for one Objective revision; each assigned Human accepts or rejects their own Charter, and a revision becomes active only after every Charter is accepted. Warnings and evidence never require a Blueprint.
- Intents are participant-owned, revisioned descriptions of planned work. Agents may publish, revise, release, and analyze only their authenticated participant's Intents. Evidence may be attached by any Participant of the Human who owns the Intent ([decision 0007](decisions/0007-review-proof.md)).
- Semantic Claims are immutable records on one exact Intent revision. Pre-edit claims are declared claims. Deterministic rules produce advisory Collision Assessments; they do not block work or prove a defect.
- Resolution Proposals are non-binding. Human decisions set collision state.

Objectives, Blueprints, and Intents use integer expected revisions. A stale write returns a conflict instead of overwriting current state. Project changes append gap-free `bigint` event cursors.

## Pre-edit loop

1. **Shared vocabulary.** The concept catalog lists subjects derived from Objective invariants (`invariant:<objective-key>:<point-key>`) and acceptance criteria (`acceptance:<objective-key>:<point-key>`), plus the normalized subjects of active claims and the Intents that claim them. It is a read model, ranked by deterministic token similarity over subjects and display names. It uses no model, embedding, or source content.
2. **Claim suggestions.** `suggest_claims` runs the analyzer in planned mode on the paths a participant intends to change: `changes` for each export, `consumes` for each repository-internal import, and a new-path note for files that do not exist yet. It merges Objective-derived and catalog candidates and persists nothing.
3. **Publication.** The participant publishes the chosen candidates as declared claims. The MCP adapter fills `repository_id`, `branch_ref`, and `base_revision_ref` from the trusted worktree when they are omitted. A declared subject that is absent from the catalog but resembles a catalog subject of the same kind returns a near-match observation; observations change nothing.
4. **Assessment.** The assessor compares normalized subjects of active revisions. A `changes` or `removes` contract claim and another Intent's `consumes` claim open `contract_producer_consumer`, so the collision exists before either participant writes.
5. **Observed analysis.** `analyze_intent` runs the analyzer in observed mode against the Intent base: `changes` for edited exports, `removes` for deleted exports, `produces` for new exports, and `consumes` for imports in changed files. Unchanged exports produce nothing. Each Intent revision accepts one analysis.

## Collision lifecycle

| Decision kind                                                     | Collision state |
| ----------------------------------------------------------------- | --------------- |
| `false_positive`                                                  | `dismissed`     |
| `dependency`, `sequencing`, `contract_agreement`, `accepted_risk` | `resolved`      |
| `revise_objective`, `revise_blueprint`, `other`                   | `under_review`  |

A decision, its state change, and the `resolution.decided` and `collision.updated` events commit in one transaction. Owner rules for binding and high-risk kinds are unchanged, and a member who is not an owner may dismiss only a collision on their own Intent. Each assessment stores its matched subjects. When a later revision of either Intent produces an assessment for the same pair and rule, it inherits a dismissed or resolved state only if its matched subjects are a subset of the decided ones; otherwise it opens. Superseded open and under-review assessments become stale; decided ones stay as history. Radar lists collisions needing attention and the most recent decided collisions on active revisions.

## Review proof

[Decision 0007](decisions/0007-review-proof.md) adds what a reviewer needs after an agent finishes.

### Check configuration and observed runs

The committed `.conn/checks.json` (schema `checks-config`) defines at most 50 reviewed checks:

```json
{
  "version": 1,
  "checks": {
    "gateway-tests": {
      "command": ["pnpm", "--filter", "gateway", "test"],
      "working_directory": ".",
      "timeout_seconds": 600,
      "acceptance_keys": ["gateway-tests"],
      "env": { "CI": "1" }
    }
  }
}
```

`command` is an argument array of 1 to 64 items that no shell interprets. `working_directory` must resolve inside the repository, `timeout_seconds` is 1 to 3,600, `env` names match `^[A-Z_][A-Z0-9_]*$` and may not start with `CONN_`, and `acceptance_keys` has at most 20 keys.

`conn evidence run <check-key>` resolves the focused Intent, or `--intent`, and verifies before execution that the Intent belongs to the signed-in Human and is active, that the worktree's origin matches its repository, that the configuration is valid, and that every acceptance key exists on the Objective revision. It records the commit and whether the worktree is dirty, runs the command with the caller's environment minus every `CONN_` variable and with process-group termination at the timeout, streams combined output to the terminal and through SHA-256, and keeps the first 10 MiB in `<git dir>/conn/runs/<evidence-id>.log`. It attaches one `check_run` evidence reference per acceptance key with state `passing` (exit 0 in time) or `failing`, locator `conn-check:<key>@<commit-12>`, and an observation record, then exits with the check's exit code. A timed-out run records exit code `-1`. If the backend is unavailable after the run, it prints the result and log path, records nothing, and exits non-zero. MCP tools and hooks never run checks.

### Evidence provenance and staleness

Evidence with an observation is an observed run, labelled as observed on the Intent owner's machine and not independent CI proof; evidence without one is reported. The MCP `attach_evidence` schema cannot carry an observation. `GET /v1/projects/{project_id}/intents/{intent_id}/evidence` lists an Intent's 200 most recent references, newest first. Radar and work-context coverage report the latest evidence's provenance and commit and label it stale when the Intent records another `head_revision_ref`. The review packet computes staleness against the local `HEAD`.

### Commit trailers

Commits name their Intent with `Conn-Intent: <intent-id>@<revision>`. `conn git-hook preview|install|remove` manages a `commit-msg` hook in the effective hooks directory, including `core.hooksPath`; installation refuses to overwrite a different hook and prints the line to chain instead, and removal deletes only byte-identical Conn content. The hook runs `conn trailer` after the message is edited; it reads only the focus record, leaves a message that is empty apart from comments untouched so Git still aborts it, adds the trailer with `git interpret-trailers` unless one exists, and always exits 0. Installation and removal also delete an unchanged version 1 `prepare-commit-msg` hook, which added the trailer before editing and so turned abandoned commits into real ones. Work context returns `focus.commit_trailer` for `git commit --trailer` when the hook is absent. `internal/review` parses the latest trailer per Intent from a commit range for the later CI check.

### Scope drift and the review packet

Scope drift compares declared and observed `code_symbol` and `contract` subjects with `changes` or `removes` relations on the current Intent revision: matched, declared but not observed, and observed but not declared. It also lists changed files cited by no declared claim, marked when a Conn-connected agent session wrote them, and declared claims that analysis cannot observe, linked to acceptance evidence. Without an analysis of the current revision it reports `not_analyzed`, never the absence of drift. Lists are bounded to 100 items with omitted counts.

The review packet (schema `review-packet`, `packet_version: 1`) is a generated view that is never stored. `internal/review.Load` reads work context, Radar, the Intent's evidence, Project members, and the decision and proposal count of up to 10 collisions, and retries once when the responses reflect different event cursors. Local Git supplies the branch, base, head, dirty state, changed files, and trailers. The packet states the sanitized summary and assumptions, acceptance evidence with provenance, commit, and staleness, drift, collisions with decisions, integrity notes (a changed `.conn/checks.json`, runs on uncommitted changes, stale or reported evidence, incomplete analysis, commits without trailers), open questions, and known unknowns. Markdown rendering keeps sections in order, cuts the longest list until the output is under 60,000 characters, and states each cut.

Surfaces: `conn review [--intent] [--base] [--format markdown|json] [--output] [--analyze]`, the read-only MCP tool `read_review_packet`, and the `/conn-review` prompt. Handoffs use the same loader.

## Verification, CI, and adoption

[Decision 0008](decisions/0008-verification-ci-and-adoption.md) proves interactions between collided changes, brings Conn results to every pull request, and lowers adoption cost.

### Collision preview

`POST /v1/projects/{project_id}/collision-previews` (contracts `collision-preview-input` and `collision-preview`) runs the same pure assessor core as persistence, `assessCandidates`, on at most 500 candidate claims against the active claims of other Intents, excluding at most 20 named Intents. The candidates get an Intent identity that is never stored; with `as_intent_id` they stand for the next revision of that active Intent, so its current claims are excluded and a covering human decision is reported as inherited. The handler runs in a read-only transaction and appends no event. `suggest_claims` accepts `preview_collisions` and `intent_id`, and `conn start` and the CI check use the same route.

### Pair verification and combination checks

`conn verify-pair <collision-id>` requires a non-stale collision, a `branch_ref` on both Intents, and a worktree whose origin matches their repository. It resolves each branch from the local branch or `origin/<branch>`, fetching only with `--fetch`, reads `.conn/checks.json` from `origin/<default branch>` with `git show`, and shows both commits and every command before asking for confirmation or `--yes`. In two detached worktrees it runs the analyzer's `diagnostics` mode on each branch, merges the right commit into the left with `git merge --no-commit --no-ff` and a fixed identity, and records up to 50 conflicted paths on a conflict. Otherwise it counts diagnostics in the merged tree, reports each `(code, path)` whose merged count exceeds the larger branch count, and runs the configured setup and pair checks through the `review-proof` check runner. Worktrees are removed with `git worktree remove --force` after success, failure, and cancellation unless `--keep-worktree` is passed.

The combination check (contracts `combination-check-input` and `combination-check`) records both Intent revisions and commits, whether each commit is the Intent's recorded head, the merge result, the diagnostic delta, setup and check exit codes, durations, and output hashes, the outcome, the configuration source, `runner: "local"`, and the producing Participant. Any member's Participant may record one for a collision whose two Intents it names. It is append-only in `combination_checks`, appends `combination.checked` in the same transaction, never changes the collision state, and is validated against the facts it states. Conflict context lists the five most recent checks, and Radar, work context, and review-packet collisions carry `latest_combination_outcome`.

The analyzer's `diagnostics` mode takes no paths, symbols, or base, and returns at most 2,000 `(code, path, count)` entries aggregated over every discovered program, counting a diagnostic once across programs, with a truncation flag and no message text. Its reports are never attached to an Intent.

### CI credential

`ci_credentials` stores a credential's Project, RepositoryConnection, label, creator, creation and expiry times, throttled `last_used_at` (at most once per minute), revocation time, and the SHA-256 hash of its `conn_ci_` token. Only an owner creates one, through `conn ci credential create` with `--github-secret` or `--print`; the backend refuses requests that carry an MCP adapter header. Creation and revocation append `ci_credential.created` and `ci_credential.revoked` events without the token.

The authentication middleware recognizes the prefix and resolves a CI principal. A CI principal reaches only `GET /v1/ci/credential` and, for its own Project, Radar, Intents, Intent evidence, Objective revisions, conflict context, members, concepts, and the collision preview. Every other route answers 403 with the standard error body, and a route for another Project answers 403 whether or not that Project exists. These reads take an `application.Reader`, which is a member Human or a CI principal; neither can write.

### Pull-request check and CI add-on

`internal/ciaddon` renders the embedded workflow template (`template v1`, `@@NAME@@` placeholders), pinned to the running build's `vcs.revision` or an explicit `--conn-ref`, and previews, installs, and removes it like the Kiro bundle: preview writes nothing, installation refuses different content, and removal re-renders the installed options and deletes only byte-identical content. The workflow triggers on `pull_request`, grants `contents: read` plus `pull-requests: write` only with `--comment`, pins every action to a commit SHA, checks out with `persist-credentials: false`, builds Conn from source in one replaceable step, and exposes `CONN_CI_TOKEN` only to the check step.

`conn ci check` skips with a note when `CONN_CI_TOKEN` is absent. Otherwise it confirms the checked-out remote equals the credential's repository, reads the pull-request number and SHAs from the event file, computes the merge base, parses `Conn-Intent` trailers in `base..head` with the shared parser, runs observed analysis of `base..head`, and previews collisions for the observed claims excluding the named Intents. For each named Intent it loads the packet through `review.LoadIndependent`, which needs no Human reader, and computes drift against the CI-observed claims. The report has a count header, notes on unregistered work, released Intents, older revisions, and unreadable Intents, the previewed collisions, and one collapsed packet per Intent. It is appended to the job summary below 1 MiB and, with `--comment`, written to the one comment that contains `<!-- conn-check v1 -->`, below 65,536 characters, by omitting whole packets. The command always exits 0, makes no Conn write, and runs no project code.

### Other coding agents, start, issue import, and insights

`internal/agentguide` is the single source for the Kiro steering file and the `AGENTS.md` section between `<!-- conn:begin v1 -->` and `<!-- conn:end -->`, which `conn agents` creates, appends, replaces only when byte-identical to a known version, and removes without touching other content. The Kiro installer replaces an unchanged earlier steering file. `conn mcp config` prints a generic `mcpServers` snippet or the `kiro-cli mcp add` command. The MCP adapter reads `clientInfo.name`; a client other than Kiro sends `X-Conn-Participant-Adapter: mcp`, and the focus record keeps the adapter so Kiro hooks read as the same Participant.

The Conn Kiro power at `powers/conn/` is rendered by `internal/kiropower` in the Agent Plugins format and committed with a `.digest` that ties its content to `kiropower.Version`. Its `mcp.json` runs `conn mcp` from `PATH` with only the `${CONN_SERVER_URL}` placeholder, and Kiro manages and namespaces that server. Its skills render from `internal/agentguide`: `conn-setup` (onboarding through preview-then-confirm installers), `conn-work-loop` (the steering text), and `conn-review-proof`. The power loads on demand, so the always-on steering and command hooks remain workspace files from `internal/kirointegration`. `conn doctor` reports the power's minimum `conn` version, manual Kiro MCP registrations of `conn mcp` (read-only), the bundle, and the Git hook.

`conn objective import-github-issue` and `conn start` fetch issues with `internal/githubissue`, suggest up to 20 acceptance criteria from the first "acceptance criteria" heading, and import through `POST /v1/projects/{project_id}/objective-imports/github-issue` with source kind `github_issue`, the issue update time, and `sha256(title + "\n\n" + body)`. `conn start` reuses an import with the same hash, offers a new revision for a changed source, previews collisions for planned paths, and publishes a CLI Intent with the local focus.

`conn start <feature-dir>` also imports a GitHub Spec Kit feature with source kind `speckit_feature` through the local-spec import route. `internal/speckit` reads `spec.md`, `tasks.md`, `plan.md`, and the constitution locally, bounded to 1 MiB each and refusing symbolic links and paths outside the repository, and extracts `fr-`, `sc-`, `question-`, and `constitution-` key points and planned paths by fixed rules. `conn speckit check` turns those paths into claims and previews their collisions at plan time, publishing nothing and always exiting 0. The Conn Spec Kit extension (`internal/speckitext`, exported to a folder the human names and installed with `specify extension add --dev`) calls these steps from the `after_tasks`, `before_implement`, and `after_implement` hooks. Conn never runs Spec Kit or writes `.specify/`.

`GET /v1/projects/{project_id}/insights?since_days=30` (contract `project-insights`) counts `collision.opened` events and decisions per rule, the false-positive share, the median hours to a first decision, decisions before either Intent's `intent.released` event, analyzed revisions with an undeclared changed or removed code-symbol or contract subject, acceptance criteria of current Objectives by latest evidence provenance, and combination checks by outcome. Every ratio states its numerator and denominator and reports `insufficient_data` below five. The contract has no property for a person and the route accepts only `since_days`.

## Identity and sessions

`conn login` runs GitHub Device Flow locally and exchanges the temporary GitHub token once. Conn stores only a 256-bit opaque session token in the operating-system credential store; the backend stores its SHA-256 hash. Sessions have a 30-day absolute expiry and immediate revocation. `CONN_TOKEN` is an explicit non-persistent override. There is no plaintext fallback.

Every backend operation derives the Human from the session. MCP and hook inputs never select a Human or Participant as authority.

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

Read-only tools cover session status, Projects, Objectives, Blueprints, own Charters, updates, focused work context, Radar, the concept catalog, claim suggestions with an optional collision preview, collisions, conflict context, the review packet, and the CI workflow preview. Mutating tools cover interactive account operations, process-local Project selection, participant-owned Intents, local analysis, reported reference-first evidence, non-binding proposals, and installing or removing the CI workflow file. No tool creates credentials, runs checks, or runs pair verification.

Prompts provide `/conn-start`, `/conn-sync`, `/conn-resolve`, `/conn-finish`, `/conn-review`, and `/conn-handoff`. `/conn-start` lists concepts, suggests claims for agreed paths, publishes, and checks collisions before any write. `/conn-finish` records the head revision, analyzes that revision against the Intent base, checks collisions, offers human-approved `conn evidence run` calls for configured checks, and reads the review packet. `/conn-review` produces the packet for `gh pr create --body-file`. Kiro CLI workflows use focused tools and do not depend on resource discovery. Optional steering and hooks are installed only by `conn kiro integration install`.

Synchronization is pull-by-cursor during explicit turns and hook events. Conn has no MCP idle push, polling timer, sidecar, SSE, or WebSocket in the MVP.

### Local analyzer resolution

`conn mcp` and the CLI resolve the analyzer from `CONN_ANALYZER_PATH`, then `packages/typescript-analyzer/dist/cli.js` beside the `conn` executable, then the same path under the current directory. Node.js resolves from `CONN_NODE_PATH`, then `node` on `PATH`. `session_status` and `conn doctor` report availability, the chosen source, and the Node.js version. When analysis is unavailable, catalog suggestions, declared claims, and warnings keep working.

### Warn-only hooks

The optional bundle installs command hooks that run `conn hook <name>` with the event JSON on standard input:

- `session-start` summarizes the focused Intent or recommends `/conn-start` in a connected repository;
- `prompt-submit` reports new collisions, state changes, and counterpart revisions since the stored cursor, and delivers queued write warnings;
- `pre-write` warns when the target path is cited by another participant's `changes`, `removes`, or `consumes` claim, by an open collision on the focused Intent, or when no Intent is published;
- `post-write` records written repository-relative paths, never content.

Hooks read from the backend and never write to it. Each run has a three-second budget. On any failure the hook allows the event silently and records a diagnostic that `conn doctor` reports. Output is labelled as Project data, contains only identifier prefixes, revisions, rule IDs, normalized subjects, states, counts, and Conn commands, and stays under 1,500 characters. A pre-write warning exits with code 1, which Kiro documents as showing standard error without blocking the tool; no hook exits 2 or prints a permission decision.

Hooks and the MCP adapter share a non-authoritative state directory at `<git rev-parse --absolute-git-dir>/conn/`: the focus record, a path index refreshed from Radar, pending warnings, written paths, and diagnostics. Git never tracks it, each linked worktree has its own, writes are atomic, and a missing or corrupt file is treated as empty. It stores no credential, prompt, source, or Project prose.

### Capability proof

The pinned official MCP Go SDK supports prompts, resource templates, structured results with text fallback, cancellation, and form/URL elicitation over the existing transport. Protocol `2026-07-28` uses multi-round-trip `InputRequests`, echoed `InputResponses`, and opaque request state; handlers do not issue a nested elicitation request.

Interactive Kiro validation confirmed prompt discovery, structured fallback, form accept/decline/dismiss, URL opening, and cancellation. Kiro CLI 3.6.1 exposed no documented resource-template selector, so focused tools remain required. Headless operation has no mid-session input and must fail closed for elicitation. Command-hook exit-code behavior is documented in the `pre-edit-collision-warnings` design and awaits the two-session trial.

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

The TypeScript analyzer uses its pinned TypeScript 5.7.3 compiler API dependency. It receives an explicit worktree, normalized remote, mode, bounded relative paths, optional export names, and, in observed mode, a validated base revision. Observed mode runs only fixed read-only `git` commands without a shell or credentials, and keeps base file content in memory. Output contains normalized subjects, bounded paths/hashes, provenance, and code-only diagnostics. Source, diffs, prompts, transcripts, raw logs, and credentials are rejected or omitted by default. Check-run output stays in the local run log; only its hash reaches Conn.

## Deferred

- production web application and authenticated browser binding review;
- blocking or permission-changing hooks;
- hooks for clients other than Kiro, and adapters beyond MCP;
- offline outbox, persistent sidecar, background synchronization, or idle push;
- hosted source analysis, source upload, embeddings, or model-inferred claims;
- OpenAPI, GraphQL, SQL, and infrastructure extractors;
- failing pull requests on findings and merge gating, CI writes to Conn (including `runner: "ci"` evidence and CI-run pair verification), and a GitHub App;
- release artifacts or a container image for CI distribution;
- signed attestations for local runs and automatic mapping of test cases to acceptance keys;
- automatic assignment, execution supervision, and productivity scoring.

See [code structure](code-structure.md), [Kiro trials](real-trial.md), and [accepted decisions](decisions/README.md).
