# Conn schema migrations

Goose SQL migrations for Conn's own backend database. They are embedded into the build and applied only by development, CI, and deployment automation through `internal/cmd/conn-migrate`. No public `conn` command or MCP tool runs them.

## Conventions

- Name files `NNNNN_short_description.sql`, for example `00001_initial_schema.sql`, with sequential versions.
- Never edit a migration after it has been applied anywhere. Add a new one instead.
- Each file contains `-- +goose Up` and `-- +goose Down` sections. Down sections exist for development only; automation never runs them.
- Conn never executes migrations found in a customer repository.

## Schema guarantees

The migrations enforce these rules in the database as well as in the application:

- Entity identities are UUIDv7.
- Each project has exactly one owner, the owner holds the `owner` membership, and only an owner adds members.
- Objective and blueprint revisions are contiguous, starting at 1, and their `document` identity fields match the typed columns.
- Accountable humans, proposers, and charter deciders are project members.
- A charter decision is made only by that cell's accountable human, at most once per revision, and a rejection has a rationale.
- Objective revisions, cells, charter decisions, memberships, and events are append-only.
- A blueprint snapshot never changes and never contains `status`. Only a project owner proposes one.
- A blueprint has at most one `active` and one `proposed` revision. Status moves only from `proposed` to `active`, `rejected`, or `superseded`, and from `active` to `superseded`.
- Event sequences are gap-free within a project, and each event type fixes its entity type.
- Sessions expire at most 30 days after creation.

`internal/migrations/schema_test.go` exercises each rule.
