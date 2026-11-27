# Design

## Purpose and scope

This specification implements milestone 3 of decision 0005. It proves interactions between collided changes, brings Conn results to every pull request through a CI add-on that Conn installs, and lowers adoption cost.

The milestone adds:

- `conn verify-pair` and immutable combination check records;
- a collision preview that publishes nothing;
- a narrow, owner-created CI credential;
- `conn ci check` and a GitHub Actions workflow that Conn previews and installs from the CLI or MCP;
- support for other MCP coding agents through an `AGENTS.md` section, a printed MCP configuration, and honest adapter attribution;
- `conn start` and GitHub issue import;
- Project-level insights.

Out of scope: failing pull requests on findings, CI writes to Conn, a GitHub App, a web application, and hooks for clients other than Kiro.

## Dependencies

| From                          | Used for                                                                                         |
| ----------------------------- | ------------------------------------------------------------------------------------------------ |
| `pre-edit-collision-warnings` | Observed analysis, assessor rules, collision states, `focus.json`, `suggest_claims`              |
| `review-proof`                | `.conn/checks.json`, the check runner, the trailer parser, the review packet loader and renderer |

## Architecture decision gate

Before implementation, record decision 0008 under `docs/decisions/`:

1. The CI add-on is a GitHub Actions workflow that Conn renders and installs into the user's repository from the CLI or MCP. It replaces the GitHub App sequence of `semantic-intent-radar` for this version.
2. The CI credential is created by a Project owner through a human-direct surface, bound to one Project and RepositoryConnection, read-only plus collision preview, expiring, revocable, hashed at rest, and prefixed `conn_ci_`.
3. CI reports findings and never fails a pull request for them. CI writes nothing to Conn in this version.
4. Distribution of `conn` in CI. Compare three options and stop for approval:
   - build from the pinned Conn source in the workflow, with no release infrastructure but one to two minutes of build time;
   - release artifacts (a GitHub Release binary and an npm analyzer package), which are fast but need a release pipeline and signing;
   - a container image, which is reproducible but needs a registry and image maintenance.

   This design assumes the first option and isolates it in one workflow step so that another method can replace it.
5. Pair verification runs code from both branches locally after confirmation, reads its commands from the default branch, and records an advisory combination check. The analyzer gains a `diagnostics` mode, which amends 0003 again.
6. GitHub issue import uses an optional, non-persisted `GH_TOKEN` or `GITHUB_TOKEN` from the environment and never sends it to Conn.
7. Insights are Project-level only, never include people, and suppress small denominators.
8. Non-Kiro MCP clients are attributed with adapter kind `mcp`. Conn still never writes MCP client configuration.

## Collision preview

The pure assessor core moves out of `assessActiveIntentCollisions` so that preview and persistence share it:

```go
func assessCandidates(changed []protocol.SemanticClaim, active []protocol.SemanticClaim, prior DecisionIndex) []collisionCandidate
```

Route: `POST /v1/projects/{project_id}/collision-previews`. Input contract `collision-preview-input`: `claims` (at most 500 `SemanticClaimCandidate` items) and `exclude_intent_ids` (at most 20). The handler gives the candidates a synthetic Intent identity that is never stored, runs `assessCandidates` against active claims outside the excluded Intents, and returns the `collision-preview` contract: rule, severity, matched subjects, counterpart Intent and revision, and any inherited state with its decision reference. It runs inside a read-only transaction and appends no event.

`suggest_claims` gains `preview_collisions: true`, which sends its candidates to this route.

## Pair verification

```text
conn verify-pair <collision-id> [--fetch] [--yes] [--keep-worktree] [--worktree <path>]
```

Sequence:

