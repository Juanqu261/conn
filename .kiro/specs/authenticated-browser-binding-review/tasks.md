# Tasks

This task list belongs to the future web-application feature. None of these implementation tasks are part of the Kiro-native work-loop specification.

- [x] 1. Select and record the web application architecture
  - Compare practical UI system, browser session, deployment, and accessibility options against the accepted Go and TypeScript boundaries.
  - Obtain approval and record the decisions before scaffolding browser code.
  - Done by [decision 0013](../../../docs/decisions/0013-web-application.md) with `web-lab-foundation`.

- [ ] 2. Approve the first binding action
  - Confirm Charter acceptance or rejection as the first narrow production slice, or select another action with equivalent immutable inputs and consequences.
  - Finalize the exact review contract and audit fields.

- [ ] 3. Implement expiring review challenges
  - Add versioned contracts, backend persistence, authorization, expected-revision checks, expiry, cancellation, replay safety, and event provenance.
  - Keep challenge state distinct from the append-only domain decision.

- [ ] 4. Implement authenticated browser review
  - Require independent Conn authentication.
  - Render the exact immutable record, consequences, stale state, and direct approve, reject, and cancel controls.
  - Add CSRF and browser-session protections selected by the architecture decision.

- [ ] 5. Add URL-elicitation integration
  - Add a review-request operation with no outcome or deciding-Human argument.
  - Treat URL acceptance only as consent to open the page and return authoritative challenge status after completion.
  - Fail closed for unsupported and headless clients.

- [ ] 6. Preserve direct-human fallbacks
  - Keep `conn app` and the documented direct CLI review available during rollout and rollback.
  - Ensure no Kiro permission or auto-approval path can count as the domain decision.

- [ ] 7. Validate the authority boundary
  - Prove direct attribution, exact revision display, one-time completion, stale rejection, cancellation, expiry, replay safety, unsupported-client behavior, and absence of source or credential leakage.
  - Run one real interactive Kiro-to-browser trial before enabling additional binding actions.
