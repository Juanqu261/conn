# Tasks

- [x] 1. Review and approve the Spec Kit coordination scope
  - Confirm the acceptance goal: plan-time collisions and Conn in every Spec Kit agent, without Conn editing Spec Kit state.
  - Confirm that extraction is deterministic Go code and that agents only present and ask.
  - Confirm that FR requirements become acceptance criteria keyed `fr-nnn`, which `.conn/checks.json` can name.
  - Do not begin implementation until the specification and decision gate are approved. Requirements: 1, 3, 4, 7.

- [x] 2. Record decision 0010
  - Add `docs/decisions/0010-speckit-coordination.md`: Spec Kit as a source and integration target, not a dependency; the selected extension distribution; deterministic extraction; the runtime-event condition.
  - Update `docs/decisions/README.md`, `.kiro/steering/tech.md`, and the delivery order in `.kiro/steering/product.md`. Requirements: 4, 5, 7.

- [x] 3. Spike the unconfirmed Spec Kit behavior
  - In a scratch repository with Spec Kit 1.4.x and `--integration kiro-cli`, plus one other agent integration, confirm: the local-directory extension install command; whether a failing hook stops the core command; how the current feature is recorded; the constitution path; and the `pre_tool_use` and `post_tool_use` payload shapes the integration produces.
  - Record the findings in this design's "Spec Kit facts" section and stop for review if any finding changes a requirement. Requirements: 4, 5.

- [x] 4. Define the contracts
  - Add `speckit_feature` to `objective-import-input` and `objective`, with the migration that widens the source-kind constraint.
  - Add the `speckit-check` schema, accepted and rejected fixtures, and Go types. Requirements: 1, 3.

- [x] 5. Implement the Spec Kit readers
  - Add `internal/speckit` with `KeyPoints` and `PlannedPaths`, bounded reads, symlink and traversal refusal.
  - Add `fixtures/speckit/` with a feature directory and constitution covering every rule, and one parameterized test. Requirements: 1, 2, 7.

- [x] 6. Extend `conn start`
  - Add `workstart.SpecKitInput` and Spec Kit detection to `conn start`, with extracted paths shown for confirmation.
  - Reuse the existing reuse and new-revision logic. Requirements: 1, 2, 6.

- [x] 7. Implement `conn speckit check`
  - Feature resolution, Objective lookup without import, extraction, claim suggestion, collision preview, own-Intent exclusion and claim delta, text and JSON output, exit 0 always.
  - Add the two-feature Go scenario from the validation plan. Requirements: 3, 8.

- [x] 8. Implement the extension package
  - Add `internal/speckitext` with the embedded `extension.yml` and commands rendered from `internal/agentguide`.
  - Add `conn speckit extension export` with overwrite refusal, and the byte-stability test. Requirements: 4, 6.

- [x] 9. Add agent lifecycle events, if task 3 confirmed them
  - Add `conn hook --client`, one adapter per confirmed agent, the hook scripts, and `events` in `extension.yml`.
  - Extend the existing hook test with each adapter's payload. Otherwise record the gap and skip. Requirements: 5.
  - Skipped: the spike found no payload samples to confirm parsing against, so the extension ships without `events`; the gap is recorded in `docs/real-trial.md`.

- [x] 10. Update documentation
  - README section "Spec Kit", `AGENTS.md` guidance, and `conn doctor` output for a detected Spec Kit installation. Requirements: 4, 6.

- [ ] 11. Run the real trial
  - One participant in Kiro, one with Spec Kit and another agent, same repository, overlapping task lists.
  - Record the plan-time warning, the `before_implement` warning, the review packet with `fr-` acceptance keys, and any friction in `docs/real-trial.md`. Requirements: 8.
