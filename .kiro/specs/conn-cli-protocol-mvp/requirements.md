# Requirements

## 1. GitHub-authenticated human identity

**User story:** As a participant, I want to authenticate through GitHub so that Conn can attribute authoritative actions to me rather than to an agent session.

### Acceptance criteria

1. WHEN a human runs `conn login`
   THE SYSTEM SHALL initiate GitHub device authorization without requiring the human to enter a GitHub password into Conn
2. WHEN GitHub confirms authorization
   THE SYSTEM SHALL associate the immutable GitHub user identifier with one Conn human UUIDv7 identity
3. WHEN authentication succeeds
   THE SYSTEM SHALL issue one opaque Conn session token and store only its SHA-256 hash on the backend
4. WHEN a session is older than 30 days or has been revoked
   THE SYSTEM SHALL reject it as unauthenticated
5. WHEN a human runs `conn logout`
   THE SYSTEM SHALL revoke the backend session and delete the local credential
6. IF an operating-system credential store is unavailable
   THE SYSTEM SHALL return an actionable error and SHALL NOT create a plaintext credential file
7. WHEN `CONN_TOKEN` is supplied explicitly
   THE SYSTEM SHALL use it without persisting it

## 2. Explicit project authorization

**User story:** As a project owner, I want to control who participates in a Conn project so that blueprint information is not visible to unrelated authenticated users.

### Acceptance criteria

1. WHEN an authenticated human creates a Project
   THE SYSTEM SHALL make that human the Project owner
2. WHEN an owner adds an existing Conn human UUID as a member
   THE SYSTEM SHALL grant that human project read access
3. WHEN a member attempts an owner-only operation
   THE SYSTEM SHALL reject the request
4. WHEN a non-member requests a project resource
   THE SYSTEM SHALL respond without revealing whether the project exists
5. WHEN any role attempts to accept another human's charter
   THE SYSTEM SHALL reject the request

## 3. Narrative Objective with keyed points

**User story:** As a project owner, I want to capture the overall objective in natural language while identifying only the factors that need stable references.

### Acceptance criteria

1. WHEN an owner creates an Objective from JSON
   THE SYSTEM SHALL require a title, Markdown overview, accountable human, and Project
2. WHEN the Objective includes key points
   THE SYSTEM SHALL accept only `outcome`, `acceptance_criterion`, `invariant`, `constraint`, and `question` kinds
3. WHEN an Objective contains duplicate key-point keys
   THE SYSTEM SHALL reject it
4. WHEN an Objective omits key points
   THE SYSTEM SHALL still accept the narrative Objective
5. WHEN an Objective changes after creation
   THE SYSTEM SHALL create a new immutable revision rather than overwrite the previous revision

## 4. Blueprint and work-cell partition

**User story:** As an objective owner, I want to propose one complete division of work so that every participant reviews the same system partition.

### Acceptance criteria

1. WHEN an owner proposes a BlueprintRevision
   THE SYSTEM SHALL reference one exact Objective revision
2. WHEN a BlueprintRevision is stored
   THE SYSTEM SHALL preserve a complete immutable snapshot of every included CellCharter
3. WHEN a CellCharter is provided
   THE SYSTEM SHALL require a stable cell UUIDv7, accountable human, title, and Markdown overview
4. WHEN a charter includes boundary points
   THE SYSTEM SHALL accept only `responsibility`, `exclusion`, `deliverable`, and `evidence_obligation` kinds
5. WHEN a charter references Objective key points
   THE SYSTEM SHALL reject references absent from the selected Objective revision
6. WHEN a charter depends on another cell
   THE SYSTEM SHALL reject a target cell absent from the same BlueprintRevision
7. WHEN any charter boundary changes
   THE SYSTEM SHALL require a new BlueprintRevision

## 5. Charter ratification

**User story:** As a cell owner, I want to accept or reject my own charter so that accountability cannot be assigned to me without consent.

### Acceptance criteria

1. WHEN a BlueprintRevision is proposed
   THE SYSTEM SHALL keep it `proposed` until every accountable human accepts their own charter
2. WHEN a human accepts their own unchanged charter more than once
   THE SYSTEM SHALL treat the operation idempotently
3. WHEN the final required acceptance is recorded
   THE SYSTEM SHALL activate the BlueprintRevision in the same transaction
4. WHEN a human rejects their charter
   THE SYSTEM SHALL require a rationale and keep the BlueprintRevision inactive
5. WHEN a rejected charter is revised
   THE SYSTEM SHALL create a new BlueprintRevision rather than overwrite the rejected revision
6. WHEN work refers to a non-active blueprint during the observation MVP
   THE SYSTEM SHALL report it as unapproved rather than blocking local code edits

## 6. CLI-first human workflow

