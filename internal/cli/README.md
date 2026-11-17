# CLI adapter

## Responsibility

Hold Conn's Cobra commands (decision 0028). Commands parse input, call an operation injected through `Dependencies`, and render the result. Domain, database, and HTTP behavior stay outside this package.

## Rules

- Diagnostics and errors go to standard error.
- `conn mcp` leaves standard output to MCP messages.
- No secret is ever accepted as a flag.
- No command administers Conn's database schema or a customer's migrations.
- Cobra's generated `completion` command is disabled until completion output is reviewed for secrets.