1. Read conflict context. Require a non-stale collision, `branch_ref` on both Intents, and a local remote that matches their repository.
2. Resolve each branch to a commit from the local branch or `refs/remotes/origin/<branch>`. With `--fetch`, first run `git fetch origin <left> <right>`.
3. Read `.conn/checks.json` from `origin/<default_branch>` with `git show`. Use its `pair_verification` section. When the section or file is absent, only the diagnostic comparison runs.
4. Show the two commits, the commands, and the statement "This runs code from both branches on this machine." Require confirmation or `--yes`.
5. Create two detached worktrees in a temporary directory: `left` at the left commit and `right` at the right commit.
6. Run the analyzer in `diagnostics` mode on `left` and `right`.
7. In `left`, run `git merge --no-commit --no-ff <right-commit>` with a fixed identity. When the merge has conflicts, record `merge_conflict` with up to 50 paths from `git diff --name-only --diff-filter=U`, then skip to step 10.
8. Run `diagnostics` mode on the merged tree. A new diagnostic is a `(code, path)` pair whose merged count exceeds the larger of its left and right counts. Report up to 50 pairs. All three runs happen before dependency setup, so they compare the same conditions.
9. Run the configured setup command, then each pair check, in the merged tree, using the `review-proof` check runner (argument arrays, no shell, `CONN_` variables removed, timeouts, hashed output, local logs).
10. Determine the outcome:
    - `merge_conflict` when step 7 had conflicts;
    - `failing` when there is any new diagnostic or any check fails;
    - `incomplete` when setup failed, a run timed out, or the analyzer was unavailable;
    - `passing` otherwise.
11. Submit the combination check, print the summary, and remove both worktrees with `git worktree remove --force`. Removal also runs on error and cancellation unless `--keep-worktree` is passed.

`.conn/checks.json` gains an optional section:

```json
{
  "pair_verification": {
    "setup": { "command": ["pnpm", "install", "--frozen-lockfile"], "timeout_seconds": 900 },
    "checks": ["gateway-tests", "order-tests"]
  }
}
```

`checks` names keys defined in the same file.

### Analyzer `diagnostics` mode

The mode returns no claims. It returns `diagnostic_counts`: `(code, path, count)` entries aggregated over every discovered program, bounded to 2,000 entries, with a truncation flag. Compiler message text stays excluded.

### Combination check record

Contract `combination-check`:

```text
check_id, project_id, collision_id
left  {intent_id, intent_revision, branch_ref, commit, head_matches_intent}
right {intent_id, intent_revision, branch_ref, commit, head_matches_intent}
merge_result: clean | merge_conflict, conflicted_paths[<=50]
diagnostics {ran, new_count, new[<=50] {code, path}, truncated}
setup? {exit_code, timed_out, duration_ms, output_sha256}
checks[<=20] {key, exit_code, timed_out, duration_ms, output_sha256, state}
outcome: passing | failing | merge_conflict | incomplete
config_source {ref, sha256} | null
runner: local
producer_participant_id, created_at
```

Route: `POST /v1/projects/{project_id}/collisions/{collision_id}/combination-checks`. Any member's Participant may record. Validation confirms that the collision belongs to the Project and that both Intent IDs match it. The table `combination_checks` is append-only and is created in its own migration. The event type `combination.checked` is added to `semanticEventType` and `internal/protocol/events.go`.

Visibility:

- the conflict context pack gains `combination_checks`, the five most recent;
- Radar collision entries and review-packet collisions gain `latest_combination_outcome`;
- `/conn-resolve` suggests `conn verify-pair` through the shell, with human approval, when both branches exist.

## CI credential

Table `ci_credentials`: `credential_id`, `project_id`, `repository_id`, `label`, `token_sha256` (unique), `created_by_human_id`, `created_at`, `expires_at`, `last_used_at`, `revoked_at`. `last_used_at` is updated at most once per minute per credential.

Commands (owner only, human-direct):

```text
conn ci credential create --repository <repository-id> [--label <text>] [--expires-in-days 90]
                          (--github-secret <owner/name> | --print)
conn ci credential list
conn ci credential revoke <credential-id>
```

- `--github-secret` runs `gh secret set CONN_CI_TOKEN --repo <owner/name>` and writes the token to its standard input, so the token never appears on standard output.
- `--print` writes the token to standard output for manual handling.
- Without either option the command refuses, so an agent shell cannot capture the token by accident.
- Steering and prompts tell agents never to create CI credentials.

The token is 32 random bytes encoded after the prefix `conn_ci_`. The backend stores the SHA-256 hash. Creation and revocation append `ci_credential.created` and `ci_credential.revoked` events whose payload contains no token.

