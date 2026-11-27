# Tasks

- [ ] 1. Review and approve the verification, CI, and adoption scope
  - Confirm the acceptance goal: pair verification, a pull-request check installed by Conn, and lower adoption cost.
  - Confirm that CI reports only, never fails a pull request for findings, and writes nothing to Conn.
  - Confirm the CI credential scope, owner-only creation through human-direct surfaces, and delivery through `--github-secret` or `--print` only.
  - Confirm that pair verification runs teammates' code only after confirmation and reads commands from the default branch.
  - Confirm that `review-proof` is complete or that its check runner, trailer parser, and packet loader are available.
  - Do not begin implementation until the specification and decision gate are approved. Requirements: 1, 4, 5, 6, 13.

- [x] 2. Record decision 0008 and select CI distribution
  - Compare building from pinned source, release artifacts, and a container image for obtaining `conn` and the analyzer in CI. Recommend one and stop for approval.
  - Add `docs/decisions/0008-verification-ci-and-adoption.md` covering the CI add-on, the credential, report-only CI, the selected distribution, pair verification and analyzer `diagnostics` mode, GitHub issue import token handling, insights privacy, and `mcp` attribution.
  - Record that it replaces the GitHub App sequence for this version and amends 0003.
  - Update `docs/decisions/README.md` and `.kiro/steering/tech.md`. Requirements: 4, 5, 6, 8, 11, 12.

- [x] 3. Define the contracts
  - Add `collision-preview-input`, `collision-preview`, `combination-check`, `ci-credential`, `ci-credential-list`, `ci-credential-context`, and `project-insights`.
  - Add `pair_verification` to `checks-config`, combination references to conflict context and Radar, `github_issue` to Objective import, `mcp` to Participant adapters, and the new event types.
  - Ensure `project-insights` has no property that can hold a Human, Participant, or login.
  - Add accepted and rejected fixtures and matching Go types.
  - Stop for vocabulary review before backend changes. Requirements: 2, 3, 4, 9, 11, 12.

- [x] 4. Implement the collision preview
  - Extract the pure assessor core and use it from both the persisted assessor and the preview.
  - Add `POST /v1/projects/{project_id}/collision-previews` in a read-only transaction with input bounds.
  - Add `preview_collisions` to `suggest_claims`.
  - Add one PostgreSQL scenario proving equality with the persisted result and the absence of writes. Requirements: 3.

- [x] 5. Implement combination check records
  - Add the `combination_checks` migration, the append-only record, the route, and the `combination.checked` event.
  - Add combination checks to conflict context and latest outcomes to Radar and the review packet.
  - Cover recording authority and visibility in task 6's scenario. Requirements: 2.

- [x] 6. Implement `conn verify-pair`
  - Add the analyzer `diagnostics` mode with aggregated, bounded counts.
  - Implement branch resolution, optional fetch, default-branch configuration, confirmation, the two worktrees, the diagnostic delta, the merge, setup and pair checks through the shared check runner, the outcome rules, submission, and guaranteed cleanup.
  - Add one Go scenario on a temporary Git repository covering the failing interaction, a merge conflict, default-branch configuration, environment stripping, and cleanup after cancellation. Requirements: 1, 2, 14.

- [x] 7. Implement the CI credential
  - Add the `ci_credentials` migration, token generation with the `conn_ci_` prefix, hashing, expiry, revocation, throttled `last_used_at`, and audit events.
  - Add the CI principal to authentication with the route allowlist, the Project match, and `GET /v1/ci/credential`.
  - Add owner-only `conn ci credential create|list|revoke` with `--github-secret` and `--print` and refusal otherwise.
  - Add one PostgreSQL and HTTP scenario parameterized over routes. Requirements: 4, 13.

- [x] 8. Implement `conn ci check`
  - Add the reader-independent packet loader path.
  - Implement event parsing, merge-base computation, trailer resolution, observed analysis, the collision preview, per-Intent packets with CI-observed drift, the report, the bounded job summary, and the optional marker comment.
  - Skip cleanly without a credential and exit 0 for every finding and error.
  - Add one Go scenario with a fake backend and a simulated event file. Requirements: 5.

- [x] 9. Implement the CI add-on installer
  - Add `internal/ciaddon` with the embedded template, the placeholders, the `--comment` variant, the build-revision default for `--conn-ref`, and conflict-safe preview, install, and remove.
  - Add `conn ci preview|install|remove` and the `preview_ci_check`, `install_ci_check`, and `remove_ci_check` MCP tools, confined to the trusted workspace root.
  - Return the prerequisites with exact commands from preview.
  - Add template assertions and an installer lifecycle test. Requirements: 6, 7, 8.

- [x] 10. Support other coding agents
  - Add `internal/agentguide` as the single source for Kiro steering and the `AGENTS.md` section.
  - Add `conn agents preview|install|remove` with marker-delimited, conflict-safe editing, and `conn mcp config --client generic|kiro`.
  - Detect non-Kiro MCP clients, send adapter kind `mcp`, and accept it in the backend.
  - Revise MCP tool descriptions and prompts for any MCP client.
  - Add one test for the `AGENTS.md` section lifecycle. Requirements: 9.

- [x] 11. Implement GitHub issue import
  - Add `conn objective import-github-issue` with the REST fetch, optional environment token, pull-request rejection, criteria suggestions, bounds, provenance, and the repository-connection warning.
  - Add the backend import route and widen the source-kind vocabulary.
  - Add one scenario against a fake GitHub server confirming that the token never reaches Conn. Requirements: 11, 13.

- [x] 12. Implement `conn start`
  - Add Objective resolution by ID, spec, or issue with reuse, a changed-source revision offer, and confirmed import.
  - Add planned-path collection, local suggestions with the collision preview, the summary display, and the publish, continue-in-Kiro, and quit choices.
  - Require `--publish` or `--import-only` when input is not interactive.
  - Cover it with one CLI scenario against the test backend. Requirements: 10.

- [x] 13. Implement Project insights
  - Add the insights queries, the route with the 1-to-365-day window, the small-denominator suppression, and `conn insights`.
  - Add one PostgreSQL scenario for metric values, suppression, and the absence of people. Requirements: 12.

- [x] 14. Update workflows, steering, and documentation
  - Update `/conn-resolve` to suggest `conn verify-pair` with human approval, and state in steering that agents never create CI credentials.
  - Document the CI add-on, its prerequisites, the credential, pair verification, other-agent setup, `conn start`, issue import, and insights in `README.md`, `docs/architecture.md`, and `docs/code-structure.md`. Requirements: 1, 4, 6, 9, 10.

- [x] 15. Validate the milestone automatically
  - Run the scenarios from tasks 4 to 13 together with `pnpm check`, `pnpm test`, and `pnpm build`.
  - Confirm the failing-interaction, default-branch-configuration, unregistered-work, and credential-rejection criteria. Requirements: 14.

- [ ] 16. Validate on a real GitHub repository
  - With a deployed HTTPS backend, install the add-on from MCP, create the credential with `--github-secret`, and set `CONN_SERVER_URL`.
  - Open a pull request with trailers, one without trailers that changes a consumed contract, and one from a fork. Run `conn verify-pair` on a real collision.
  - Record sanitized summaries, comment behavior, build time, and friction in `docs/real-trial.md`.
  - Do not mark this task complete from expected results alone. Requirements: 14.
