# Requirements

Milestone 3 of decision 0005. Acceptance goal: a team can test two collided changes together, sees Conn warnings and the review packet on every pull request through a CI add-on that Conn installs, and can adopt Conn with less setup and outside Kiro.

This milestone depends on `pre-edit-collision-warnings` (observed analysis, collision lifecycle, hook state) and `review-proof` (check configuration, trailers, review packet).

## 1. Pair verification

**User story:** As a developer whose Intent collides with a teammate's, I want to test both branches together so that we learn whether the changes break each other before either merges.

### Acceptance criteria

1. WHEN a human runs `conn verify-pair <collision-id>`
   THE SYSTEM SHALL require that the collision is not stale, that both Intents name a branch, and that the local worktree matches their repository
2. WHEN a branch is not available locally
   THE SYSTEM SHALL report it and SHALL fetch only when the human passes `--fetch`
3. WHEN verification starts
   THE SYSTEM SHALL warn that code from both branches will run on this machine and SHALL require confirmation, or `--yes` for non-interactive use
4. WHEN the branches merge with conflicts
   THE SYSTEM SHALL record a merge-conflict outcome with the bounded list of conflicted paths and SHALL run no checks
5. WHEN the branches merge cleanly
   THE SYSTEM SHALL compare TypeScript diagnostics of the merged tree with each branch alone and SHALL run the configured setup command and pair checks in the merged tree
6. THE SYSTEM SHALL read pair-verification commands from `.conn/checks.json` on the repository default branch, never from either feature branch
7. WHEN any new diagnostic appears only in the merged tree or any pair check fails
   THE SYSTEM SHALL record a failing outcome, and otherwise a passing or incomplete outcome
8. THE SYSTEM SHALL remove temporary worktrees after success, failure, or cancellation unless the human passes `--keep-worktree`
9. THE SYSTEM SHALL pass no Conn credential to setup or check commands

## 2. Combination check records

**User story:** As participants in a conflict, we want the result of a combined test to be part of the collision record so that a decision can rely on it.

### Acceptance criteria

1. WHEN pair verification finishes
   THE SYSTEM SHALL store an immutable combination check with both Intent revisions and commits, the merge result, the diagnostic delta, check results, the outcome, the configuration source, and the producing Participant
2. THE SYSTEM SHALL append a `combination.checked` event in the same transaction
3. WHEN a member's Participant records a combination check
   THE SYSTEM SHALL accept it and SHALL NOT change the collision state
4. WHEN conflict context, a review packet, or Radar shows a collision
   THE SYSTEM SHALL include its latest combination check outcome

## 3. Collision preview without publication

**User story:** As a participant or CI check, I want to know which active Intents a set of claims would collide with, without publishing anything.

### Acceptance criteria

1. WHEN a member or CI credential submits candidate claims and optional Intents to exclude
   THE SYSTEM SHALL return the collisions the deterministic assessor would open against active Intent revisions, including any inherited decision state
2. THE SYSTEM SHALL persist nothing and append no event for a preview
3. THE SYSTEM SHALL use the same rule implementation as the assessor
4. THE SYSTEM SHALL bound a preview to 500 claims and 20 excluded Intents
5. WHEN `suggest_claims` receives `preview_collisions: true`
   THE SYSTEM SHALL return the preview for its candidates

## 4. CI credential

**User story:** As a Project owner, I want a narrow credential for CI so that the pull-request check can read Conn without a personal session.

### Acceptance criteria

1. WHEN an owner runs `conn ci credential create --repository <repository-id>`
   THE SYSTEM SHALL create a credential bound to that Project and RepositoryConnection with an expiry between 1 and 365 days, defaulting to 90
2. THE SYSTEM SHALL create CI credentials only through human-direct CLI or terminal-application surfaces and SHALL reject creation from MCP or a CI credential
3. WHEN the credential is created
   THE SYSTEM SHALL deliver the token only to `gh secret set` when `--github-secret <owner/name>` is passed, or to standard output only when `--print` is passed, and otherwise SHALL refuse
4. THE SYSTEM SHALL store only the token's SHA-256 hash and SHALL prefix tokens with `conn_ci_` so that secret scanners can detect them
5. WHEN a request uses a CI credential
   THE SYSTEM SHALL allow only the read routes and the collision preview route that the CI check needs, for its own Project
6. WHEN a CI credential is expired or revoked
   THE SYSTEM SHALL reject it immediately
7. WHEN an owner lists credentials
   THE SYSTEM SHALL show identifier, label, repository, creator, creation, expiry, last use, and revocation, but never the token
