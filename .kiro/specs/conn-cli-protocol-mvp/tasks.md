# Tasks

- [x] 1. Review and approve the MVP design
  - Confirm requirements, API boundaries, CLI commands, schema set, security constraints, and deferred work.
  - Do not begin implementation before the project owner approves this specification.

- [x] 2. Create canonical contracts and shared fixtures
  - Add Draft 2020-12 schemas for Project, ProjectMembership, Objective, BlueprintRevision, CharterDecision, and EventEnvelope.
  - Add a small accepted and rejected fixture corpus covering fundamental structural rules.
  - Validate the same fixtures through Ajv and the Go validator.
  - Stop for schema review before implementing persistent records.

- [x] 3. Select protocol model generation
  - Compare generation options against the accepted schemas and generated output quality.
  - Ask the project owner to approve a generation strategy.
  - Record the decision and generate only the models needed by this slice.

- [x] 4. Add approved Go dependencies and process modes
  - Add current pinned releases of Cobra, the official MCP Go SDK, zalando/go-keyring, pgx, and Goose.
  - Refactor `cmd/conn` into thin Cobra command and dependency-construction boundaries.
  - Add `conn server` and `conn mcp` startup modes without product behavior.
  - Preserve MCP standard-output isolation.

- [x] 5. Add the development PostgreSQL environment
  - Add PostgreSQL 18.7 Docker Compose configuration with loopback binding, named volume, and health check.
  - Add non-destructive root scripts for database start, stop, status, and internal migration execution.
  - Do not add a public Conn migration command or any volume-deletion command.

- [x] 6. Implement Conn's initial database schema
  - Add internal Goose SQL migrations for humans, sessions, projects, memberships, objective revisions, blueprint revisions, cell projections, charter decisions, project event counters, and events.
  - Add constraints for UUID identities, revisions, roles, statuses, ownership, and project-scoped event sequences.
  - Add server schema-compatibility readiness checking.

- [x] 7. Implement authentication and local credentials
  - Implement direct GitHub device flow with bounded polling and fake-server testability.
  - Implement backend GitHub identity exchange without retaining the GitHub token.
  - Implement opaque 30-day Conn sessions with hash-only persistence and revocation.
  - Implement OS keyring storage, `CONN_TOKEN` precedence, login, logout, whoami, and credential diagnostics.
  - Add only the fundamental authentication, redaction, and credential-precedence tests.

- [x] 8. Implement Project and Objective operations
  - Implement owner project creation and owner-managed membership by existing Conn human UUID.
  - Implement project authorization that does not reveal resources to non-members.
  - Implement narrative Objective creation and immutable revision reads.
  - Enforce key-point kinds and unique keys through schema and application validation.
  - Append state and project events atomically.

- [x] 9. Implement BlueprintRevision and charter ratification
  - Validate Objective point references, accountable project members, boundary-point kinds, and cell dependencies.
  - Store the immutable blueprint snapshot and query projections atomically.
  - Implement own-charter acceptance, rationale-required rejection, idempotency, and final activation.
  - Preserve rejected revisions and require a new revision for changes.
  - Add one three-human authorization and ratification scenario.

- [x] 10. Implement the versioned HTTP API
  - Add `net/http` handlers, authentication and project-authorization middleware, bounded JSON decoding, response envelopes, safe errors, health, and readiness.
  - Implement the approved `/v1` routes without exposing database or customer migration administration.
  - Add expected-revision conflict handling and event-cursor paging.
  - Test public behavior through the fundamental scenarios rather than one test per handler.

- [x] 11. Implement the CLI-first workflow
  - Add project, member, objective, blueprint, charter, status, sync, doctor, and authentication commands.
  - Validate JSON files locally and repeat validation on the backend.
  - Add human and `--json` output modes with stable exit categories.
  - Ensure secrets never enter command arguments, output, or completion.

- [x] 12. Implement read-only Kiro MCP
  - Register read-objective, read-blueprint, read-my-charter, and check-updates tools through the official Go SDK.
  - Use the same authenticated backend client as CLI reads.
  - Derive human authority from the Conn session rather than tool input.
  - Add one subprocess-level initialization, charter-read, cursor-update, and clean-exit scenario.

- [ ] 13. Validate the three-work-cell MVP
  - Run three distinct GitHub-authenticated humans through project membership, one Objective, one proposed BlueprintRevision, and three charter decisions.
  - Confirm the third acceptance activates the blueprint atomically.
  - Launch three independent Kiro sessions and confirm each reads the same active revision and only its own assigned charter through the convenience tool.
  - Run formatting, type checking, fundamental tests, builds, and frozen dependency installation.
  - Document observed friction and questions for post-MVP review without starting web application work.
