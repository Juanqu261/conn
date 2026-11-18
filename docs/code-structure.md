# Code structure

Conn is a Go-first monorepo with language-neutral JSON Schema contracts and two small Node.js/TypeScript workspaces. The directories below describe the implemented MVP, not planned components.

## Top-level layout

| Path | Ownership |
| --- | --- |
| `cmd/conn` | Main package for the single `conn` executable. It wires dependencies and contains no product behavior. |
| `internal/` | Private Go implementation. Its packages are grouped by adapter, application, persistence, protocol, and support responsibility. |
| `contracts/v1` | Canonical versioned JSON Schema protocol contracts and cross-language fixtures. |
| `contracts/embed.go` | Embeds `contracts/v1` for Go components. |
| `packages/contracts` | Node.js and TypeScript AJV validation adapter for the canonical schemas. |
| `packages/typescript-analyzer` | Buildable placeholder for future TypeScript analysis. It has no analysis behavior yet. |
| `internal/migrations/sql` | Goose SQL migrations for Conn's backend database. |
| `compose.yaml` | Local PostgreSQL 18.7 development database. |
| `scripts/` | Root-tooling support, including Go formatting validation. |
| `docs/` | Maintained architecture, source layout, real-trial, and decision records. |

`apps/web`, `internal/controlplane`, `internal/github`, `internal/sidecar`, and `fixtures` contain no implemented MVP behavior. Git does not retain empty directories, so they may disappear until a later accepted decision gives them a concrete responsibility.

## Go boundaries

```text
cmd/conn
   ├── internal/cli ────── internal/account ───── internal/auth/github
   │       │                       │                         │
   │       │                       ├──── internal/credentials │
   │       │                       └──── internal/httpclient ─┤
   │       │                                                 │
   ├── internal/mcp ─────────────── internal/httpclient ─────┤
   │                                                         │
   └── internal/httpapi ─────────── internal/application ─── internal/postgres
                                             │                    │
                              internal/protocol + contracts ─────┘
                                             │
                                   internal/contracts
```

### Process and presentation adapters

- `cmd/conn` constructs the dependencies for `conn`, `conn server`, and `conn mcp`.
- `internal/cli` defines Cobra parsing, `--file` and `--json` behavior, output rendering, and stable exit codes. It must not contain domain, database, or HTTP behavior.
- `internal/mcp` defines the Kiro stdio adapter and its four read-only tools. SDK types stay in this package.
- `internal/httpapi` defines the `/v1` HTTP handlers, authentication middleware, input/output boundaries, health/readiness endpoints, request IDs, and access logging.
- `internal/httpclient` is the authenticated backend client used by the CLI account flow and MCP adapter.

### Application and persistence

- `internal/application` owns authorization, project workflow rules, session behavior, blueprint lifecycle, charter decisions, optimistic concurrency, and event sequencing. It works through store and transaction interfaces.
- `internal/postgres` implements those store interfaces with pgx and handwritten parameterized SQL.
- `internal/migrations` embeds, checks, and applies Conn-owned Goose migrations. `internal/cmd/conn-migrate` is the development, CI, and deployment migration executable; it is not part of the public `conn` CLI.

### Identity and local environment

- `internal/account` combines Device Flow, credential resolution, and the backend client for `login`, `logout`, `whoami`, and `doctor`.
- `internal/auth/github` performs CLI Device Flow and backend GitHub identity verification. `githubtest` is its local fake GitHub server.
- `internal/credentials` resolves `CONN_TOKEN` or the operating-system credential store without a plaintext fallback.
- `internal/devenv` reads supported local development environment values.
- `internal/secret` holds sensitive values in a type that avoids accidental formatting or logging.

### Protocol boundary

- `contracts/v1` remains the source of truth for protocol documents. Human-authored input schemas omit server-assigned fields. Record, view, error, and event schemas describe data that crosses a process boundary.
- `internal/contracts` loads embedded schemas and validates untyped JSON before it reaches typed application logic.
- `internal/protocol` owns the matching handwritten Go records, identity and timestamp rules, codecs, and typed event payloads. It validates decoded and encoded records against the schemas.
- `packages/contracts` provides the equivalent Node.js/TypeScript Ajv registry. Its test runs the same `contracts/v1/fixtures/cases.json` data as Go.

Schema validation verifies document shape. `internal/application` and PostgreSQL enforce rules that depend on current state, including project authority, charter assignment, revision ordering, and blueprint lifecycle.

## Dependency rules

1. JSON Schema contracts define the public data boundary. Go and TypeScript adapters consume them; neither defines a competing protocol shape.
2. Presentation adapters (`internal/cli`, `internal/mcp`, and `internal/httpapi`) parse and render. They delegate behavior to application operations or the authenticated backend client.
3. `internal/application` owns business rules and depends on interfaces rather than concrete PostgreSQL or HTTP implementations.
4. `internal/postgres` owns SQL persistence. It does not own HTTP, CLI, or MCP behavior.
5. Secrets stay in `internal/secret` values and credential resolution stays in `internal/credentials`.
6. Tests focus on public contracts and fundamental invariants. Go tests sit beside the behavior they protect; the Node shared-fixture test sits in `packages/contracts/test`.

## Root commands

Root `pnpm` scripts coordinate formatting, checks, tests, builds, and local database operations. They are intentionally the only task interface:

```text
pnpm db:start     start the Compose PostgreSQL container
pnpm db:migrate   apply Conn backend migrations
pnpm db:status    show the PostgreSQL container state
pnpm db:stop      stop the container without removing its volume
pnpm format       format Go, JavaScript, TypeScript, and JSON
pnpm check        run formatting validation, static checks, and tests
pnpm test         run Node and Go tests
pnpm build        build Go and Node workspaces
```

See [architecture](architecture.md) for runtime behavior and [the testing policy](decisions/0003-minimal-fundamental-testing.md) for the criteria used to keep the suite small.