### Authentication and route allowlist

The authentication middleware recognizes the `conn_ci_` prefix and resolves a CI principal containing the credential, Project, and repository. Session tokens keep their current path. For a CI principal, only these routes are allowed, and `{project_id}` must equal the credential's Project:

| Method and route                                                               | Purpose                                       |
| ------------------------------------------------------------------------------ | --------------------------------------------- |
| `GET /v1/ci/credential`                                                        | Project, repository, canonical remote, expiry |
| `GET /v1/projects/{project_id}/radar`                                          | Claims, collisions, decisions                 |
| `GET /v1/projects/{project_id}/intents` and `/intents/{intent_id}`             | Trailer resolution                            |
| `GET /v1/projects/{project_id}/intents/{intent_id}/evidence`                   | Packet evidence                               |
| `GET /v1/projects/{project_id}/objectives/{objective_id}/revisions/{revision}` | Acceptance criteria                           |
| `GET /v1/projects/{project_id}/collisions/{collision_id}/context`              | Packet collisions                             |
| `GET /v1/projects/{project_id}/members`                                        | Logins for attribution                        |
| `GET /v1/projects/{project_id}/concepts`                                       | Subject display                               |
| `POST /v1/projects/{project_id}/collision-previews`                            | Unregistered work                             |

Every other route returns 403 with the standard error body. Responses do not reveal whether other Projects exist.

Because work context requires a Human reader, the `review-proof` packet loader gains a reader-independent path built from these routes.

## `conn ci check`

```text
conn ci check [--worktree repo] [--event $GITHUB_EVENT_PATH] [--summary $GITHUB_STEP_SUMMARY] [--comment]
```

Sequence:

1. When `CONN_CI_TOKEN` is absent, write "Conn check skipped: CI credential unavailable (fork pull request or missing secret)" to the summary and exit 0.
2. Read `GET /v1/ci/credential`. Confirm that the worktree remote equals the credential's canonical remote.
3. Read the pull-request number, base SHA, and head SHA from the event file. Compute `base = git merge-base <base-sha> <head-sha>`.
4. Parse `Conn-Intent` trailers in `base..head` with the shared parser.
5. Run observed analysis on `base..head` in the worktree.
6. Preview collisions for the observed claims, excluding the named Intents.
7. For each named Intent, load the packet through the reader-independent loader. Compute drift against the CI-observed claims instead of stored observed claims, with no written-path record.
8. Render the report:
   - a header with counts of Intents, collisions needing attention, previewed collisions, and failing combination checks;
   - unregistered-work or trailer notes (released Intent, older revision, unreadable Intent);
   - previewed collisions;
   - one collapsed `<details>` block per Intent packet.
9. Write the job summary (truncated below 1 MiB). With `--comment`, use `GITHUB_TOKEN` to find the comment containing `<!-- conn-check v1 -->` and update it, or create one, keeping the body under 65,536 characters.
10. Exit 0. Conn, network, and configuration errors appear in the summary.

The command makes no Conn write and runs no project code.

## CI add-on installation

`internal/ciaddon` mirrors `internal/kirointegration`: an embedded template, preview, install, and remove with byte-identical removal and conflict refusal.

CLI: `conn ci preview|install|remove [--root <dir>] [--conn-ref <commit>] [--comment]`.

MCP tools:

| Tool               | Annotation             | Behavior                                                                   |
| ------------------ | ---------------------- | -------------------------------------------------------------------------- |
| `preview_ci_check` | read-only              | Rendered workflow, target path, and prerequisites with exact commands      |
| `install_ci_check` | write, not destructive | Writes `.github/workflows/conn-check.yml` under the trusted workspace root |
| `remove_ci_check`  | destructive            | Removes only byte-identical content                                        |

The MCP tools take `conn_ref` and `comment` only. They cannot choose a path outside the workspace and never create credentials, set secrets or variables, commit, push, or call GitHub.

Prerequisites returned by preview:

1. A Conn backend reachable from GitHub-hosted runners over HTTPS. The local development server is not reachable, unless the team uses a self-hosted runner on the same network.
2. The Conn source repository must be readable from the workflow. The template assumes a public repository.
3. Owner: `conn ci credential create --repository <id> --github-secret <owner/name>`.
4. `gh variable set CONN_SERVER_URL --body <https-url> --repo <owner/name>`.
5. Commit and push the workflow file.

