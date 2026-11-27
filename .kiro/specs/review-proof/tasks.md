# Tasks

- [ ] 1. Review and approve the review-proof scope
  - Confirm the acceptance goal: a reviewer can decide whether to trust an agent-produced pull request from the packet alone.
  - Confirm that only the CLI runs check commands, that local observations are labeled as attested by the owner's machine, and that evidence may come from any Participant of the Intent's Human.
  - Confirm the trailer format and the 60,000-character Markdown bound.
  - Confirm that `pre-edit-collision-warnings` is complete or that its observed analysis, Git context, and hook state are available.
  - Do not begin implementation until the specification and decision gate are approved. Requirements: 3, 5, 6, 9.

- [x] 2. Record decision 0007 for review proof
  - Add `docs/decisions/0007-review-proof.md` covering CLI-run checks, the observation trust level, same-Human evidence attachment, trailers and explicit Git hook installation, and the generated packet.
  - Update `docs/decisions/README.md` and `.kiro/steering/tech.md`. Requirements: 3, 4, 5, 6, 7.

- [x] 3. Define the contracts
  - Add `checks-config.schema.json` and `review-packet.schema.json`.
  - Add kind `check_run` and the optional `observation` to the evidence schemas, `provenance` and `commit` to Radar coverage, and `focus.commit_trailer` to work context.
  - Keep `observation` out of the MCP `attach_evidence` input schema.
  - Add accepted and rejected fixtures and matching Go types.
  - Stop for vocabulary review before backend changes. Requirements: 2, 4, 6, 7.

- [x] 4. Extend the evidence backend
  - Add migration `00009_evidence_observation.sql`.
  - Change `AttachEvidence` to a same-Human check that records the calling Participant as producer.
  - Add `GET /v1/projects/{project_id}/intents/{intent_id}/evidence` and its client method.
  - Add provenance and commit to Radar coverage.
  - Add one PostgreSQL scenario for same-Human acceptance, other-Human rejection, observation storage, and the listing order. Requirements: 4, 5.

- [x] 5. Implement `conn evidence run`
  - Load and validate `.conn/checks.json`, resolve the Intent, and verify the repository and acceptance keys before execution.
  - Run the argument array without a shell, with the environment minus `CONN_` variables, the timeout, and process-group termination.
  - Stream output, hash it, keep a 10 MiB local log, and attach one evidence reference per acceptance key.
  - Add one Go scenario on a temporary Git repository covering passing, failing, timeout, environment stripping, the dirty flag, and the payload. Requirements: 2, 3, 9.

- [x] 6. Implement scope drift
  - Add `internal/review` drift functions for matched, undeclared, unobserved, files without claims, not observable, and analysis status.
  - Read `written.jsonl` to mark agent-written files.
  - Add one parameterized Go test. Requirements: 1.

- [x] 7. Implement commit trailers
  - Add `conn trailer` using `git interpret-trailers` and `focus.json`, always exiting 0.
  - Add `conn git-hook preview|install|remove` for `commit-msg` with `core.hooksPath` support and conflict safety; retire an unchanged version 1 `prepare-commit-msg` hook.
  - Return `commit_trailer` in work context and add the trailer parser to `internal/review`.
  - Add one Go scenario on a temporary Git repository. Requirements: 6.

- [x] 8. Implement the review packet
  - Add the shared loader with the cursor-consistency retry, and move the handoff to it without changing handoff output.
  - Build the packet, validate the JSON form, and render bounded, sanitized Markdown with truncation notes.
  - Add `conn review`, the read-only `read_review_packet` MCP tool, and the `/conn-review` prompt.
  - Add one Go test for schema validity, the size bound, sanitization, the source-marker check, and the integrity flags. Requirements: 7, 8.

- [x] 9. Update workflows and steering
  - Rewrite `/conn-finish` to record the head revision first, then run observed analysis on that revision, check collisions, offer human-approved check runs, and generate the packet.
  - Describe trailers, `conn evidence run`, and `/conn-review` in the optional steering.
  - Update the Kiro bundle templates and the bundle test. Requirements: 6, 8.

- [x] 10. Update documentation
  - Document `.conn/checks.json`, `conn evidence run`, trailers, the Git hook, and the review packet in `README.md` and `docs/architecture.md`.
  - Update `docs/code-structure.md` for `internal/review`. Requirements: 2, 3, 6, 7.

- [x] 11. Validate the milestone automatically
  - On `fixtures/pre-edit-two-sessions`, declare a change to `shouldRetryCharge`, change an undeclared export as well, run observed analysis and a configured check, and generate the packet.
  - Confirm drift, observed evidence, trailer presence, and stale evidence after a new commit, plus the checks-configuration flag when `.conn/checks.json` changes.
  - Run `pnpm check`, `pnpm test`, and `pnpm build`. Requirements: 10.

- [ ] 12. Validate with a real reviewer
  - Open a real pull request whose description is the generated packet.
  - Ask a reviewer who did not see the agent session to review it, and record the questions the packet left open and the information they had to look up elsewhere in `docs/real-trial.md`.
  - Do not mark this task complete from expected results alone. Requirements: 10.
