# Tasks

- [ ] 1. Review and approve the pre-edit warning scope
  - Confirm the milestone acceptance goal: two Kiro sessions that never exchange subject strings receive a correct warning before the second agent writes a conflicting file.
  - Confirm that pre-edit claims are declared claims built from `suggest_claims` candidates, with no new provenance kind.
  - Confirm the decision-to-state mapping, the carry-forward subset rule, and the non-owner dismissal restriction.
  - Confirm warn-only hooks, backend-read-only hooks, and local state under the worktree's Git directory.
  - Do not begin implementation until the specification and decision gate are approved. Requirements: 1, 8, 9, 11, 15.

- [x] 2. Record decision 0006 for pre-edit coordination
  - Add `docs/decisions/0006-pre-edit-coordination.md` covering declared pre-edit claims, analyzer planned and observed modes with read-only Git, analyzer resolution, decision-driven collision states, command hooks, and non-authoritative hook state.
  - State that it amends 0002 (bundle content and hook boundary) and 0003 (analyzer input and Git access).
  - Update `docs/decisions/README.md` and the matching lines in `.kiro/steering/tech.md`. Requirements: 6, 7, 8, 9, 13.

- [ ] 3. Prove Kiro command-hook behavior
  - In Kiro CLI V3 and the IDE, verify PreToolUse exit-code 1 display and tool continuation, PostToolUse standard-output delivery to the agent, the `tool_input` shape for built-in write tools, the standalone `timeout` field name and unit, and Windows quoting for an absolute executable path containing spaces.
  - Record the results in this design's capability-proof section and choose the non-blocking warning channel.
  - Never use `deny`, `ask`, or exit code 2. Stop for review if no documented non-blocking channel shows the warning to the human. Requirements: 10, 11, 12.

- [x] 4. Define the version 2 contracts
  - Add `concept-list.schema.json`.
  - Change `local-analyzer-report.schema.json` to version 2 with `mode` and base and head references.
  - Add `matched_subjects` and `decision_ref` to `collision-assessment.schema.json` and `decided_collisions` to `radar-view.schema.json`.
  - Add accepted and rejected fixtures, update the handwritten Go types, and keep the Go and Node.js validators green.
  - Stop for vocabulary review before backend changes. Requirements: 2, 6, 8.

- [x] 5. Implement the collision lifecycle
  - Add migration `00008_collision_lifecycle.sql` with `matched_subjects`, `decision_id`, the backfill, and the carry-forward index.
  - Store matched subjects in the assessor, set state and decision reference in `DecideResolution`, and enforce the non-owner `false_positive` rule.
  - Apply carry-forward for subsets, reopen for supersets, and append `collision.updated` or `collision.opened` in the same transaction.
  - Update Radar `decided_collisions`, work-context `related_collisions`, `check_collisions` grouping, and `resolutionDependencies`.
  - Add one PostgreSQL scenario parameterized by decision kind that covers the mapping, authority, carry-forward, reopening, and events. Requirements: 8.

- [x] 6. Implement the concept catalog and near-match advice
  - Add Objective-derived subjects, the catalog read model, and the deterministic similarity function in `internal/application`.
  - Add `GET /v1/projects/{project_id}/concepts`, the authenticated client method, the `list_concepts` MCP tool, and `conn concepts`.
  - Add near-match observations to Intent publication and revision responses, bounded to 20.
  - Add one catalog scenario covering stability across Objective revisions, ranking, bounds, and the fact that near-match advice changes nothing. Requirements: 1, 2, 3.

- [x] 7. Implement analyzer version 2
  - Add the version 2 request with `mode`, `symbols`, and `base_revision_ref`, and validate references before any Git call.
  - Implement planned mode (`changes` for exports, `consumes` for internal imports, new-path diagnostic).
  - Implement observed mode (changed set from Git, base parsing, declaration fingerprints, `changes`, `removes`, `produces`, `consumes`, base-unavailable diagnostic).
  - Apply the truncation priority and diagnostic, and run Git with fixed arguments, no shell, a timeout, and a minimized environment.
  - Add Vitest scenarios on a temporary Git repository, including a source-marker exclusion check. Requirements: 4, 6, 15.

