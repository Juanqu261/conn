# Tasks

- [ ] 1. Review and approve the product-direction amendment
  - Confirm that discussion attached to a record passes the feature test and that channels, DMs, presence, reactions as votes, task assignment, and per-person counts stay out.
  - Confirm outbound-only Slack and Discord bridges and human-only posting.
  - Do not begin implementation until `web-lab-board` task 1 and this specification are approved. Requirements: 1, 6, 7.

- [ ] 2. Record decision 0014 and amend the steering
  - Add `docs/decisions/0014-record-attached-discussion.md` with the scope boundary, no-authority rule, bridge direction, secret storage and key rotation, and the in-process delivery worker.
  - Amend the feature-test sentence in `.kiro/steering/product.md`, and update `.kiro/steering/tech.md` and `docs/decisions/README.md`. Requirements: 1.

- [ ] 3. Define the contracts and migration
  - Add the thread, comment, version, notification, preference, bridge, and delivery tables.
  - Add the `thread-view`, `comment-input`, `comment-edit-input`, `notification-list`, `bridge`, and `bridge-input` schemas and the new event types in `event-envelope`, with fixtures and Go types. Requirements: 2, 5, 6.

- [ ] 4. Implement threads and the timeline
  - Comment post, reply, edit with versions, delete tombstone, owner hide and unhide, credential-pattern warning, timeline assembly with paging.
  - Add the authority and lifecycle tests. Requirements: 2, 3.

- [ ] 5. Implement mentions and references
  - Token storage, server-side validation, "Mentioned in" lookup.
  - Add the validation test. Requirements: 4.

- [ ] 6. Implement notifications
  - `internal/notify` recipient mapping in each qualifying write transaction, preferences and mutes, private stream messages, read state.
  - Add the recipient test. Requirements: 5.

- [ ] 7. Implement bridges
  - `CONN_SECRET_KEY` encryption with key IDs, URL validation, enqueue in the event transaction, the delivery worker with backoff, `Retry-After`, pause after five failures, test message, owner management events.
  - Add the URL-validation and delivery tests. Requirements: 6.

- [ ] 8. Build the Séance and Bell tower in the Lab
  - Thread panel on the Specimen, Stitch, Objective, and Blueprint pages; composer with `@` and `#` autocomplete; sanitized rendering; version history; owner hide.
  - Bell tower view, rail badge, preferences, mute; bridge settings for owners; comment export on Account.
  - Add the sanitizer and token-rendering Vitest checks. Requirements: 2, 3, 4, 5, 6, 8.

- [ ] 9. Give agents read access
  - Add the `read_thread` MCP tool, the work-context discussion summary, and the agent-guide rule rendered to steering, power skills, `AGENTS.md`, and the Spec Kit extension; regenerate the committed power and extension.
  - Add the bounds and labelling test. Requirements: 7.

- [ ] 10. Update documentation
  - README "Séance and bridges" section, including Slack and Discord webhook setup, `CONN_SECRET_KEY` generation and rotation, and what bridged messages contain. Requirements: 6, 8.

- [ ] 11. Run the real trial
  - Two Kiro participants collide; the counterpart owners discuss it in the Lab; a third participant's agent runs `/conn-resolve` and its proposal cites the thread; a human decides; Slack and Discord bridges each receive the opened and decided messages with working links.
  - Break a bridge URL to confirm retry and pause, and record findings in `docs/real-trial.md`. Requirements: 2, 5, 6, 7.
