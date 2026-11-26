# 0001: Product model and human authority

- Status: accepted
- Owner: project owner

## Context

Conn needs one agreed plan per objective, explainable semantic history, and a binding authority boundary that an agent cannot cross.

Alternatives included parallel active Blueprints, mutable or globally canonical Claims, binding MCP form input, and CLI/TUI-only review. Those options either made the accepted plan ambiguous, weakened history, or provided weaker direct-human provenance.

## Decision

- A Project is the authorization and event boundary with `owner` and `member` roles.
- An Objective has at most one Blueprint identity. Planning changes append Blueprint revisions.
- A Blueprint revision activates only after every assigned Human accepts their own immutable Charter snapshot. A pending revision never silently replaces the active one.
- Every declared or observed Semantic Claim is immutable and belongs to one exact Intent revision. Radar may group equal normalized subjects without creating a canonical mutable Claim.
- Agents may manage participant-owned Intents, permitted evidence, and non-binding proposals. They cannot approve Objectives or Blueprints, decide Charters for Humans, change membership, accept risk, or record binding Resolution Decisions.
- Future binding actions use an authenticated Conn browser review page. URL elicitation may open it, but the browser independently authenticates the Human and submits directly to the backend.
- Review challenges are short-lived, single-use, bound to an exact record revision, and contain no credential or pre-authenticated access. Cancellation, expiry, stale revisions, replay, headless clients, and unsupported clients fail closed.
- `conn app` and direct CLI review remain the human-authoritative fallback until browser review is implemented.

## Consequences

Conn keeps one unambiguous accepted partition per Objective and can explain historical collisions from exact Claims. Browser review adds future implementation cost but separates model requests from Human decisions and durable audit provenance.

## Validation

Tests protect one-Blueprint enforcement, Charter authority, immutable Claim references, stale-revision rejection, and absence of binding MCP inputs. The future browser feature must prove direct Human authentication, exact record display, one-time completion, cancellation, replay safety, and fail-closed headless behavior.
