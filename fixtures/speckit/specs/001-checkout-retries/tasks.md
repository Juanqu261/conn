# Tasks: Checkout retries

**Input**: Design documents from `specs/001-checkout-retries/`

## Phase 1: Setup

- [ ] T001 Create project structure per implementation plan

## Phase 2: User Story 1 - Retry a failed charge (Priority: P1)

- [ ] T002 [P] [US1] Change the retry rule in src/payments/retry.ts
- [x] T003 [US1] Add `src/payments/retry-policy.ts` for the attempt limit.
- [ ] T004 [P] Document the behavior, e.g. retries and/or refunds, in docs/missing/guide.md
- [ ] T005 [US1] Call shouldRetryCharge from src/orders/checkout.ts, then recheck src\payments\retry.ts.
- [ ] T006 Read https://example.com/specs/retry.md and ../outside/retry.ts and /etc/retry.ts
- T007 Not a task line because it has no checkbox: src/orders/ignored.ts