**User story:** As a participant, I want to perform authoritative actions through the CLI so that the protocol can be validated before a web application is built.

### Acceptance criteria

1. WHEN a human runs a structured CLI command with `--json`
   THE SYSTEM SHALL emit a machine-readable result on standard output
2. WHEN a command fails
   THE SYSTEM SHALL emit human diagnostics on standard error and return a stable nonzero exit category
3. WHEN a command accepts an Objective or Blueprint file
   THE SYSTEM SHALL validate it locally before upload and the backend SHALL validate it again
4. WHEN CLI help is displayed
   THE SYSTEM SHALL contain no command for administering Conn's PostgreSQL schema or a customer's migrations
5. WHEN a secret is used
   THE SYSTEM SHALL NOT include it in flags, output, logs, errors, or completion data

## 7. Read-only Kiro MCP access

**User story:** As a work cell using Kiro, I want to read the accepted shared blueprint and my charter so that my agent works from the team's ratified context.

### Acceptance criteria

1. WHEN Kiro starts `conn mcp`
   THE SYSTEM SHALL communicate over standard input and output without opening a local network port
2. WHEN MCP initialization completes
   THE SYSTEM SHALL expose only read-objective, read-blueprint, read-my-charter, and check-updates capabilities in the first slice
3. WHEN a Kiro session requests its charter
   THE SYSTEM SHALL derive the human from the authenticated Conn session and SHALL NOT accept another human identifier as authority
4. WHEN `conn mcp` emits diagnostics
   THE SYSTEM SHALL write them to standard error and preserve standard output for MCP messages
5. WHEN the MCP process exits
   THE SYSTEM SHALL close cleanly without requiring a persistent sidecar

## 8. Event cursor synchronization

**User story:** As a participant, I want to retrieve project changes since my last observation so that independent Kiro sessions can converge on current team state.

### Acceptance criteria

1. WHEN an authoritative project mutation commits
   THE SYSTEM SHALL append its event atomically with the state change
2. WHEN a client supplies its last event sequence
   THE SYSTEM SHALL return committed events after that cursor in ascending order
3. WHEN a client repeats the same cursor request
   THE SYSTEM SHALL return the same retained event range without creating duplicates
4. WHEN state refresh fails
   THE SYSTEM SHALL NOT present cached state as current
5. WHILE no Conn operation is occurring
   THE SYSTEM SHALL NOT run a background polling timer in the first slice

## 9. Persistence and migration boundaries

**User story:** As an operator, I want Conn's authoritative records to survive restarts and schema upgrades without exposing database administration as a product capability.

### Acceptance criteria

1. WHEN Conn changes authoritative state and appends an event
   THE SYSTEM SHALL commit both or neither
2. WHEN the Conn database schema is incompatible
   THE SYSTEM SHALL refuse server readiness rather than operate against it
3. WHEN development or deployment applies Conn's internal migrations
   THE SYSTEM SHALL do so outside the public CLI and MCP command surfaces
4. WHEN normal development database commands run
   THE SYSTEM SHALL NOT delete the PostgreSQL named volume
5. WHEN Conn observes a migration in a customer repository
   THE SYSTEM SHALL NOT execute that migration

## 10. Minimal validation

**User story:** As a maintainer, I want a small test suite centered on fundamental guarantees so that test volume does not obscure product risk.

### Acceptance criteria

1. WHEN the first schemas are created
   THE SYSTEM SHALL validate the same accepted and rejected fixtures through Go and Node.js
2. WHEN persistence is implemented
   THE SYSTEM SHALL test atomic state-and-event commit behavior
3. WHEN authorization and ratification are implemented
   THE SYSTEM SHALL test the three-human activation path and forbidden cross-human acceptance
4. WHEN MCP is implemented
   THE SYSTEM SHALL test one subprocess initialization and read-only tool call
5. WHEN implementation details change without changing a public guarantee
   THE SYSTEM SHALL NOT require duplicate tests of those details
6. THE SYSTEM SHALL NOT enforce a coverage-percentage target during the MVP

## 11. Deferred product surfaces

**User story:** As the product owner, I want the first slice narrowly scoped so that protocol value is tested before adjacent products are built.

### Acceptance criteria

1. THE SYSTEM SHALL NOT include a web application implementation in this slice
2. THE SYSTEM SHALL NOT include Cursor, Claude Code, Codex, or other coding-agent adapters
3. THE SYSTEM SHALL NOT include boundary contracts, active change intents, collision detection, or evidence compilation in this slice
4. THE SYSTEM SHALL NOT include a persistent sidecar, local SQLite outbox, SSE, or WebSockets
5. THE SYSTEM SHALL NOT allow agents to accept charters or modify authoritative objectives and blueprints through MCP
