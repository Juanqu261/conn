# Design

## Purpose and scope

This specification implements milestone 1 of decision 0005: Conn warns a coding agent before it edits when another in-progress Intent changes the same behavior, contract, or data.

The milestone fixes the reasons the current loop rarely warns in real use and brings Conn into the agent loop without a sidecar:

- a shared concept catalog and near-match advice so participants reuse subjects;
- claim suggestions from planned paths, so precise claims exist before any write;
- Git context filled on Intent publication;
- diff-based observed analysis that emits `changes`, `removes`, and `consumes`;
- analyzer resolution that works in any connected repository;
- a collision lifecycle that follows human decisions;
- warn-only command hooks with small local state;
- updated `/conn-start`, `/conn-finish`, and steering.

Out of scope: blocking hooks, review packets, check runs, commit trailers, CI, pair verification, other agents, and insights. Those belong to `review-proof` and `verification-ci-and-adoption`.

## Problems addressed

| Problem in the current implementation                                                                            | Location                                                                                | Change                                                              |
| ---------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------- | ------------------------------------------------------------------- |
| Collisions require identical `normalized_subject` strings and agents get no shared vocabulary                    | `internal/application/collisions.go` groups by subject; `WorkContext` lists no subjects | Concept catalog, Objective-derived subjects, near-match advice      |
| Static analysis never opens `contract_producer_consumer`                                                         | Analyzer emits `produces`/`consumes`/`affects`; rule needs `changes`/`removes`          | Planned suggestions and observed analysis emit `changes`/`removes`  |
| Every export of an analyzed file is `affects`; the base revision is ignored; no paths means the whole repository | `packages/typescript-analyzer/src/index.ts`                                             | Planned and observed modes with base comparison                     |
| Analysis runs only in `/conn-finish`                                                                             | `internal/mcp/workflows.go`                                                             | `suggest_claims` before editing                                     |
| Decisions never change collision state                                                                           | Only `stale` is set in `internal/postgres/collisions.go`                                | Decision-driven states and carry-forward                            |
| Hooks are credit-consuming agent prompts and nothing runs before a write                                         | `internal/kirointegration/templates/hooks/conn-lifecycle.json`                          | Command hooks on four triggers                                      |
| Analyzer path resolves from the current directory                                                                | `localAnalyzerConfig` in `cmd/conn/main.go`                                             | Environment, executable-relative, then current-directory resolution |
| Intents rarely carry branch and base references                                                                  | `publish_intent` sends only caller input                                                | Git context filled by the MCP adapter                               |

## Architecture decision gate

Before implementation, record decision 0006 under `docs/decisions/`. It amends 0002 and 0003:

1. Pre-edit claims are declared claims. `suggest_claims` returns candidates and the participant declares the chosen ones. No new provenance kind is added.
2. The analyzer gains `planned` and `observed` modes. In observed mode it may run local, read-only `git` commands with fixed arguments and no shell to read base versions of changed files. Output remains source-free.
3. The analyzer resolves from `CONN_ANALYZER_PATH`, then the executable directory, then the current directory. Node.js resolves from `CONN_NODE_PATH`, then `PATH`.
4. Human decisions set collision states. Assessments for later Intent revisions inherit a decision only when their matched subjects are a subset of the decided subjects.
5. The optional Kiro bundle contains command-action hooks for SessionStart, UserPromptSubmit, PreToolUse on writes, and PostToolUse on writes. It no longer contains agent-prompt hooks. Hooks read from the backend and never write to it. Hooks only warn.
6. Hooks and the MCP adapter share a small non-authoritative local state under the worktree's Git directory. Process-local Project selection keeps its current precedence. The focus record never grants access.

## System shape

```mermaid
flowchart LR
    K[Kiro agent] -->|tools and prompts| MCP[conn mcp]
    K -->|hook events on stdin| HOOK[conn hook ...]
    MCP --> AN[TypeScript analyzer: planned / observed]
    AN -->|read-only git| WT[(Worktree)]
    MCP -->|focus record| STATE[(git-dir/conn)]
    HOOK --> STATE
    MCP -->|HTTPS| API[/v1 API]
    HOOK -->|HTTPS reads| API
    API --> APP[application: catalog, lifecycle, assessor]
    APP --> PG[(PostgreSQL)]
```

