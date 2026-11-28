# Spec Kit fixture

A repository planned with GitHub Spec Kit. `internal/speckit` tests every extraction rule against it, and `cmd/conn/speckit_test.go` copies it into two Git worktrees to show a plan-time collision.

- `.specify/memory/constitution.md` has two real principles, one template placeholder, and a version line.
- `specs/001-checkout-retries/` has metadata lines, an `**Input**` description, functional requirements with a repeated ID and a placeholder, success criteria, two clarification markers, and a task list that covers each path rule: plain and backtick paths, a new file, a missing parent directory, Windows separators, prose such as `e.g.` and `and/or`, a URL, traversal, an absolute path, and a line without a checkbox. Its `plan.md` is ignored because `tasks.md` exists.
- `specs/002-order-limits/` has a placeholder title and a task that names `shouldRetryCharge` in `src/payments/retry.ts`, the same exported function feature 001 changes.
- `specs/003-refund-audit/` has no title and no `tasks.md`, so its paths come from the source-structure block of `plan.md`, not the documentation tree.

The TypeScript sources come from `fixtures/pre-edit-two-sessions`. The private literal in `src/payments/retry.ts` must never appear in a report or a stored record.
