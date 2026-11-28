---
inclusion: always
---

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
- The analyzer has planned and observed modes (request and report version 2). Observed mode may run local read-only `git` with fixed arguments, no shell, a timeout, and a minimized environment. Resolve the analyzer from `CONN_ANALYZER_PATH`, then beside the `conn` executable, then the current directory, and Node.js from `CONN_NODE_PATH`, then `PATH`.
- Pre-edit claims are declared claims built from `suggest_claims` candidates. Human decisions set collision states, and later revisions inherit a dismissed or resolved state only while their matched subjects stay covered.
- The optional Kiro bundle contains warn-only command hooks (`conn hook ...`) that read from the backend and never write to it. Hooks and the MCP adapter share non-authoritative local state under the worktree's Git directory.
- Only `conn evidence run` executes check commands, from the repository's `.conn/checks.json`, as argument arrays without a shell and without `CONN_` variables. MCP tools and hooks never run checks. Local observations are labelled as observed on the participant's machine, and only the output hash leaves the machine.
- Evidence may be attached by any Participant of the Human who owns the Intent. Commits name Intents with `Conn-Intent: <intent-id>@<revision>`; Conn writes the `commit-msg` hook only through explicit `conn git-hook install`.
- The review packet is a generated, non-persisted view with the versioned `review-packet` schema, built by the shared `internal/review` loader for the CLI, MCP, and later CI.
- The CI add-on is a GitHub Actions workflow that Conn renders and installs at `.github/workflows/conn-check.yml` from `conn ci install` or the `install_ci_check` MCP tool. It replaces the GitHub App sequence for this version. CI reports findings, never fails a pull request for them, and writes nothing to Conn.
- CI builds `conn` and the analyzer from the pinned Conn source with the pinned toolchains, in one replaceable workflow step.
- CI credentials are created only by a Project owner with `conn ci credential create`, delivered only through `--github-secret` (to `gh secret set` standard input) or `--print`, bound to one Project and RepositoryConnection, expire in 1 to 365 days, are revocable, are stored as SHA-256 hashes, and carry the `conn_ci_` prefix. A CI principal reads only an explicit route allowlist of its own Project plus the collision preview. Agents never create CI credentials.
- `conn verify-pair` runs code from both branches of a collision locally only after confirmation, reads pair commands from `.conn/checks.json` on the default branch, runs them through the shared check runner, and records an advisory, append-only combination check. The analyzer has a `diagnostics` mode whose reports are never attached to an Intent.
- GitHub issue import runs in the CLI with an optional `GH_TOKEN` or `GITHUB_TOKEN` that is never persisted or sent to Conn.
- Project insights are Project-level only, contain no person, and report insufficient data below five samples.
- MCP clients other than Kiro are attributed with participant adapter kind `mcp` from `clientInfo.name`. `conn mcp config` prints configuration and `conn agents install` manages one marker-delimited `AGENTS.md` section from the same source as the Kiro steering; Conn never writes MCP client configuration.
- The Conn Kiro power lives at `powers/conn/` in the Agent Plugins format (`plugin.json`, `mcp.json`, `skills/`), rendered by `internal/kiropower` from `internal/agentguide` and exported with `conn kiro power export`. It declares `conn mcp` from `PATH` with only the `${CONN_SERVER_URL}` placeholder and loads on demand; the always-on steering and warn-only hooks stay workspace files that onboarding installs after preview and confirmation. Power users build `conn` from source until release archives exist. Empty Conn environment values are treated as unset.
- GitHub Spec Kit features are an Objective source (`speckit_feature`) imported by `conn start <feature-dir>` through the local-spec import route. `internal/speckit` extracts FR, SC, clarification, and constitution key points and planned paths from `tasks.md` or the `plan.md` source structure by fixed rules, bounded to 1 MiB per file, refusing symbolic links and paths outside the repository. `conn speckit check` previews collisions for those paths, publishes nothing, and always exits 0. The Conn Spec Kit extension lives at `integrations/speckit/conn/`, rendered by `internal/speckitext` from `internal/agentguide`, exported with `conn speckit extension export`, and installed by the human with `specify extension add --dev`. Conn never installs or runs Spec Kit and never writes `.specify/`. Runtime `events` stay off until agent payloads are confirmed.
- Use Biome for JavaScript, TypeScript, and JSON formatting and linting.
- Use Vitest for fundamental Node.js and TypeScript behavior tests.
- Start with `gofmt`, `go vet`, and `go test` for Go.

## Testing constraint

Keep the automated suite as small as practical. Test public contracts and fundamental invariants rather than implementation details. Prefer parameterized tests, avoid duplicate assertions across layers, and do not add a coverage-percentage gate during the MVP.

The detailed policy is `docs/decisions/0004-minimal-fundamental-testing.md`.

Root task orchestration uses pnpm workspace scripts. Frameworks, persistence libraries, deployment, and exact component process boundaries remain undecided. Ask before selecting them and record each accepted choice.