8. THE SYSTEM SHALL append audit events for creation and revocation

## 5. Pull-request check

**User story:** As a reviewer, I want every pull request to show its Intents, collisions, review packet, and any unregistered changes, so that I see Conn results without asking the author.

### Acceptance criteria

1. WHEN `conn ci check` runs for a pull request
   THE SYSTEM SHALL read the Intents named by `Conn-Intent` trailers in the pull request's commits
2. THE SYSTEM SHALL run observed analysis on the pull request's base-to-head diff and preview collisions for the observed claims, excluding the pull request's own Intents
3. WHEN trailers name Intents
   THE SYSTEM SHALL render a review packet for each Intent, with drift computed against the CI-observed claims
4. WHEN no trailer names an Intent
   THE SYSTEM SHALL report the pull request as unregistered work and show the previewed collisions
5. WHEN a trailer names a released Intent, an older revision, or an Intent the credential cannot read
   THE SYSTEM SHALL say so explicitly
6. THE SYSTEM SHALL write the report to the job summary and, when enabled, create or update one pull-request comment identified by a fixed marker
7. THE SYSTEM SHALL keep the comment under 65,536 characters and the job summary under 1 MiB
8. THE SYSTEM SHALL exit successfully for every finding and every Conn or configuration error, and SHALL describe errors in the summary
9. WHEN the CI credential is unavailable, including on pull requests from forks
   THE SYSTEM SHALL report that the check was skipped and exit successfully
10. THE SYSTEM SHALL write nothing to Conn from CI in this version

## 6. CI add-on installation from MCP and CLI

**User story:** As a developer working in Kiro, I want my agent to add the Conn pull-request check to my project, while I keep control of credentials.

### Acceptance criteria

1. WHEN `conn ci preview` or the `preview_ci_check` MCP tool runs
   THE SYSTEM SHALL return the exact workflow file, its target path, and the prerequisites with exact commands, and SHALL write nothing
2. WHEN `conn ci install` or the `install_ci_check` MCP tool runs
   THE SYSTEM SHALL write `.github/workflows/conn-check.yml` and SHALL refuse to overwrite different content
3. WHEN `conn ci remove` or the `remove_ci_check` MCP tool runs
   THE SYSTEM SHALL remove only byte-identical Conn content and SHALL preserve modified files for manual review
4. THE SYSTEM SHALL NOT create credentials, set secrets or variables, commit, push, or call GitHub from the installer
5. WHEN rendering the workflow
   THE SYSTEM SHALL pin the Conn source revision to the running build's commit or to an explicit `--conn-ref`
6. THE SYSTEM SHALL list among the prerequisites that the Conn backend must be reachable from GitHub runners over HTTPS

## 7. Workflow safety

### Acceptance criteria

1. THE SYSTEM SHALL trigger the workflow on `pull_request` and SHALL NOT use `pull_request_target`
2. THE SYSTEM SHALL grant `contents: read` and SHALL add `pull-requests: write` only when comments are enabled
3. THE SYSTEM SHALL pin every action to a full commit SHA
4. THE SYSTEM SHALL check out code with `persist-credentials: false`
5. THE SYSTEM SHALL expose the CI credential only to the `conn ci check` step
6. THE SYSTEM SHALL run no code from the pull request in the default workflow

## 8. Conn distribution in CI

### Acceptance criteria

1. THE SYSTEM SHALL obtain the `conn` executable and analyzer in CI by the method selected in decision 0008
2. WHEN the selected method builds from source
   THE SYSTEM SHALL build from the pinned revision with the repository's pinned toolchain versions and cache dependencies

## 9. Other coding agents

**User story:** As a developer who uses a coding agent other than Kiro, I want the same Conn loop through MCP and repository instructions.

### Acceptance criteria

1. WHEN a human runs `conn agents preview|install|remove`
   THE SYSTEM SHALL manage one delimited Conn section in `AGENTS.md`, or another named Markdown file, without changing content outside the markers
2. WHEN the Conn section was modified
   THE SYSTEM SHALL preserve it and report a conflict
3. THE SYSTEM SHALL generate the section and the Kiro steering from one shared source
4. WHEN a human runs `conn mcp config --client <generic|kiro>`
   THE SYSTEM SHALL print a stdio configuration snippet or the `kiro-cli mcp add` command and SHALL write nothing
5. THE SYSTEM SHALL word MCP tool descriptions and prompts for any MCP client and keep Kiro-specific notes labeled
6. WHEN the MCP client identifies itself as a client other than Kiro during initialization
   THE SYSTEM SHALL attribute its Participant with adapter kind `mcp` instead of `kiro`