## Concept catalog

### Objective-derived subjects

For each current Objective revision, each keyed invariant and keyed acceptance criterion produces one subject:

| Key point kind         | Subject kind           | Normalized subject                       |
| ---------------------- | ---------------------- | ---------------------------------------- |
| `invariant`            | `invariant`            | `invariant:<objective-ref>:<point-key>`  |
| `acceptance_criterion` | `acceptance_criterion` | `acceptance:<objective-ref>:<point-key>` |

`<objective-ref>` is the Objective `key` when present, otherwise `objective-<objective_id>`. Both forms match the existing `semanticSubject` pattern. A later revision that keeps both keys derives the same subject, so claims stay comparable across Objective revisions. The display name is the key-point statement, truncated to the `title` bound.

### Catalog entries

The catalog is a read model computed from current Objective revisions and active claims. It is not stored. Each entry has:

- `normalized_subject`, `subject_kind`, and `display_name`;
- `sources`: Objective key points and/or active claims;
- `claims`: up to 20 active claim references with Intent ID, revision, relation, and provenance;
- `score` and `reason` when a query was supplied.

New contract: `contracts/v1/concept-list.schema.json` with matching Go types in `internal/protocol`.

Route: `GET /v1/projects/{project_id}/concepts?kind=&q=&limit=&after=`. The default limit is 50 and the maximum is 200. The response uses the standard `{ "data": ..., "as_of_event": n }` envelope and an opaque `after` continuation.

### Deterministic similarity

Tokens come from the subject after its namespace and from the display name. Text is split on `: / . _ - ~`, spaces, and camelCase boundaries, then lowercased. Tokens shorter than three characters and a fixed stop list (`typescript`, `src`, `index`, `lib`, `the`, `and`, `for`) are dropped.

- Query ranking: Jaccard similarity between query tokens and entry tokens. An exact subject match scores 1.0. The `reason` lists shared tokens.
- Near-match advice: same `subject_kind`, a declared subject absent from the catalog, and similarity of at least 0.5.

The algorithm is a pure function in `internal/application` with a parameterized test. It uses no model, embedding, or source content.

### Near-match observations

`PublishIntent` and `ReviseIntent` compute near-match observations inside the write transaction, after claims are validated and before the response is built. They are appended to the existing `IntentView.advisory_observations` list. At most three similar subjects per declared subject and 20 observations per revision are returned. Observations are recomputed when the Intent is read and never change stored records.

## Claim suggestions

`suggest_claims` is a read-only MCP tool. `conn intent suggest-claims` is the matching CLI command. Both live in the local adapter because they need the worktree.

Input:

```json
{
  "project_id": "optional",
  "objective_id": "optional",
  "paths": ["src/orders/checkout.ts"],
  "symbols": { "src/payments/retry.ts": ["shouldRetryCharge"] },
  "focus": "optional free text for catalog matching",
  "worktree": "optional"
}
```

Sequence:

1. Resolve the Project and trusted worktree with the existing rules, and confirm that the local remote matches a RepositoryConnection with analysis enabled.
2. Run the analyzer in `planned` mode with the paths and symbol filter.
3. Query the catalog with the `focus` text and the Objective-derived subjects of `objective_id`.
4. Merge and deduplicate candidates by subject and relation. Analysis candidates come first, then Objective subjects, then catalog matches.
5. Return candidates with `provenance_kind: "declared"`, the analyzer's `source_location` evidence references, a `source` label (`analysis`, `objective`, or `catalog`), and a short reason.

The output also lists `new_paths` for planned files that do not exist, an `omitted_count`, analyzer diagnostics, and `as_of_event`. Nothing is persisted. When the participant includes candidates in `publish_intent` or `revise_intent`, they become ordinary declared claims, and the existing 100-claim input bound applies.

