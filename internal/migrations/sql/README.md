# Conn schema migrations

Goose SQL migrations for Conn's own backend database. They are embedded into the build and applied only by development, CI, and deployment automation through `internal/cmd/conn-migrate`. No public `conn` command or MCP tool runs them.

## Conventions

- Name files `NNNNN_short_description.sql`, for example `00001_initial_schema.sql`, with sequential versions.
- Never edit a migration after it has been applied anywhere. Add a new one instead.
- Each file contains `-- +goose Up` and `-- +goose Down` sections. Down sections exist for development only; automation never runs them.
- Conn never executes migrations found in a customer repository.
