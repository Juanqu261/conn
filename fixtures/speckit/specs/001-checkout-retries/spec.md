# Feature Specification: Checkout retries

**Feature Branch**: `001-checkout-retries`

**Created**: 2026-11-20

**Status**: Draft

**Input**: User description: "Retries must never charge a customer twice."

## User Scenarios & Testing _(mandatory)_

### User Story 1 - Retry a failed charge (Priority: P1)

A shopper whose charge failed on a transport error is retried at most three times with one idempotency key.

**Acceptance Scenarios**:

1. **Given** a transport failure, **When** checkout retries, **Then** the same idempotency key is reused.

### Edge Cases

- What happens when the provider times out after charging? [NEEDS CLARIFICATION: provider timeout semantics are unknown]

## Requirements _(mandatory)_

### Functional Requirements

- **FR-001**: System MUST reuse the idempotency key on every retry of one checkout attempt.
- **FR-002**: System MUST stop retrying after [NEEDS CLARIFICATION: retry limit not specified - three or five?] attempts.
- **FR-003**: [FUNCTIONAL_REQUIREMENT]
- **FR-001**: A repeated requirement ID keeps its first statement.

## Success Criteria _(mandatory)_

### Measurable Outcomes

- **SC-001**: No shopper is charged twice for one checkout attempt.
- **SC-002**: Retried checkouts complete within 10 seconds for 95% of attempts.
