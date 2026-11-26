# Design

## Toolchains

- Go 1.27.4
- Node.js 26.13.1 LTS
- pnpm 12.21.0
- TypeScript 7.0.2
- Biome 2.6.8
- Vitest 5.1.0

The repository pins exact versions. pnpm workspaces manage Node.js packages. The root package scripts call both Node.js and Go tools without a separate task runner.

## Repository structure

```text
cmd/conn/                      minimal Go CLI entry point
internal/                      documented Go-side component boundaries
apps/web/                      web application boundary; framework deferred
packages/typescript-analyzer/  minimal buildable TypeScript package
contracts/                     future language-neutral protocol definitions
integrations/kiro/             Kiro adapter boundary
fixtures/                      future shared behavioral scenarios
.kiro/steering/                persistent product and technical guidance
.kiro/specs/                   Kiro Feature Specs
```

The `cmd`, `internal`, `apps`, and `packages` conventions preserve idiomatic language layouts inside one repository. This scaffold does not decide whether every documented boundary becomes a separate process.

## Go shell

A root Go module contains one `cmd/conn` executable. The executable only reports that it is a development scaffold. Other Go-side boundaries contain responsibility documents and no behavior.

## Node.js shell

The TypeScript analyzer package compiles one capability descriptor. It contains no repository analysis behavior. The root TypeScript project references the package and uses strict compiler settings.

## Root command surface

- `pnpm format`: apply Biome and Go formatting
- `pnpm check`: run non-mutating formatting checks, type checking, fundamental tests, and Go vetting
- `pnpm test`: run Vitest and Go tests
- `pnpm build`: build Go and TypeScript shells

A small cross-platform Node.js script checks tracked and untracked Go files with `gofmt -l`. It is repository tooling, not product behavior.

## Testing strategy

No artificial application test is created. Vitest runs with `--passWithNoTests`, and `go test ./...` validates the current packages without requiring a test file. Tests will be added only when a fundamental behavior from decision 0004 exists.

## Continuous integration

One GitHub Actions workflow provisions the exact Go and Node.js versions, installs the package-manager version declared by the repository, installs dependencies from the lockfile, and runs `pnpm check` followed by `pnpm build`.

Third-party Actions are pinned to commit hashes with release names in comments.

## Security and configuration

- Secrets and `.env` files are ignored by default.
- No MCP configuration is created in this scaffold.
- No Kiro hook executes commands in this scaffold.
- No production credentials are required.
- No source code, prompts, or telemetry are collected.

## Deferred decisions

- Web framework and UI system
- Go HTTP or RPC framework
- MCP SDK and tool contract
- Protocol schema technology
- PostgreSQL and SQLite libraries
- Authentication and authorization implementation
- Deployment platform
- Process boundaries and release packaging
