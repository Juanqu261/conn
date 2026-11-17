# Conn executable

## Responsibility

Provide Conn's single executable (decisions 0010 and 0011). `main.go` constructs adapters and hands them to `internal/cli`; it holds no command or product behavior.

## Modes

- `conn server`: backend HTTP service from `internal/httpapi`. Only `GET /healthz` exists so far.
- `conn mcp`: Kiro MCP adapter from `internal/mcp` over stdio. Before the session starts, `os.Stdout` is redirected to standard error so stray output cannot corrupt the protocol stream.

## Test

`main_test.go` re-executes the test binary as `conn mcp`, completes MCP initialization over stdio, and requires a clean exit after the client disconnects.

## Deferred

- Human commands for authentication, projects, objectives, blueprints, charters, status, sync, and doctor
- Stable exit-code categories and `--json` output
- Release packaging
