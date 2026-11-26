---

## inclusion: always

# Accepted technical direction

- Conn uses Go and Node.js with TypeScript.
- Go is the default for local and service-oriented components.
- Node.js with TypeScript is the default for TypeScript-specific analysis and browser-facing development.
- Conn uses one monorepo with language-neutral, versioned contracts between Go and TypeScript components.
- Use Go 1.27.4, Node.js 26.13.1 LTS, and pnpm 12.21.0.
- Use pnpm workspaces and root package scripts without a separate task runner.
- Use JSON Schema Draft 2020-12 as the canonical, language-neutral protocol contract format.
- Validate schemas with Ajv 8.20.0 in Node.js and `santhosh-tekuri/jsonschema/v6` 6.0.3 in Go.
- Use handwritten Go protocol types in `internal/protocol`, held to the JSON Schemas by one conformance test over the shared fixtures; defer TypeScript model generation until a TypeScript component consumes protocol records.
- Use one local `conn` Go executable; Kiro launches `conn mcp` over stdio, and Conn reaches the authoritative backend over HTTPS.
- Use the same Go binary with a separate `conn server` mode for the MVP.
- Implement the `/v1` backend JSON API with Go `net/http`.
- Use the official MCP Go SDK behind the MCP adapter boundary.
- Authenticate pilot humans with GitHub OAuth device flow.
- Access PostgreSQL with pgx, handwritten parameterized SQL, and Goose SQL migrations.
- Require backend connectivity initially; defer the local SQLite outbox.
- Build objective, blueprint revision, and three accepted cell charters as the first vertical slice.
- Pull team updates by event cursor during Conn operations; do not run a background polling timer initially.
- Store local Conn session credentials in the operating-system credential store through `zalando/go-keyring` 0.2.8; allow `CONN_TOKEN` only as an explicit non-persistent override.
- Never use an automatic plaintext credential fallback.
- Use 256-bit opaque Conn session tokens with server-side SHA-256 hashes, 30-day absolute expiry, and immediate revocation; do not add refresh tokens during the MVP.
- Use UUIDv7 for entity identities, integer aggregate revisions for optimistic concurrency, and per-project `bigint` event cursors.
- Use PostgreSQL 18.7 through Docker Compose for local development; run `conn server` natively and never delete the named database volume through normal commands.
- Version objectives independently; each immutable BlueprintRevision contains the complete set of CellCharter snapshots, while CharterAcceptance remains a separate append-only record.
- Require every assigned human to accept their own charter before a BlueprintRevision becomes active; agents and other humans cannot accept on their behalf.
- Build the first human workflow through authenticated CLI commands with JSON inputs. Keep initial Kiro MCP context tools read-only, then add participant-owned Intent publication and non-binding resolution proposals through the approved semantic protocol; defer the web application until protocol validation.
- Use explicit Projects as authorization and event-cursor boundaries with only `owner` and `member` roles in the MVP; owners add already authenticated humans by immutable Conn human UUID.
- Use Cobra only for CLI parsing and presentation; keep domain, database, and HTTP behavior outside command objects.
- Keep Conn's own Goose migrations inside backend development and deployment automation; expose no public CLI or MCP database-migration operation and never execute migrations in customer repositories.
- Model each Objective revision as a required Markdown overview plus an optional small set of keyed outcomes, acceptance criteria, invariants, constraints, or questions; do not force every statement into structured fields.
- Model each CellCharter snapshot as a required Markdown overview plus optional keyed responsibilities, exclusions, deliverables, and evidence obligations; keep cross-cell dependencies as explicit cell references.
- Run GitHub device authorization directly in `conn login`, exchange the temporary GitHub token once with the backend, and persist only the resulting Conn session token.
- Do not add a persistent local sidecar until background behavior justifies it.
- Store each declared or observed SemanticClaim as an immutable record attached to one exact IntentRevision. Radar views may group equivalent normalized subjects but do not create mutable or globally canonical claims in the MVP.
- Run TypeScript repository analysis locally as a direct Node.js subprocess with versioned JSON standard input and bounded JSON standard output. The analyzer receives no Conn session token and has no database access. It uses its direct TypeScript 5.7.3 compiler API dependency while root TypeScript 7.0.2 remains workspace tooling. Defer a persistent sidecar and hosted analysis.
- Use Biome for JavaScript, TypeScript, and JSON formatting and linting.
- Use Vitest for fundamental Node.js and TypeScript behavior tests.
- Start with `gofmt`, `go vet`, and `go test` for Go.

## Testing constraint

Keep the automated suite as small as practical. Test public contracts and fundamental invariants rather than implementation details. Prefer parameterized tests, avoid duplicate assertions across layers, and do not add a coverage-percentage gate during the MVP.

The detailed policy is `docs/decisions/0004-minimal-fundamental-testing.md`.

Root task orchestration uses pnpm workspace scripts. Frameworks, persistence libraries, deployment, and exact component process boundaries remain undecided. Ask before selecting them and record each accepted choice.