`--conn-ref` defaults to the `vcs.revision` from `debug.ReadBuildInfo`. When the build has no revision or has `vcs.modified=true`, preview requires an explicit `--conn-ref`.

### Workflow template

Placeholders use `@@NAME@@` so they never collide with GitHub `${{ }}` expressions. Action pins match the repository's `.github/workflows/ci.yml`.

```yaml
# Generated by conn ci install (template v1). Conn source @@CONN_REF@@.
name: Conn check

on:
  pull_request:
    types: [opened, synchronize, reopened, ready_for_review]

permissions:
  contents: read
@@PR_WRITE_PERMISSION@@

concurrency:
  group: conn-check-${{ github.event.pull_request.number }}
  cancel-in-progress: true

jobs:
  conn-check:
    runs-on: ubuntu-latest
    timeout-minutes: 15
    steps:
      - name: Check out pull request
        uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
        with:
          ref: ${{ github.event.pull_request.head.sha }}
          fetch-depth: 0
          path: repo
          persist-credentials: false

      - name: Check out Conn
        uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
        with:
          repository: Juanqu261/conn
          ref: @@CONN_REF@@
          path: conn
          persist-credentials: false

      - uses: pnpm/action-setup@73e46992e05b19abd31f2f3c20e0e7370d762115 # v6.1.1
        with:
          version: 12.21.0
      - uses: actions/setup-node@949feb2413d6458794dcd2491c4babbbce0c15c1 # v7.1.0
        with:
          node-version-file: conn/.node-version
      - uses: actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e # v7.0.0
        with:
          go-version: 1.27.4
          cache-dependency-path: conn/go.sum

      - name: Build Conn
        working-directory: conn
        run: |
          pnpm install --frozen-lockfile
          pnpm --filter @conn/typescript-analyzer run build
          go build -o "$RUNNER_TEMP/conn" ./cmd/conn

      - name: Conn check
        env:
          CONN_SERVER_URL: ${{ vars.CONN_SERVER_URL }}
          CONN_CI_TOKEN: ${{ secrets.CONN_CI_TOKEN }}
          CONN_ANALYZER_PATH: ${{ github.workspace }}/conn/packages/typescript-analyzer/dist/cli.js
@@COMMENT_ENV@@
        run: '"$RUNNER_TEMP/conn" ci check --worktree repo @@COMMENT_FLAG@@'
```

The pull request is checked out at its head SHA in `repo/` and Conn in `conn/`, so the analyzer never scans Conn's own sources. With `--comment`, the template adds `pull-requests: write`, `GITHUB_TOKEN: ${{ github.token }}`, and `--comment`. Template version changes are byte-distinct so that removal stays exact.

## Other coding agents

- `internal/agentguide` produces the shared work-loop guidance. The Kiro steering template and the `AGENTS.md` section render from it.
- `conn agents preview|install|remove [--file AGENTS.md] [--root <dir>]` manages the section between `<!-- conn:begin v1 -->` and `<!-- conn:end -->`. Install creates the file when it is absent, appends the section when the markers are absent, and replaces the section only when its content is byte-identical to a known Conn version. Otherwise it reports a conflict.
- `conn mcp config --client generic` prints `{"mcpServers":{"conn":{"command":"<abs conn>","args":["mcp"],"env":{"CONN_SERVER_URL":"<url>"}}}}`. `--client kiro` prints the `kiro-cli mcp add` command from the README. Neither writes a file.
- The MCP adapter reads `clientInfo.name` from initialization. A name other than Kiro sets `X-Conn-Participant-Adapter: mcp`. The backend's `participantAdapter` accepts `kiro` and `mcp`, and the participant schema adds `mcp`. Attribution changes and authority does not.
- Tool descriptions and prompts are revised to refer to "the coding agent" and "the MCP client", with Kiro-only notes labeled.

## `conn start`

```text
conn start <kiro-spec-path | owner/name#number | issue-url | objective-id>
           [--paths <p>...] [--publish | --import-only] [--objective-key <key>]
```

