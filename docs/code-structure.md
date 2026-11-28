# Code structure

Conn is a Go-first monorepo with language-neutral contracts and two small Node.js/TypeScript workspaces.

## Layout

| Path                                                               | Responsibility                                                                                                                                          |
| ------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `cmd/conn`                                                         | Wires the single executable. No product behavior.                                                                                                       |
| `internal/cli`                                                     | Cobra parsing, local file input, rendering, stable exit codes, and the `conn hook` commands.                                                            |
| `internal/tui`                                                     | Bubble Tea terminal application and reviewed in-memory forms.                                                                                           |
| `internal/mcp`                                                     | MCP stdio tools, prompts, resources, session selection, client attribution, analysis orchestration, focus records, handoffs, and CI workflow tools.     |
| `internal/preedit`                                                 | Worktree-bound pre-edit steps shared by MCP and CLI: Git context filling and claim suggestions.                                                         |
| `internal/review`                                                  | Review proof shared by CLI, MCP, and later CI: scope drift, the trailer parser, the shared loader, and packets.                                         |
| `internal/checkrun`                                                | `.conn/checks.json` validation and shell-free check runs that become observed evidence. CLI only.                                                       |
| `internal/pairverify`                                              | `conn verify-pair`: branch resolution, default-branch pair commands, temporary worktrees, merge, diagnostic delta, pair checks, and cleanup. CLI only.  |
| `internal/cicheck`                                                 | `conn ci check`: event parsing, trailers, observed analysis, collision preview, reader-independent packets, the bounded report, and the marker comment. |
| `internal/ciaddon`                                                 | The embedded GitHub Actions workflow template and its conflict-safe preview, install, and removal.                                                      |
| `internal/agentguide`                                              | The single source of the Kiro steering file, the marker-delimited `AGENTS.md` section with its lifecycle, and the Kiro power's skill bodies.            |
| `internal/lineend`                                                 | Line-ending-insensitive comparison for installers, so files Git checks out with CRLF still count as unchanged Conn content.                             |
| `internal/githubissue`, `internal/workstart`                       | GitHub issue reading and criteria suggestions with a local-only token, and `conn start` Objective resolution.                                           |
| `internal/speckit`                                                 | Deterministic, bounded reading of Spec Kit features: title, overview, FR/SC/question/constitution key points, planned paths, and the current feature.   |
| `internal/speckitext`                                              | The Conn Spec Kit extension: `extension.yml` and command files rendered from `internal/agentguide`, and conflict-safe export.                           |
| `internal/githook`                                                 | Conflict-safe `commit-msg` installation and the `Conn-Intent` trailer writer.                                                                           |
| `internal/hooks`                                                   | Warn-only Kiro command hooks: session start, prompt submit, pre-write, and post-write. Backend reads only.                                              |
| `internal/hookstate`                                               | Non-authoritative local hook state under the worktree's Git directory, with atomic writes and a lock file.                                              |
| `internal/kirointegration`                                         | Optional steering and command-hook templates, legacy migration, and conflict-safe explicit installation.                                                |
| `internal/kiropower`                                               | The Conn Kiro power: rendering, version and digest, conflict-safe export, and read-only detection of manual Kiro MCP registrations.                     |
| `internal/envvalue`                                                | Reads Conn environment variables, treating an empty or whitespace-only value as unset.                                                                  |
| `powers/conn`                                                      | The committed, rendered Kiro power (`plugin.json`, `mcp.json`, `skills/`, `.digest`). Regenerate with `conn kiro power export`.                         |
| `integrations/speckit/conn`                                        | The committed, rendered Spec Kit extension. Regenerate it by removing the folder and running `conn speckit extension export`.                           |
| `internal/account`, `internal/auth/github`, `internal/credentials` | Device Flow, session resolution, and OS credential storage.                                                                                             |
| `internal/httpclient`                                              | Authenticated client shared by CLI, TUI, MCP, and hooks.                                                                                                |
| `internal/httpapi`                                                 | `/v1` handlers, middleware, validation, health, and readiness; Conn Lab browser sign-in, browser sessions, CSRF, and the event stream.                  |
| `internal/webui`                                                   | Serves the embedded Conn Lab build from `site/` with the SPA fallback, caching, and Content-Security-Policy, or a build placeholder.                    |
| `internal/eventstream`                                             | In-memory wake-ups and stream limits for Conn Lab's Server-Sent Events; cursors in PostgreSQL stay the source of truth.                                 |
| `internal/application`                                             | Authorization, lifecycle, revisions, the concept catalog, collisions and their decisions, evidence, and events.                                         |
| `internal/postgres`                                                | pgx persistence and parameterized SQL.                                                                                                                  |
| `internal/migrations`                                              | Embedded Goose migrations used by development/deployment automation.                                                                                    |
| `internal/protocol`                                                | Handwritten Go records, codecs, timestamps, identities, and typed events.                                                                               |
| `internal/analyzer`                                                | Bounded credential-free Node.js subprocess adapter and analyzer resolution.                                                                             |
| `internal/repositorygit`, `internal/kirospec`                      | Credential-free repository identity, read-only Git context, and local spec hashing.                                                                     |
| `contracts/v1`                                                     | Canonical JSON Schema contracts and shared fixtures.                                                                                                    |
| `packages/contracts`                                               | Ajv contract adapter and fixture tests.                                                                                                                 |
| `packages/typescript-analyzer`                                     | Compiler-API analyzer with planned and observed modes, and its CLI artifact.                                                                            |
| `apps/web`                                                         | Conn Lab: the React and TypeScript single-page application built by Vite into `internal/webui/site`.                                                    |
| `fixtures/semantic-intent-radar-two-intents`                       | Controlled source-free semantic coordination fixture.                                                                                                   |
| `fixtures/pre-edit-two-sessions`                                   | Producer and consumer files for the two-session pre-edit acceptance scenario.                                                                           |
| `docs/decisions`                                                   | Accepted architecture records and summary.                                                                                                              |

