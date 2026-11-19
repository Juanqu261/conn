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
- Project status, cursor-based event sync, and read-only Kiro MCP tools.
- A keyboard-first `conn app` terminal application with project selection, reviewed in-app project, member, objective, blueprint, and charter-decision flows, plus Overview, Plan, Team, Updates, and Account sections.

The MVP does not include agent-initiated writes, a web application, a local offline outbox, background update polling, or coding-agent adapters other than Kiro.

# Run Conn locally

## Interactive app

Start the Conn application from an interactive terminal:

```powershell
go run ./cmd/conn app
```

It lets you choose an existing project, including reopening the project picker with `p` when only one project is available, or create a project directly from the terminal without a JSON file. Owners can add an already authenticated member by pasting that person's Conn human UUID, create an objective by entering its title and Markdown overview and selecting an accountable project member, and propose a blueprint by selecting a current objective and building one or more accountable cells. Every form keeps its draft only in memory, shows a read-only review, and needs Enter confirmation before it writes.

An assigned person can press `d` in Plan to inspect and accept or reject only their own pending charter. Rejection requires a rationale and both outcomes need an explicit confirmation. `Esc` cancels or goes back without writing. The existing CLI commands remain available for scripts and automation.

## Kiro MCP adapter

Run `conn mcp` to expose the read-only Kiro adapter over standard input and output:

```powershell
$env:CONN_PROJECT = '<project-uuid>'
go run ./cmd/conn mcp
```

The adapter acts as the person authenticated by `conn login`, or uses the non-persistent `CONN_TOKEN` override when explicitly set. Standard output is reserved for MCP messages; diagnostics go to standard error. Conn does not provide a tool that writes, accepts, or rejects on a human's behalf.

## Development and test

`pnpm check` runs Biome, TypeScript checking, Vitest, Go format validation, `go vet`, and `go test`. See [architecture](docs/architecture.md), [code structure](docs/code-structure.md), [the real-trial guide](docs/real-trial.md), and [the accepted decisions](docs/decisions/README.md).