1. Resolve the Project with the CLI precedence.
2. Resolve the Objective:
   - an Objective ID is read directly;
   - for a spec or issue, look for an Objective whose import provenance has the same source reference;
   - with the same content hash, reuse it;
   - with a different hash, show that the source changed and offer a new Objective revision for confirmation;
   - otherwise show the content and suggested key points and import after confirmation.
3. Collect planned paths from `--paths` or a prompt. Run claim suggestions locally with `preview_collisions`.
4. Render the result in the style of the original concept:

```text
Objective: Prevent duplicate checkout charges (github.com/acme/shop/issues/218, revision 1)
Concepts:
  contract:typescript:src/payments/retry.ts/shouldRetryCharge   changes   (analysis)
  invariant:checkout:one-charge-per-key                         changes   (objective)
Previewed collisions: AMBER
  contract_producer_consumer with Intent 0197a9f3@2 on contract:typescript:src/payments/retry.ts/shouldRetryCharge
[P] Publish Intent from this terminal   [K] Continue in Kiro   [Q] Quit
```

5. `P` publishes a CLI Intent with the chosen claims and Git context and writes `focus.json`. `K` prints `/conn-start` with the Objective ID. Non-interactive runs require `--publish` or `--import-only`.

## GitHub issue import

```text
conn objective import-github-issue <owner/name#number | https://github.com/owner/name/issues/number>
                                   [--key <objective-key>] [--accountable <human-id>] [--criteria suggested|none]
```

- The command calls `GET https://api.github.com/repos/{owner}/{name}/issues/{number}` with `Accept: application/vnd.github+json` and `X-GitHub-Api-Version`. It sends `Authorization` only when `GH_TOKEN` or `GITHUB_TOKEN` is set. The token is held in `secret.String`, never logged, never persisted, and never sent to Conn.
- It rejects an issue object that contains `pull_request`.
- It suggests acceptance criteria from list items under the first heading whose text contains "acceptance criteria" (case-insensitive). Keys are slugified from the first words and made unique. At most 20 are suggested. The human reviews them before import.
- Provenance: `source_kind: github_issue`, `source_ref: github.com/<owner>/<name>/issues/<number>`, `source_revision_ref: <updated_at>`, and `content_hash: sha256(title + "\n\n" + body)`.
- Bounds: the title follows the `title` definition. A body over 32,768 characters is refused with guidance to shorten the imported text.
- Backend: `POST /v1/projects/{project_id}/objective-imports/github-issue`. It reuses the import handler, and `objective-import-input.source_kind` becomes the enum `kiro_spec | github_issue`. A database constraint on source kind, if present, is widened in a migration.

## Insights

Route `GET /v1/projects/{project_id}/insights?since_days=30` (1 to 365), contract `project-insights`, CLI `conn insights [--since-days 30] [--json]`.

| Metric                                                              | Source                                                                                                              |
| ------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------- |
| Collisions opened per rule                                          | `collision_assessments.created_at` within the window                                                                |
| Decisions per kind and rule; false-positive share per rule          | `resolution_decisions` joined to assessments                                                                        |
| Median time from open to first decision                             | Same join                                                                                                           |
| Share decided before either Intent was released                     | Decision time compared with `intent.released` events                                                                |
| Share of analyzed Intent revisions with undeclared observed changes | `intent_analysis_reports` joined to declared and observed claims of the same revision, `changes` and `removes` only |
| Acceptance criteria by provenance (observed run, reported, missing) | Evidence on active Objective revisions                                                                              |
| Combination checks by outcome                                       | `combination_checks`                                                                                                |

Every ratio carries its numerator and denominator and becomes `insufficient_data` when the denominator is below 5. The schema has no property that can hold a Human, Participant, or login. The route accepts no grouping parameter.

## Contracts and persistence summary