## Dependency direction

```text
CLI / TUI / MCP / hooks ──> HTTP client ──> /v1 API ──> application ──> PostgreSQL
                                     protocol + contracts <──────────┘
MCP / CLI ──> preedit ──> analyzer adapter ──> Node.js analyzer ──> read-only git
MCP / CLI ──> review ──> HTTP client + read-only git + hookstate (written paths)
CLI ──> pairverify ──> git worktrees + analyzer (diagnostics) + checkrun ──> HTTP client (combination check)
CI ──> cicheck ──> HTTP client (CI credential) + analyzer (observed) + review.LoadIndependent ──> job summary / GitHub comment
CLI / MCP ──> ciaddon, agentguide ──> one workflow file or one AGENTS.md section (never MCP configuration)
CLI ──> kiropower ──> agentguide ──> powers/conn or a folder the human names (never MCP configuration)
CLI ──> speckit (local Spec Kit files) ──> preedit ──> HTTP client (collision preview; never .specify/)
CLI ──> speckitext ──> agentguide ──> integrations/speckit/conn or a folder the human names
CLI ──> checkrun ──> check command (argument array, no shell, no CONN_ variables)
git commit ──> commit-msg ──> conn trailer ──> githook ──> hookstate focus
MCP / hooks ──> hookstate (worktree Git directory)
```

Rules:

1. JSON Schema defines public document shapes. Go and TypeScript consume it rather than inventing competing models.
2. Presentation adapters parse, render, and transport. Domain and database rules stay outside Cobra, Bubble Tea, MCP handlers, and hooks.
3. `internal/application` owns authority and state transitions through store interfaces.
4. `internal/postgres` owns SQL; `internal/httpapi` owns HTTP.
5. Credentials stay in `secret.String` and never enter analyzer input, Git subprocesses, check commands, or hook state.
6. Hooks never write to the backend, never exit 2, and never print a permission decision. `conn trailer` always exits 0.
7. Only `conn evidence run` and `conn verify-pair` execute check commands; MCP tools and hooks never do.
8. CI credentials reach only the routes registered with `readable`, for their own Project, and never write. `conn ci check` always exits 0.
9. Tests protect public contracts and fundamental invariants rather than implementation details.

## Processes

- `conn server` runs the authoritative backend.
- `conn mcp` is started and stopped by Kiro.
- `conn hook <name>` runs once per Kiro hook event within a three-second budget.
- `conn evidence run` runs one reviewed check as a bounded child process with its configured timeout.
- `conn trailer` runs once per commit from the optional `commit-msg` hook, after the message is edited.
- `conn verify-pair` runs once per confirmed pair verification, with temporary worktrees it removes.
- `conn ci check` runs once per pull-request workflow run on a CI runner.
- `conn app` is an interactive human-authoritative client.
- `packages/typescript-analyzer/dist/cli.js` runs only as a bounded child process.
- PostgreSQL runs in Docker Compose for local development.

No persistent sidecar exists in the MVP. Conn Lab runs in the browser and holds one event stream per open Project tab.

## Root commands

```text
pnpm db:start      start PostgreSQL
pnpm db:migrate    apply Conn migrations
pnpm db:status     inspect the container
pnpm db:stop       stop without deleting data
pnpm format        format supported files
pnpm check         lint, type-check, build the analyzer, vet, and test
pnpm test          run Node and Go tests
pnpm build         build Go and Node components
```

See [architecture](architecture.md) for runtime behavior and [decision 0004](decisions/0004-minimal-fundamental-testing.md) for the testing policy.