## Git context on publication

The MCP adapter fills missing Intent fields from the trusted worktree before calling the backend:

- `repository_id`: the single RepositoryConnection whose canonical remote equals the normalized local remote;
- `branch_ref`: `git symbolic-ref --short HEAD`, omitted on a detached HEAD;
- `base_revision_ref`: `git merge-base HEAD origin/<default_branch>`, or `HEAD` when the current branch is the default branch.

Explicit input always wins. The tool result lists `filled_fields` with each value. When Git is unavailable or the remote matches no connection, or more than one, the fields stay empty and the result explains why.

## Analyzer version 2

### Request

```json
{
  "request_version": 2,
  "mode": "planned",
  "worktree": "/abs/path",
  "canonical_remote": "github.com/acme/shop",
  "tsconfig_path": "optional",
  "paths": ["src/payments/retry.ts"],
  "symbols": { "src/payments/retry.ts": ["shouldRetryCharge"] },
  "base_revision_ref": "required in observed mode when known"
}
```

`base_revision_ref` must match `^[0-9a-f]{7,64}$` or a ref name containing only `[A-Za-z0-9._/-]` and not starting with `-`. The analyzer confirms it with `git rev-parse --verify --quiet <ref>^{commit}`.

### Planned mode

- `paths` is required and bounded to 50 entries.
- For each existing path that a discovered TypeScript program includes, every export, or the named subset, produces a `code_symbol` claim with `changes` and a `contract` claim with `changes`.
- Each repository-internal import in a planned file produces a `contract` claim with `consumes`, using the existing subject form `contract:typescript:<target-path>/<export>`.
- A missing path produces the diagnostic `conn-planned-path-new` with the path and no claims.

### Observed mode

1. Changed set: `git diff --name-only <base>` (which compares the base with the working tree, covering staged and unstaged changes) plus `git ls-files --others --exclude-standard`, filtered to `.ts`, `.tsx`, `.mts`, and `.cts` files in a discovered program. Explicit `paths` replace the changed set.
2. For each changed file, read the base version with `git show <base>:<path>` and parse it with `ts.createSourceFile`. No base type check is needed.
3. Compare exports by name. The declaration fingerprint is the SHA-256 of the declaration text with whitespace runs collapsed. The fingerprint covers the body as well as the signature, because behavior can change without a signature change.
4. Emit `changes` for a different fingerprint, `removes` for an export missing in the working tree, and `produces` (contract only) for a new export. Unchanged exports emit nothing.
5. Each repository-internal import in a changed file emits `consumes`.
6. When the base cannot be resolved, emit `conn-base-unavailable` and no claims.

Git runs through `execFile` with a fixed argument list, a 10-second timeout, `GIT_TERMINAL_PROMPT=0`, no inherited credential variables, and the worktree as `cwd`. Base file content stays in memory and never enters the report.

### Report

`local-analyzer-report` becomes `report_version: 2` with `mode`, `repository.base_revision_ref`, and `repository.head_revision_ref`. Claim limits stay at 500. When the limit is exceeded, claims are kept in the order `removes`, `changes`, `consumes`, `produces`, then by subject, and the diagnostic `conn-claims-truncated` reports kept and omitted counts. Version 1 is removed because the analyzer and Go adapter ship together.

### Go adapter and `analyze_intent`

`analyze_intent` sends `mode: "observed"` and the Intent's `base_revision_ref`. When the Intent has none, it sends the merge base computed as in the Git context section. Explicit `paths` are still accepted and confined to the worktree. The backend attachment flow and exact-revision checks stay unchanged. `intent_analysis_reports` still allows one analysis per Intent revision, so analyzing again after further edits requires a new Intent revision. Planned suggestions are never attached, so they do not use up that analysis.

### Analyzer resolution

`localAnalyzerConfig` resolves, in order:

1. `CONN_ANALYZER_PATH`;
2. `<dir of os.Executable()>/packages/typescript-analyzer/dist/cli.js`;
3. `<cwd>/packages/typescript-analyzer/dist/cli.js`.

