# Code structure

Conn is a Go-first monorepo with language-neutral contracts and two small Node.js/TypeScript workspaces.

## Layout

| Path                                                               | Responsibility                                                                                           |
| ------------------------------------------------------------------ | -------------------------------------------------------------------------------------------------------- |
| `cmd/conn`                                                         | Wires the single executable. No product behavior.                                                        |
| `internal/cli`                                                     | Cobra parsing, local file input, rendering, and stable exit codes.                                       |
| `internal/tui`                                                     | Bubble Tea terminal application and reviewed in-memory forms.                                            |
| `internal/mcp`                                                     | Kiro stdio tools, prompts, resources, session selection, analysis orchestration, and generated handoffs. |
| `internal/kirointegration`                                         | Optional steering/hook templates and conflict-safe explicit installation.                                |
| `internal/account`, `internal/auth/github`, `internal/credentials` | Device Flow, session resolution, and OS credential storage.                                              |
| `internal/httpclient`                                              | Authenticated client shared by CLI, TUI, and MCP.                                                        |
| `internal/httpapi`                                                 | `/v1` handlers, middleware, validation, health, and readiness.                                           |
| `internal/application`                                             | Authorization, lifecycle, revisions, collisions, evidence, and events.                                   |
| `internal/postgres`                                                | pgx persistence and parameterized SQL.                                                                   |
| `internal/migrations`                                              | Embedded Goose migrations used by development/deployment automation.                                     |
| `internal/protocol`                                                | Handwritten Go records, codecs, timestamps, identities, and typed events.                                |
| `internal/analyzer`                                                | Bounded credential-free Node.js subprocess adapter.                                                      |
| `internal/repositorygit`, `internal/kirospec`                      | Credential-free repository identity and local spec hashing.                                              |
| `contracts/v1`                                                     | Canonical JSON Schema contracts and shared fixtures.                                                     |
| `packages/contracts`                                               | Ajv contract adapter and fixture tests.                                                                  |
| `packages/typescript-analyzer`                                     | Implemented compiler-API analyzer and CLI artifact.                                                      |
| `fixtures/semantic-intent-radar-two-intents`                       | Controlled source-free semantic coordination fixture.                                                    |
| `docs/decisions`                                                   | Accepted architecture records and summary.                                                               |

## Dependency direction

```text
CLI / TUI / MCP ──> HTTP client ──> /v1 API ──> application ──> PostgreSQL
                              protocol + contracts <──────────┘
MCP ──> analyzer adapter ──> Node.js analyzer
```

Rules:

1. JSON Schema defines public document shapes. Go and TypeScript consume it rather than inventing competing models.
2. Presentation adapters parse, render, and transport. Domain and database rules stay outside Cobra, Bubble Tea, and MCP handlers.
3. `internal/application` owns authority and state transitions through store interfaces.
4. `internal/postgres` owns SQL; `internal/httpapi` owns HTTP.
5. Credentials stay in `secret.String` and never enter analyzer input.
6. Tests protect public contracts and fundamental invariants rather than implementation details.

## Processes

- `conn server` runs the authoritative backend.
- `conn mcp` is started and stopped by Kiro.
- `conn app` is an interactive human-authoritative client.
- `packages/typescript-analyzer/dist/cli.js` runs only as a bounded child process.
- PostgreSQL runs in Docker Compose for local development.

No persistent sidecar or browser process exists in the MVP.

## Root commands

```text
pnpm db:start      start PostgreSQL
pnpm db:migrate    apply Conn migrations
pnpm db:status     inspect the container
pnpm db:stop       stop without deleting data
pnpm format        format supported files
pnpm check         lint, type-check, vet, and test
pnpm test          run Node and Go tests
pnpm build         build Go and Node components
```

See [architecture](architecture.md) for runtime behavior and [decision 0004](decisions/0004-minimal-fundamental-testing.md) for the testing policy.
