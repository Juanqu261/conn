# Requirements

## Purpose

This future feature adds an authenticated Conn web review page for binding human actions. It is a focused authority boundary within the planned web application, not part of the current Kiro-native participant work loop.

## Requirements

### 1. Direct human outcome

- A model, MCP tool argument, agent prompt, hook, or Kiro permission decision must not be able to provide the binding outcome.
- The authenticated Human must submit the outcome directly through the Conn review page or an explicitly documented direct-human fallback.
- URL elicitation acceptance must mean only consent to open the page.

### 2. Exact review subject

- The page must show the Project, action, immutable entity ID, exact revision, relevant context or diff, consequences, and reversibility before submission.
- The backend must reject stale revisions and require a new review challenge showing current state.
- The initial production slice must support one narrow binding action before the mechanism is generalized.

### 3. Authentication and authorization

- The browser must authenticate the Conn Human independently from the MCP client.
- The backend must authorize the authenticated Human for the exact action at commit time.
- Review URLs must contain no credential, personal information, protected record content, or pre-authenticated access.

### 4. Audit provenance

- A committed decision must record the fields required by decision 0001, including deciding Human, initiating identity, exact record revision, review challenge, outcome, consequences version, and timestamp.
- Initiator, agent-authored proposal, and deciding Human must remain distinct.
- Conn must not retain source, prompts, transcripts, or browser credentials as decision provenance.

### 5. Safe lifecycle

- Challenges must be high entropy, short-lived, single use, and bound server-side to the user and exact action.
- Decline, cancellation, dismissal, browser close, expiry, replay, authorization failure, and stale revision must not create an unintended decision.
- A committed browser decision must survive MCP disconnect; an incomplete challenge must expire safely.

### 6. Kiro and fallback behavior

- Supported interactive Kiro clients may open the review page through URL elicitation.
- Tool permission may gate requesting review but must not count as approval.
- Unsupported and headless clients must fail closed and provide a browser review queue, `conn app`, or documented direct CLI fallback.
- Existing fallback surfaces must remain usable during rollout and rollback.

### 7. Architecture gate

- Before browser-facing implementation, the web-application feature must select and record its UI system, browser authentication/session design, and deployment boundary.
- No production binding-action MCP tool or web UI may be scaffolded until those decisions and this specification are approved.