| Artifact                                                       | Change                                                                           |
| -------------------------------------------------------------- | -------------------------------------------------------------------------------- |
| `collision-preview-input`, `collision-preview`                 | New                                                                              |
| `combination-check`                                            | New                                                                              |
| `ci-credential`, `ci-credential-list`, `ci-credential-context` | New                                                                              |
| `project-insights`                                             | New                                                                              |
| `checks-config`                                                | Adds `pair_verification`                                                         |
| `conflict-context-pack`, `radar-view`                          | Add combination check references and outcomes                                    |
| `objective-import-input`                                       | `source_kind` enum adds `github_issue`                                           |
| `participant`                                                  | `adapter_kind` adds `mcp`                                                        |
| `common` `semanticEventType`                                   | Adds `combination.checked`, `ci_credential.created`, `ci_credential.revoked`     |
| Migrations                                                     | `combination_checks`, `ci_credentials`, source-kind and adapter-kind constraints |

## Security and privacy

- CI tokens use a scannable prefix, are hashed at rest, are delivered to `gh` through standard input, and are redacted from logs and errors.
- The CI principal reads one Project through an explicit route allowlist and cannot write.
- The workflow uses `pull_request`, minimal permissions, SHA-pinned actions, and non-persisted checkout credentials. It runs no pull-request code. Fork pull requests receive no secret and are skipped.
- Pair verification runs teammates' code only after explicit confirmation and reads commands from the default branch, so a feature branch cannot choose what runs on a teammate's machine.
- The GitHub token used for import stays in the local process.
- Insights cannot express per-person data.
- No MCP tool creates credentials, runs checks, or runs pair verification.

## Error behavior

| Condition                                    | Behavior                                                       |
| -------------------------------------------- | -------------------------------------------------------------- |
| Branch missing for pair verification         | Fails before creating worktrees and suggests `--fetch`         |
| Worktree cleanup fails                       | Prints the leftover path and the `git worktree remove` command |
| CI backend unreachable or credential expired | Summary explains the cause and the job exits 0                 |
| Trailer names an unknown Intent              | Reported as unreadable, without revealing other Projects       |
| Template conflict                            | Installer refuses and shows the difference                     |
| GitHub API rate limit during import          | Fails with the reset time and suggests `GH_TOKEN`              |

## Validation plan

1. **Collision preview (PostgreSQL):** the preview equals the persisted assessor result for the same claims and writes no rows or events.
2. **Pair verification (Go, temporary Git repository):** branch A changes `shouldRetryCharge` to require a second parameter, and branch B adds a call with one argument. Each type-checks alone, and the merged tree yields a new diagnostic and a `failing` outcome. Also covers a merge-conflict case, default-branch configuration despite a modified feature-branch configuration, `CONN_` variable stripping, and cleanup after cancellation.
3. **CI credential (PostgreSQL and HTTP, parameterized over routes):** the allowlist is enforced, another Project is denied, expiry and revocation take effect immediately, the database stores only the hash, events are appended, and creation without `--github-secret` or `--print` is refused.
4. **CI check (Go, fake backend, simulated event file):** a trailer-named Intent, unregistered work with a previewed collision, a released Intent, a missing token, summary and comment bounds, and the comment update by marker.
5. **Templates and installers:** workflow assertions (no `pull_request_target`, 40-character SHA pins, minimal permissions, `persist-credentials: false`), CI installer conflict and removal, and the `AGENTS.md` section lifecycle.
6. **GitHub issue import (fake GitHub server):** provenance, criteria suggestions, pull-request rejection, the token absent from Conn requests, and the oversized-body refusal.
7. **Insights (PostgreSQL):** small-denominator suppression and a schema that cannot hold people.
8. **Manual:** on a real GitHub repository with a deployed backend, install the add-on from MCP, create the credential with `--github-secret`, open a pull request with trailers, open one without trailers that touches a consumed contract, open a fork pull request, and run `conn verify-pair` on a real collision. Record sanitized results in `docs/real-trial.md`.

Run `pnpm check`, `pnpm test`, and `pnpm build`.

## Deferred

- Failing pull requests on findings and merge gating (Phase 4).
- CI writes to Conn, including CI-attested evidence with `runner: "ci"` and CI-run pair verification.
- GitHub App, GitLab, Jira, Linear, Slack, and Teams.
- Hooks for clients other than Kiro.
- OpenAPI, GraphQL, SQL, and infrastructure extractors.
- Web application views over Radar, conflict room, combination checks, packets, and insights.