Node.js resolves from `CONN_NODE_PATH`, then `node` on `PATH`. `session_status` and `conn doctor` report availability, the chosen source, and the Node.js version.

## Collision lifecycle

### Decision to state

| Decision kind                                                     | Collision state |
| ----------------------------------------------------------------- | --------------- |
| `false_positive`                                                  | `dismissed`     |
| `dependency`, `sequencing`, `contract_agreement`, `accepted_risk` | `resolved`      |
| `revise_objective`, `revise_blueprint`, `other`                   | `under_review`  |

`DecideResolution` updates the assessment state and `decision_id`, inserts the decision, and appends `resolution.decided` and `collision.updated` in one transaction. Owner requirements for binding and high-risk kinds are unchanged. A non-owner member may record `false_positive` only when they are the Human behind one of the two Intents' Participants. Proposals never change state.

### Matched subjects and carry-forward

Each assessment stores `matched_subjects`, the sorted normalized subjects that matched, bounded to 50. The schema gains `matched_subjects` and an optional `decision_ref`.

When the assessor builds a candidate for an Intent pair and rule, it looks up the most recent assessment for the same two Intent IDs and rule with state `dismissed` or `resolved`:

- When the candidate's subjects are a subset of that assessment's `matched_subjects`, and the earlier list was not truncated, the new assessment is inserted with the inherited state and `decision_ref`. Conn appends `collision.updated` instead of `collision.opened`.
- Otherwise the new assessment is `open` and Conn appends `collision.opened`.

Superseding older revisions still marks only `open` and `under_review` assessments as `stale`. Decided assessments stay as history.

### Read models

- Radar keeps `open_collisions` as `open` plus `under_review` and adds `decided_collisions`: the 50 most recent `resolved` and `dismissed` assessments on active revisions, each with its `decision_ref`.
- Work context `related_collisions` includes every state for the focused Intent's current revision.
- `check_collisions` groups results into `needs_attention` and `decided`.
- `resolutionDependencies` in `radar.go` includes `resolved` collisions with a `dependency` decision while both Intents are active.

### Persistence

Migration `00008_collision_lifecycle.sql`:

- adds `matched_subjects text[] NOT NULL DEFAULT '{}'` and `decision_id uuid NULL REFERENCES resolution_decisions`;
- backfills `matched_subjects` from the matching claims of existing rows;
- adds an index on `(project_id, left_intent_id, right_intent_id, rule_id, created_at DESC)` for carry-forward lookup.

## Local hook state

Location: `<git rev-parse --absolute-git-dir>/conn/`. Git never tracks it, and each linked worktree has its own directory.

| File                    | Writer                       | Content                                                                                                                                                                | Bound          |
| ----------------------- | ---------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------- |
| `focus.json`            | MCP adapter, `prompt-submit` | `version`, `server_url`, `project_id`, `repository_id`, `intent_id`, `intent_revision`, `participant_id`, `last_event`, `known_collisions` (ID to state), `updated_at` | 100 collisions |
| `index.json`            | hooks                        | `as_of_event`, `fetched_at`, map from repository-relative path to claims and collision IDs                                                                             | 10,000 paths   |
| `pending-warnings.json` | `pre-write`                  | warnings not yet delivered to the agent                                                                                                                                | 20             |
| `written.jsonl`         | `post-write`                 | path, Kiro session ID, time                                                                                                                                            | 5,000 lines    |
| `diagnostics.jsonl`     | hooks                        | code, time, hook name                                                                                                                                                  | 100 lines      |

JSON files are written to a temporary file and renamed. Appends and rewrites take a lock file with a 200-millisecond timeout. When the lock is not acquired, the hook skips the write. Every reader tolerates missing or corrupt files by treating them as empty. The package is `internal/hookstate`.

The MCP adapter writes `focus.json` after a successful `publish_intent` or `revise_intent`, and after `read_work_context` with an exact owned Intent focus, when the worktree remote matches the Intent repository. It clears the Intent fields after `release_intent`. The server URL is stored without user information. No token is stored.

