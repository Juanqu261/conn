# Tasks

- [ ] 1. Review and approve the board scope
  - Confirm the lane rules and their precedence, and that Intents never move by hand.
  - Confirm that collision decisions and Charter decisions are the Lab's first writes, with the same authority as `conn app`, and that agent-initiated browser review stays in `authenticated-browser-binding-review`.
  - Do not begin implementation until `web-lab-foundation` task 2 and this specification are approved. Requirements: 1, 3, 5, 9.

- [ ] 2. Amend decision 0013
  - Record the drag-and-drop and graph libraries with pinned versions, SVG-only charts, and that collision decisions ship alongside the Charter slice.
  - Update `.kiro/steering/tech.md`. Requirements: 5, 8, 10.

- [ ] 3. Define the contracts
  - Add the `board-view` schema, the optional expected-revision and expected-state fields on `resolution-decision-input`, and the decision-surface field in the decision event payload, each with accepted and rejected fixtures and Go types.
  - Expose the review packet over HTTP if it is not already. Requirements: 1, 5, 6.

- [ ] 4. Implement the board read model
  - Add `internal/board` with the pure lane function, reusing `internal/review` staleness, and `GET …/board`.
  - Add the parameterized lane test. Requirements: 1.

- [ ] 5. Implement decision preconditions
  - Compare expected revisions and state inside the decision transaction, return `409` with current state, and record the session kind as the decision surface.
  - Add the precondition test. Requirements: 5, 9.

- [ ] 6. Build the Intent board
  - Lanes, cards, swimlanes, URL filters, list rendering for small screens and screen readers, refusal-with-reason on drag, live lane moves, and the "It's alive!" effect with reduced-motion handling.
  - Add the Vitest checks for refusal and the once-per-revision effect. Requirements: 1, 2, 3, 4.

- [ ] 7. Build the collision board and the decision dialog
  - Columns, cards, drag and keyboard "Decide…", dialog with exact revisions, proposals, pair check, consequences, required rationale, stale handling.
  - Add the Vitest check for offered kinds and no preselection. Requirements: 5.

- [ ] 8. Build the Specimen and Stitch pages
  - Specimen header, tabs, acceptance checklist, claims and drift, evidence timeline, review packet with copy, revision selector, GitHub link.
  - Stitch page with side-by-side claims, proposals, history, pair checks, `conn verify-pair` command, and the shared dialog. Requirements: 6, 7.

- [ ] 9. Build the Stitch Map
  - Graph from radar with state styling, Objective clusters, subject layer, focus mode, keyboard traversal, table view, and the omission banner. Requirements: 8.

- [ ] 10. Build Blueprints and Pulse
  - Objective and Blueprint views with coverage; own-Charter Accept and Reject dialog against existing endpoints.
  - Pulse charts with window selector, insufficient-data states, and table alternatives. Requirements: 9, 10.

- [ ] 11. Update documentation
  - README "Conn Lab" sections for the board, decisions in the browser, and the Stitch Map; `docs/architecture.md` collision-lifecycle note on the decision surface. Requirements: 5, 9.

- [ ] 12. Run the real trial
  - Two Kiro participants create a contract collision; a third member watches it appear on both boards, decides it from the browser, and the Kiro sessions see the decision on their next prompt.
  - Race two browsers on the same decision to confirm the stale path, accept a Charter from the browser, and record findings in `docs/real-trial.md`. Requirements: 1, 4, 5, 9.
