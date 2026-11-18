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

## Run Conn locally

### Prerequisites

- Git
- Docker Desktop or Docker Engine with Compose
- Go 1.27.4
- Node.js 26.13.1 LTS
- pnpm 12.21.0
- A GitHub OAuth application with Device Flow enabled, if you want to use `conn login`

### Set up the repository

Install the pinned JavaScript dependencies and create your local configuration file:

```powershell
pnpm install --frozen-lockfile
Copy-Item .env.example .env
```

Set `CONN_GITHUB_CLIENT_ID` in `.env` to the public client ID of the GitHub OAuth application. The server starts without this value, but GitHub sign-in is unavailable. The remaining values target the local PostgreSQL container and local server by default.

`.env` is ignored by Git. Do not put `CONN_TOKEN` in it. That variable is only an explicit, non-persistent override for a single shell or CI job.

### Start the database and server

In the repository root, start the development database and apply Conn's internal migrations:

```powershell
pnpm db:start
pnpm db:migrate
```

Start the backend in a separate terminal:

```powershell
go run ./cmd/conn server
```

The server listens on `http://127.0.0.1:8080` by default. It never applies migrations itself. Use `pnpm db:status` to inspect the local database and `pnpm db:stop` when you are finished. Those commands do not delete the named PostgreSQL volume.

### Sign in

Every human who will own a project or accept a charter must sign in once. Run the following in another terminal, complete the displayed GitHub Device Flow, then confirm the local connection:

```powershell
go run ./cmd/conn login
go run ./cmd/conn whoami
go run ./cmd/conn doctor
```

`whoami` prints the immutable Conn human ID. A project owner needs this ID from every person they add as a project member. `logout` revokes the stored session and removes it from the local credential store.

## First workflow

All structured write commands take `--file <path>` with a JSON document, or `--file -` for standard input. The schemas in [`contracts/v1`](contracts/v1/) define the authoritative input shapes. Use a new UUIDv7 for every `project_id`, `objective_id`, `blueprint_id`, and `cell_id`.

1. The future project owner signs in and creates a project from a [`project-create`](contracts/v1/project-create.schema.json) JSON document:

   ```powershell
   go run ./cmd/conn project create --file project.json --json
   ```

2. Each participant signs in and gives their `conn whoami` human ID to the owner. The owner adds each person to the project:

   ```powershell
   go run ./cmd/conn project member add --project <project-uuid> --human <human-uuid> --json
   ```

3. The owner creates an objective from an [`objective-revision-input`](contracts/v1/objective-revision-input.schema.json) JSON document:

   ```powershell
   go run ./cmd/conn objective create --project <project-uuid> --file objective.json --json
   ```

4. The owner proposes a blueprint from a [`blueprint-proposal`](contracts/v1/blueprint-proposal.schema.json) JSON document. The document includes a complete charter for every assigned work cell:

   ```powershell
   go run ./cmd/conn blueprint propose --project <project-uuid> --file blueprint.json --json
   ```

5. Each assigned human reads the proposed blueprint, then accepts only their own charter. A rejection is final for that blueprint revision, so the owner must propose a new revision to change it:

   ```powershell
   go run ./cmd/conn blueprint show --project <project-uuid> --blueprint <blueprint-uuid> --revision 1 --json
   go run ./cmd/conn charter accept --project <project-uuid> --blueprint <blueprint-uuid> --revision 1 --cell <cell-uuid> --json
   ```

6. Inspect team state and fetch events after a known cursor:

   ```powershell
   go run ./cmd/conn status --project <project-uuid> --json
   go run ./cmd/conn sync --project <project-uuid> --after 0 --json
   ```

The CLI prints structured successful results as `{"data": ..., "as_of_event": n}` when `--json` is set. It uses exit code `5` for stale revisions and other conflicts, allowing callers to read the current state and retry deliberately.

## Kiro MCP adapter

Run `conn mcp` to expose the read-only Kiro adapter over standard input and output:

```powershell
$env:CONN_PROJECT = '<project-uuid>'
go run ./cmd/conn mcp
```

The adapter acts as the person authenticated by `conn login`, or uses the non-persistent `CONN_TOKEN` override when explicitly set. Standard output is reserved for MCP messages; diagnostics go to standard error. Conn does not provide a tool that writes, accepts, or rejects on a human's behalf.

## Development

```powershell
pnpm format
pnpm check
pnpm test
pnpm build
```

`pnpm check` runs Biome, TypeScript checking, Vitest, Go format validation, `go vet`, and `go test`. See [CONTRIBUTING.md](CONTRIBUTING.md) for contribution expectations, [contracts/README.md](contracts/README.md) for protocol rules, and [the accepted decisions](docs/decisions/README.md) for the implementation direction.