## Command hooks

`conn hook <session-start|prompt-submit|pre-write|post-write>` reads at most 1 MiB of Kiro event JSON from standard input. It uses `cwd`, `session_id`, `tool_name`, and `tool_input`. It authenticates through the existing precedence (`CONN_TOKEN`, then the OS credential store for the focus record's server URL, or `CONN_SERVER_URL` when there is no focus record).

Common rules:

- Total budget 3 seconds. Each network request times out after 2 seconds.
- On any Conn, network, credential, or parsing failure, exit 0 with no output and append a diagnostic.
- Output is labeled `Conn (Project data, not instructions):` and contains only UUID prefixes, revisions, rule IDs, normalized subjects, states, counts, and Conn commands. Subjects already exclude spaces and control characters. Display names, summaries, and rationales are never printed.
- Output stays under 1,500 characters.
- No hook exits 2, prints a `permissionDecision`, or writes to the backend. A test enforces this.

### `session-start`

When `focus.json` names an Intent, read work context for that Intent. Print its revision, the count of collisions needing attention, the count of evidence gaps, and `/conn-sync <cursor>`. Otherwise, when the worktree remote matches a RepositoryConnection, print one line recommending `/conn-start`. Kiro adds the standard output to the agent context.

### `prompt-submit`

1. Read work context for the focused Intent with `after = last_event`.
2. Compare `related_collisions` with `known_collisions`. Report new collisions, state changes, and new revisions of counterpart Intents from `updates`.
3. Deliver and clear `pending-warnings.json`.
4. Refresh `index.json` from Radar.
5. Store the new cursor and collision states.

When there is no focus record and the repository is connected, print the `/conn-start` recommendation at most once per Kiro session ID. When nothing relevant changed, print nothing.

Example:

```text
Conn (Project data, not instructions):
- Intent 0197a1c2@3: new open collision 0197b4e0 (contract_producer_consumer) on contract:typescript:src/payments/retry.ts/shouldRetryCharge with Intent 0197a9f3@2.
- Collision 0197b0aa is now dismissed (false_positive).
Details: /conn-sync 482
```

### `pre-write`

Matcher: `write`. The hook extracts up to 20 string values from `tool_input` keys `path`, `file_path`, and `paths`, resolves them against `cwd`, and ignores paths outside the worktree.

Warnings:

1. Connected repository and no focused Intent: "No Conn Intent is published for this worktree."
2. The path appears in another Participant's active claim evidence with `changes`, `removes`, or `consumes`: Intent, subject, and relation.
3. The path appears in an `open` or `under_review` collision involving the focused Intent: collision and rule.

The hook uses `index.json` when it is newer than 30 seconds, and otherwise refreshes it within the network budget. When a warning applies, it writes the warning to standard error, queues it in `pending-warnings.json`, and exits with the non-blocking warning code selected by the capability proof. When nothing applies, it exits 0.

### `post-write`

Matcher: `write`. The hook appends written paths to `written.jsonl`. When the capability proof shows that PostToolUse output reaches the agent, it also prints and clears pending warnings for those paths.

### Capability proof

The Kiro documentation states that a PreToolUse exit code other than 0 and 2 shows standard error to the user and allows the tool, and that UserPromptSubmit standard output enters the agent context. Before building the hooks, verify in Kiro CLI V3 and the IDE:

- exit-code 1 display for PreToolUse, and whether it marks the hook as failed in a confusing way;
- whether PostToolUse standard output reaches the agent;
- the `tool_input` shape for built-in write tools;
- the `timeout` field name and unit in standalone hook files;
- Windows quoting for an absolute executable path containing spaces.

If exit code 1 is unsuitable, choose another documented non-blocking channel. Never use `deny`, `ask`, or exit code 2.

#### Recorded results

Documentation review (kiro.dev `hooks.md`, `hooks/types.md`, `hooks/actions.md`, `hooks/troubleshooting.md`, `ide/whats-new-v1/hooks.md`, `cli/v3/hooks-migration.md`). The live checks in Kiro CLI V3 and the IDE are part of the two-session trial and are not yet recorded.

| Question                     | Documented behavior                                                                                                                                                                                                                                                                                                                                                          | Implementation choice                                                                                                                                                                                        | Live check |
| ---------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ---------- |
| PreToolUse exit code 1       | CLI: "Other exit codes: show STDERR warning to user, allow tool execution." Troubleshooting (IDE and CLI): "Non-zero exit codes (other than 2) show STDERR as a warning but don't block execution." IDE 1.0: "Any other non-zero exit code is treated as an error, not a block." The older IDE text in `hooks/actions.md` says a non-zero Pre Tool Use exit blocks the tool. | Exit code 1 is the warning channel. `conn hook pre-write --warn-exit-code 0` keeps warnings in the prompt-submit queue only, as a fallback if the IDE blocks. Exit code 2, `deny`, and `ask` are never used. | Pending    |
| PostToolUse standard output  | Exit 0 output is added to context only for SessionStart and UserPromptSubmit and is ignored for other triggers.                                                                                                                                                                                                                                                              | `post-write` prints nothing; queued warnings are delivered by `prompt-submit`.                                                                                                                               | Pending    |
| `tool_input` for write tools | Not specified for built-in write tools. MCP and permission examples use `path`.                                                                                                                                                                                                                                                                                              | `pre-write` and `post-write` read `path`, `file_path`, and `paths` (string or string array), up to 20 values.                                                                                                | Pending    |
| `timeout` field              | Standalone files: `timeout` in seconds, default 60. CLI profile docs also mention `timeout_ms`.                                                                                                                                                                                                                                                                              | Each hook sets `"timeout": 5`; the hook enforces its own three-second budget.                                                                                                                                | Pending    |
| Windows quoting              | Command actions run through a shell; the shell on Windows is not documented.                                                                                                                                                                                                                                                                                                 | The command is rendered unquoted when the path has no spaces or shell metacharacters and double-quoted otherwise. `--command-on-path` avoids quoting entirely.                                               | Pending    |
| Matcher                      | `write` is the built-in category for file write tools in CLI V3 and the IDE.                                                                                                                                                                                                                                                                                                 | PreToolUse and PostToolUse hooks use `"matcher": "write"`.                                                                                                                                                   | Pending    |

## Kiro bundle version 2

Templates:

- `templates/hooks/conn-hooks.json`: four command hooks (`conn-session-start`, `conn-prompt-submit`, `conn-pre-write`, `conn-post-write`) with `{{CONN_COMMAND}}` placeholders;
- `templates/steering/conn-work-loop.md`: the updated loop.

`conn kiro integration preview|install|remove` renders `{{CONN_COMMAND}}` as the quoted absolute path of `os.Executable()` by default, or `conn` with `--command-on-path`. Removal recognizes Conn content by re-rendering the template with the command value found in the installed file.

Migration: when `.kiro/hooks/conn-lifecycle.json` is byte-identical to the version 1 template, preview lists it as `remove` and install deletes it. A modified version 1 file is preserved and reported as a conflict for manual review. Other conflict rules stay unchanged.

## Workflow and steering changes

`/conn-start` becomes:

1. `session_status`, then Project resolution.
2. `read_work_context` with the supplied focus.
3. Agree planned paths with the human, then call `list_concepts` with the task focus and `suggest_claims` with the planned paths.
4. Publish or revise the Intent with the chosen candidates. The adapter fills the Git context.
5. `check_collisions`. Present collisions as advisory and point to `/conn-resolve`.
6. Summarize scope, claims, collisions, unknowns, and the cursor before any write.

`/conn-finish` calls `analyze_intent` without paths so observed mode uses the diff. `/conn-sync` is unchanged. Steering describes the pre-edit loop, warn-only hooks, and optional Blueprints.

## Contracts and persistence summary

| Artifact                                 | Change                                                                                                            |
| ---------------------------------------- | ----------------------------------------------------------------------------------------------------------------- |
| `concept-list.schema.json`               | New                                                                                                               |
| `local-analyzer-report.schema.json`      | Version 2 with `mode` and base/head references                                                                    |
| `collision-assessment.schema.json`       | Adds `matched_subjects` and `decision_ref`                                                                        |
| `radar-view.schema.json`                 | Adds `decided_collisions`                                                                                         |
| `intent-view.schema.json`                | `advisory_observations` bound raised from 10 to 21: one Charter observation plus up to 20 near-match observations |
| `GET /v1/projects/{project_id}/concepts` | Also accepts `objective_id`, so `suggest_claims` reads one Objective's derived subjects in one bounded request    |
| `00008_collision_lifecycle.sql`          | Matched subjects, decision reference, lookup index                                                                |
| `fixtures/cases.json`                    | Accepted and rejected cases for every changed schema                                                              |

## Security and privacy

- Catalog, suggestions, reports, hook output, and assessments contain identifiers, subjects, paths, line numbers, and hashes only.
- Hook output reaches the agent's context automatically. It therefore excludes free text written by other participants, which limits prompt injection through Project data.
- Analyzer and Git subprocesses receive a minimized environment without `CONN_TOKEN` or credential-store access.
- Revision references are validated before use as Git arguments.
- Planned and observed paths are confined to the trusted worktree with the existing symlink-resolving containment check.
- Hooks read only. All backend writes remain explicit MCP or CLI operations.

## Error behavior

| Condition                     | Behavior                                                                                                         |
| ----------------------------- | ---------------------------------------------------------------------------------------------------------------- |
| Analyzer or Node.js missing   | `suggest_claims` returns catalog candidates and reports analysis as unavailable. `analyze_intent` fails plainly. |
| Base revision unknown         | Observed report has `conn-base-unavailable` and no claims. The tool result marks analysis as incomplete.         |
| Catalog query exceeds bounds  | Bounded page plus continuation.                                                                                  |
| Hook backend timeout          | Silent allow and a diagnostic.                                                                                   |
| Corrupt hook state            | Treated as empty and rewritten.                                                                                  |
| Decision on a stale collision | Recorded, state set, no carry-forward source change.                                                             |

## Validation plan

Per decision 0004, add only these scenarios:

1. **Catalog and near-match (application, PostgreSQL):** Objective-derived subjects stay stable across revisions, active claim subjects are listed, ranking is deterministic, and near-match observations are bounded and change nothing.
2. **Analyzer modes (Vitest, temporary Git repository):** planned mode yields `changes` and `consumes`; observed mode yields `changes`, `removes`, `produces`, and `consumes` and nothing for unchanged exports; truncation order; base unavailable; an option-like ref is rejected; no source marker appears in output.
3. **Collision lifecycle (application, PostgreSQL, parameterized by decision kind):** state mapping, non-owner dismissal authority, carry-forward for a subset and reopening for a superset, events in the same transaction.
4. **Hooks (Go, fake backend):** focus write and clear, one-time prompt-submit report, the three pre-write warnings, fail-open on backend failure, never exit 2 or deny, output bound.
5. **Bundle (existing bundle test):** version 2 install, version 1 migration, modified version 1 preserved, removal.
6. **Two-participant MCP scenario:** two MCP participants on the new fixture `fixtures/pre-edit-two-sessions` call `suggest_claims` and `publish_intent` and receive `contract_producer_consumer` and `shared_declared_invariant` assessments before any file write.
7. **Manual:** two real Kiro sessions repeat scenario 6 with the hooks installed, and the results are recorded in `docs/real-trial.md`.

Run `pnpm check`, `pnpm test`, and `pnpm build`.

## Deferred

- Blocking or permission-changing hooks (Phase 4).
- OpenAPI, GraphQL, SQL, and infrastructure extractors.
- Model-inferred claims and embedding search.
- Hooks for clients other than Kiro.
- Review packets, check runs, trailers, CI, pair verification, and insights (later milestones).