- [x] 8. Resolve the analyzer in any connected repository
  - Resolve the analyzer from `CONN_ANALYZER_PATH`, the executable directory, then the current directory, and Node.js from `CONN_NODE_PATH` or `PATH`.
  - Report availability, the chosen source, and the Node.js version in `session_status` and `conn doctor`.
  - Extend the existing subprocess adapter test for resolution order. Requirements: 7.

- [x] 9. Add claim suggestions and Git context
  - Add the `suggest_claims` MCP tool and `conn intent suggest-claims`, merging analyzer and catalog candidates with source labels, bounds, and the new-path list.
  - Fill `repository_id`, `branch_ref`, and `base_revision_ref` in `publish_intent` and `revise_intent` when omitted, and report `filled_fields`.
  - Switch `analyze_intent` to observed mode with the Intent's base revision or the computed merge base.
  - Cover the behavior in the two-participant scenario of task 15 rather than in separate tests. Requirements: 4, 5, 6.

- [x] 10. Implement local hook state
  - Add `internal/hookstate` with Git-directory resolution, atomic JSON writes, the lock file, bounded appends, and tolerant reads.
  - Write and clear `focus.json` from the MCP adapter after Intent publication, revision, exact focus, and release.
  - Store no credentials, prompts, source, or Project prose. Requirements: 9, 15.

- [x] 11. Implement the `conn hook` commands
  - Add `session-start`, `prompt-submit`, `pre-write`, and `post-write` with the shared time budget, fail-open behavior, output label, and 1,500-character bound.
  - Implement the collision-state comparison and cursor update in `prompt-submit`, the three warnings and pending queue in `pre-write`, and the written-path record in `post-write`.
  - Use the warning channel selected in task 3.
  - Add one Go scenario with a fake backend covering every warning, one-time reporting, fail-open behavior, and a check that no hook exits 2 or prints a permission decision. Requirements: 10, 11, 12.

- [x] 12. Ship Kiro bundle version 2
  - Replace the agent-prompt hook template with `conn-hooks.json` command hooks and update the steering template.
  - Render the executable path by default and support `--command-on-path`.
  - Migrate an unchanged version 1 hook file, preserve a modified one, and keep preview write-free.
  - Extend the existing bundle test for install, migration, conflict, and removal. Requirements: 13.

- [x] 13. Update workflow prompts
  - Rewrite `/conn-start` for catalog lookup, planned suggestions, publication with chosen claims, and collision checks before any write.
  - Make `/conn-finish` use observed analysis without paths.
  - Describe missing Blueprints and Charters as optional context in prompts, work context, and Radar known unknowns. Requirements: 1, 14.

- [x] 14. Update documentation
  - Update `README.md` and `docs/architecture.md` with the purpose from decision 0005, the pre-edit loop, analyzer resolution, the hook behavior, and the collision lifecycle.
  - Update `docs/code-structure.md` for `internal/hookstate` and the hook commands.
  - Add a two-session pre-edit section to `docs/real-trial.md`. Requirements: 13, 14.

- [x] 15. Validate the milestone automatically
  - Add `fixtures/pre-edit-two-sessions` with one `tsconfig.json`, `src/payments/retry.ts` exporting `shouldRetryCharge`, and `src/orders/checkout.ts` importing it, plus a README.
  - Add a two-participant MCP scenario: both participants call `list_concepts` and `suggest_claims`, publish, and receive `contract_producer_consumer` and `shared_declared_invariant` assessments before any file write. Then run `pre-write` for the second participant's file, `prompt-submit` for the first, a dismissal, and a revision without new subjects.
  - Run `pnpm check`, `pnpm test`, and `pnpm build`. Requirements: 16.

- [ ] 16. Validate the milestone in two real Kiro sessions
  - Repeat task 15 with two people, two GitHub accounts, two worktrees, and the version 2 bundle installed.
  - Record sanitized identifiers, the warning text each person saw, prompt-submit output, hook latency, and friction in `docs/real-trial.md`.
  - Do not mark this task complete from expected results alone. Requirements: 16.
