# Design

## Status and scope

This design records the transport and authority boundary accepted in decision 0001. Production implementation is deferred to the planned web-application feature.

The first implementation should choose one narrow action, preferably Charter acceptance or rejection, because the immutable Charter snapshot, accountable Human, decision consequence, and activation effect are already explicit. Objective or Blueprint approval, membership changes, accepted risk, and binding ResolutionDecisions remain deferred until the first action validates the mechanism.

## System shape

```text
Kiro agent
  -> request-review operation (no outcome argument)
  -> Conn backend creates expiring challenge
  -> URL elicitation displays HTTPS review URL
Human browser
  -> independent Conn authentication
  -> exact immutable record and consequences
  -> direct approve, reject, or cancel
Conn backend
  -> authorization and expected-revision check
  -> append-only decision and audit provenance
Kiro agent
  <- authoritative result or pending/cancelled/stale status
```

The browser submits the decision directly to the backend. The MCP client never carries the decision value. URL elicitation `accept` only resumes status checking.

## Challenge model

A review challenge is ephemeral workflow state, not the domain decision. It contains:

- random challenge UUID or equivalent high-entropy identifier;
- Project and action kind;
- exact entity ID and revision;
- immutable record digest or reference;
- initiating Human and Participant when present;
- required deciding Human or authorization rule;
- consequence-statement version;
- created and absolute expiry timestamps;
- pending, completed, cancelled, expired, or stale status;
- resulting decision reference when completed.

A challenge identifier locates the review but grants no access. The browser must authenticate independently. Completion and domain decision insertion occur atomically with authorization and expected-revision checks.

## Browser page

The page must present, without relying on model prose:

- Conn and Project identity;
- exact action and immutable revision;
- complete record fields needed for an informed decision;
- relevant prior decision or proposal references;
- consequences, including activation, reassignment, risk, or irreversibility;
- stale-state warning;
- explicit approve or reject controls and safe cancellation.

The future web-app specification must select the UI system and accessibility behavior before implementation. This design does not select a framework.

## Kiro adapter

A future request-review MCP operation may identify the action and immutable record, but it accepts no decision, deciding Human, accepted-risk, or binding rationale field. It returns URL-mode `InputRequests` only when the client advertises support.

After browser interaction, the adapter reads challenge status. It does not translate URL acceptance into a decision. Headless or unsupported clients receive an actionable fail-closed response naming the direct-human fallback.

## Audit and security

The committed decision follows decision 0001's provenance requirements. Challenge and decision records distinguish initiation from decision authority.

The URL uses HTTPS outside loopback development and contains no Conn token, pre-authenticated browser session, PII, or protected record data. The implementation must include CSRF protection, secure cookies or the accepted alternative session mechanism, bounded retry, expiry, replay protection, and safe cancellation.

## Validation boundary

Fundamental tests should prove the public guarantees rather than page internals:

- model/tool input cannot contain the outcome;
- exact revision and consequence data reaches the browser view;
- authenticated direct submission creates one attributed decision;
- stale, expired, replayed, cancelled, unauthorized, headless, and unsupported cases create no unintended decision;
- tool auto-approval does not bypass review;
- CLI and terminal fallbacks remain available.
