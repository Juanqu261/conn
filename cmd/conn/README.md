# Conn executable

## Responsibility

Provide Conn's single executable. `main.go` constructs adapters and hands them to `internal/cli`; it holds no command or product behavior.

## Commands

- `conn login`, `logout`, `whoami`, `doctor`: account operations from `internal/account`. They target `--server`, then `CONN_SERVER_URL`, then `http://127.0.0.1:8080`.

## Modes

- `conn server`: backend HTTP service from `internal/httpapi`. It requires `CONN_DATABASE_URL` and serves `GET /healthz` and `GET /readyz`. It never changes the database schema.
- `conn mcp`: Kiro MCP adapter from `internal/mcp` over stdio. Before the session starts, `os.Stdout` is redirected to standard error so stray output cannot corrupt the protocol stream.

## Test

`main_test.go` re-executes the test binary as `conn mcp`, completes MCP initialization over stdio, and requires a clean exit after the client disconnects.

## Deferred

- Project, objective, blueprint, charter, status, and sync commands
- Stable exit-code categories and `--json` output
- Release packaging
