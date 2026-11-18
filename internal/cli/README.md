# CLI adapter

## Responsibility

Hold Conn's Cobra commands (decision 0028). Commands parse input, call an operation injected through `Dependencies` or the shared backend client, and render the result. Domain, database, and HTTP behavior stay outside this package.

## Commands

- Account: `login`, `logout`, `whoami`, `doctor`.
- Project workflow: `project create`, `project member add`, `objective create|revise|show`, `blueprint propose|revise|show`, `charter accept|reject`, `status`, `sync`.
- Process modes: `server`, `mcp`.

## Rules

- Results go to standard output; diagnostics and errors go to standard error.
- Structured commands accept `--json` and then print the API envelope `{"data": ..., "as_of_event": n}`.
- `--file` reads one JSON document of at most 1 MiB, or standard input when given `-`. It is validated against its schema before upload, and the server validates it again.
- Exit codes are a stable contract, listed in `conn --help` and `exit.go`: 0 success, 1 failure, 2 usage or invalid input, 3 authentication, 4 not found or not permitted, 5 conflict, 6 server unavailable, 130 interrupted.
- Untrusted text from the server is printed with control characters replaced, so it cannot drive the terminal.
- Status and blueprint output label every non-active revision as unapproved.
- `conn mcp` leaves standard output to MCP messages.
- No secret is ever accepted as a flag or printed.
- No command administers Conn's database schema or a customer's migrations.
- Cobra's generated `completion` command is disabled until completion output is reviewed for secrets.

## Test

`TestCLI` runs commands through `Execute` against the real HTTP handler and a fresh PostgreSQL database. It checks JSON output, stream separation, exit categories, local validation, terminal-safe output, help content, and that no token reaches the output.
