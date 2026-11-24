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
- Semantic coordination: Kiro specification imports with immutable provenance, repository connections, participant-owned change Intents with declared claims, local TypeScript analysis, deterministic advisory collision assessments, a portable conflict context pack, non-binding resolution proposals, human resolution decisions, reference-only evidence coverage, and the Radar read model.
- Kiro MCP tools that read shared state and write only the signed-in human's own Intents, evidence, and non-binding resolution proposals.
- A keyboard-first `conn app` terminal application with project selection, reviewed in-app project, member, objective, blueprint, and charter-decision flows, plus Overview, Plan, Radar, Work, Team, Updates, and Account sections.

Agents never make binding decisions. The MVP does not include a web application, a local offline outbox, background update polling, or coding-agent adapters other than Kiro.

# Run Conn locally

## Interactive app

Start the Conn application from an interactive terminal:

```powershell
go run ./cmd/conn app
```

It lets you choose an existing project, including reopening the project picker with `p` when only one project is available, or create a project directly from the terminal without a JSON file. Owners can add an already authenticated member by pasting that person's Conn human UUID, create an objective by entering its title and Markdown overview and selecting an accountable project member, and propose a blueprint by selecting a current objective and building one or more accountable cells. Every form keeps its draft only in memory, shows a read-only review, and needs Enter confirmation before it writes.

An assigned person can press `d` in Plan to inspect and accept or reject only their own pending charter. Rejection requires a rationale and both outcomes need an explicit confirmation. Owners can also press `k` in Plan to import a local Kiro specification as an objective, which hashes the file locally and never uploads it.

Work lists repository connections and Intents. Owners connect a repository with `g`. Anyone can publish an Intent with `n`, and `i` inspects Intents to revise (`e`), release (`x`), attach reference-only evidence (`v`), or run the local TypeScript analyzer (`z`) on the person's own CLI-participant Intents. Radar shows the coordination snapshot; `x` opens a collision, where `c` shows the conflict context pack to copy or save, `p` records a non-binding proposal, and `d` records a human decision. Binding decisions are owner-only. `Esc` cancels or goes back without writing. The existing CLI commands remain available for scripts and automation.

## Kiro MCP adapter

Run `conn mcp` to expose the Kiro adapter over standard input and output:

```powershell
$env:CONN_PROJECT = '<project-uuid>'
go run ./cmd/conn mcp
```

The adapter acts as the person authenticated by `conn login`, or uses the non-persistent `CONN_TOKEN` override when explicitly set. Standard output is reserved for MCP messages; diagnostics go to standard error. Agent tools may publish, revise, and release only the signed-in human's participant-owned Intents, attach reference-only evidence, and record non-binding resolution proposals. No tool accepts or rejects a charter, changes an objective, blueprint, or membership, or records a resolution decision.

## Development and test

`pnpm check` runs Biome, TypeScript checking, Vitest, Go format validation, `go vet`, and `go test`. See [architecture](docs/architecture.md), [code structure](docs/code-structure.md), [the real-trial guide](docs/real-trial.md), and [the accepted decisions](docs/decisions/README.md).