## 10. One-step start

**User story:** As a developer starting work from a ticket or spec, I want one command that brings the Objective into Conn and shows what my planned change would collide with.

### Acceptance criteria

1. WHEN a human runs `conn start <source>` with a Kiro spec path, a GitHub issue reference, or an Objective ID
   THE SYSTEM SHALL resolve the Project and resolve or import the Objective after showing the content for confirmation
2. WHEN the source was imported before with the same content hash
   THE SYSTEM SHALL reuse the Objective
3. WHEN the source content changed since import
   THE SYSTEM SHALL say so and offer a new Objective revision for confirmation
4. WHEN planned paths are supplied or entered
   THE SYSTEM SHALL show suggested concepts and previewed collisions with their severity
5. WHEN the human chooses to publish
   THE SYSTEM SHALL publish a CLI Intent with the chosen claims and Git context and write the local focus record
6. WHEN the human chooses to continue in Kiro
   THE SYSTEM SHALL print the `/conn-start` invocation for the Objective and publish nothing
7. WHEN input is not interactive
   THE SYSTEM SHALL require `--publish` or `--import-only`

## 11. GitHub issue import

**User story:** As a team that tracks work in GitHub issues, I want to import an issue as an Objective with provenance.

### Acceptance criteria

1. WHEN a human runs `conn objective import-github-issue <owner/name#number | URL>`
   THE SYSTEM SHALL fetch the issue from the GitHub REST API, using `GH_TOKEN` or `GITHUB_TOKEN` only when present
2. THE SYSTEM SHALL NOT persist the GitHub token or send it to the Conn backend
3. WHEN the reference is a pull request
   THE SYSTEM SHALL reject it
4. WHEN the issue body has a heading containing "Acceptance criteria" followed by list items
   THE SYSTEM SHALL suggest up to 20 keyed acceptance criteria for human review
5. WHEN the human confirms
   THE SYSTEM SHALL import the Objective with source kind `github_issue`, a stable source reference, the issue update time as source revision, and a content hash
6. WHEN the issue repository is not a RepositoryConnection of the Project
   THE SYSTEM SHALL warn before import
7. WHEN the issue body exceeds the Objective overview bound
   THE SYSTEM SHALL refuse and ask the human to shorten the imported text

## 12. Project insights

**User story:** As a team tuning Conn, I want Project-level numbers on warnings and evidence so that we know which rules help, without measuring individuals.

### Acceptance criteria

1. WHEN a member runs `conn insights` or reads the insights route for a window of up to 365 days
   THE SYSTEM SHALL report collisions opened per rule, decisions per kind and rule, the false-positive share per rule, median time to first decision, the share of collisions decided before either Intent was released, the share of analyzed Intent revisions with undeclared observed changes, acceptance criteria by evidence provenance, and combination checks by outcome
2. THE SYSTEM SHALL NOT include any Human, Participant, or login, any per-person grouping, or any ranking
3. WHEN a metric's denominator is below 5
   THE SYSTEM SHALL report insufficient data instead of a value

## 13. Safety, privacy, and authority

### Acceptance criteria

1. THE SYSTEM SHALL keep source text, diffs, prompts, transcripts, credentials, and raw logs out of combination checks, CI reports, previews, imports, and insights
2. THE SYSTEM SHALL redact CI tokens and GitHub tokens from logs and errors
3. THE SYSTEM SHALL expose no MCP tool that creates credentials, runs checks, or runs pair verification
4. THE SYSTEM SHALL keep every installer's preview write-free and conflict-safe
5. THE SYSTEM SHALL keep CI results advisory and free of binding decisions

## 14. Milestone validation

### Acceptance criteria

1. WHEN two branches each type-check alone but fail together
   THE SYSTEM SHALL record a failing combination check that names the new diagnostic
2. WHEN a feature branch changes `.conn/checks.json`
   THE SYSTEM SHALL still run the default branch's pair commands
3. WHEN a pull request with trailers runs the installed workflow against a deployed backend
   THE SYSTEM SHALL show the packet and collisions in the job summary
4. WHEN a pull request has no trailer but changes a contract that an active Intent consumes
   THE SYSTEM SHALL report unregistered work with the previewed collision
5. WHEN a CI credential calls a write route or another Project
   THE SYSTEM SHALL reject it
6. THE SYSTEM SHALL demonstrate the CI add-on on a real GitHub repository, including a fork pull request, and record sanitized results
