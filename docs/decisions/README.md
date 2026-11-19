# Accepted decisions

This record summarizes the accepted decisions that shape Conn's MVP. It replaces the planning-era per-decision files while retaining the decisions that implementation depends on. [Decision 0003](0003-minimal-fundamental-testing.md) remains separate because it is the project's detailed testing policy.

New decisions must state the context, alternatives, chosen trade-off, consequences, and validation plan before implementation depends on them.

## Product and authority

| Decision | Accepted direction |
| --- | --- |
| Product role | Conn is a coordination protocol and evidence layer, not an agent launcher or supervising agent. |
| MVP scope | Validate one objective with three humans in independent Kiro sessions. Implement Kiro only and retain GitHub as the initial repository host. |
| Human authority | Humans approve objectives and accept their own cell charters. Agents can propose context but cannot approve proposals or silently change accepted boundaries. |
| Project boundary | Projects define authorization and event-cursor boundaries. Owners add already authenticated people by immutable Conn human ID; roles are `owner` and `member`. |
| First workflow | Build objectives, blueprint revisions, and cell charters first. Human writes use the CLI; Kiro MCP tools are read-only. |
| Objective model | Each immutable objective revision has a required Markdown overview and optional keyed outcomes, acceptance criteria, invariants, constraints, or questions. |
| Blueprint and charter model | A blueprint revision contains complete, immutable cell-charter snapshots. Each charter has a Markdown overview and optional responsibilities, exclusions, deliverables, evidence obligations, and explicit cell dependencies. |
| Activation | Every assigned human must accept their own charter before a proposed blueprint revision becomes active. Charter acceptance is append-only. |

## Implementation and contracts

| Decision | Accepted direction |
| --- | --- |
| Languages and repository | Use one monorepo with Go and Node.js with TypeScript. Go is the default for local and service components; TypeScript is for TypeScript-specific analysis and browser-facing work. |
| Toolchain | Use Go 1.27.4, Node.js 26.13.1 LTS, pnpm 12.21.0, pnpm workspaces, and root package scripts. |
| Quality tools | Use Biome for JavaScript, TypeScript, and JSON. Use Vitest for Node and TypeScript behavior tests. Start Go validation with `gofmt`, `go vet`, and `go test`. |
| CLI parsing | Use Cobra only for CLI parsing and presentation. Keep domain, database, and HTTP behavior outside command objects. |
| Protocol contracts | JSON Schema Draft 2020-12 is the versioned, language-neutral contract format. Validate with Ajv 8.20.0 in Node.js and `santhosh-tekuri/jsonschema/v6` 6.0.3 in Go. |
| Models | Use handwritten Go types in `internal/protocol`, held to schemas by one shared-fixture conformance test. Defer TypeScript model generation until it has a consumer. |
| Backend API | The Go backend uses `net/http` and a `/v1` JSON API. |

## Local and backend operation

| Decision | Accepted direction |
| --- | --- |
| Process topology | One local `conn` Go executable runs the CLI, Kiro stdio MCP adapter, and `conn server`. Conn reaches its authoritative backend over HTTPS outside local loopback development. |
| MCP implementation | Keep the official MCP Go SDK behind an adapter boundary. |
| Database | Use PostgreSQL through pgx, handwritten parameterized SQL, and Goose SQL migrations. Migrations stay in backend development and deployment automation, never as a public CLI or MCP operation. |
| Local development | Use PostgreSQL 18.7 through Docker Compose. Run `conn server` natively. Normal commands must not remove the named database volume. |
| Connectivity | Require backend connectivity for the MVP. Defer a local SQLite outbox, persistent sidecar, and background polling. Pull events by cursor during Conn operations. |

## Identity, sessions, and consistency

| Decision | Accepted direction |
| --- | --- |
| Pilot identity | `conn login` runs GitHub Device Flow directly, exchanges the temporary GitHub token with Conn once, and persists only the resulting Conn session token. |
| Local credentials | Store sessions in the operating-system credential store with `zalando/go-keyring` 0.2.8. `CONN_TOKEN` is an explicit non-persistent override. Never fall back to plaintext storage. |
| Session tokens | Use 256-bit opaque tokens, server-side SHA-256 hashes, a 30-day absolute expiry, immediate revocation, and no refresh token during the MVP. |
| Record consistency | Use UUIDv7 identities, integer aggregate revisions for optimistic concurrency, and per-project `bigint` event cursors. |
