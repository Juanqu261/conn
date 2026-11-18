# Conn

Conn is a coordination protocol and evidence layer for human-agent software teams. It records a software objective, an immutable blueprint that divides it into accountable work cells, and each assigned human's decision on their charter.

Conn does not launch or supervise agents. Humans approve objectives and accept their own charters. Agents can read the accepted context through Kiro, but cannot write, approve, or change an accepted boundary.

## MVP

The first slice supports one project with an objective, versioned blueprints, and cell charters. It is designed for a three-person, three-Kiro-session experiment, although a blueprint may contain one to 32 cell charters.

It currently provides:

- GitHub Device Flow sign-in for pilot humans, with a 30-day Conn session stored in the operating-system credential store.
- Owner-created projects and explicit member management.
- Immutable objective revisions with a Markdown overview and optional structured key points.
- Immutable blueprint revisions with complete cell-charter snapshots, responsibilities, exclusions, deliverables, evidence obligations, and cross-cell dependencies.
- Individual, final charter acceptance or rejection. A blueprint becomes active only after every assigned human accepts their own charter. A newer proposed revision does not replace the active one until it is accepted.
- Project status and cursor-based event sync.
- A read-only Kiro MCP adapter exposing `read_objective`, `read_blueprint`, `read_my_charter`, and `check_updates`.

The MVP does not include agent-initiated writes, a web application, a local offline outbox, background update polling, or coding-agent adapters other than Kiro.

# Run Conn locally

## Kiro MCP adapter

Run `conn mcp` to expose the read-only Kiro adapter over standard input and output:

```powershell
$env:CONN_PROJECT = '<project-uuid>'
go run ./cmd/conn mcp
```

The adapter acts as the person authenticated by `conn login`, or uses the non-persistent `CONN_TOKEN` override when explicitly set. Standard output is reserved for MCP messages; diagnostics go to standard error. Conn does not provide a tool that writes, accepts, or rejects on a human's behalf.

## Development and test

`pnpm check` runs Biome, TypeScript checking, Vitest, Go format validation, `go vet`, and `go test`. See [architecture](docs/architecture.md), [code structure](docs/code-structure.md), [the real-trial guide](docs/real-trial.md), and [the accepted decisions](docs/decisions/README.md).
