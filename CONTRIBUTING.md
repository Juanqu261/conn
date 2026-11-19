# Contributing to Conn

Conn's MVP is implemented. Keep changes within its current boundary: the Kiro integration, authenticated CLI writes, read-only MCP tools, and the objective-blueprint-cell-charter workflow.

## Prerequisites

- Git
- Docker Desktop or Docker Engine with Compose
- Go 1.27.4
- Node.js 26.13.1 LTS
- pnpm 12.21.0

## Setup

```powershell
pnpm install --frozen-lockfile
Copy-Item .env.example .env
pnpm db:start
pnpm db:migrate
```

Set `CONN_GITHUB_CLIENT_ID` in `.env` when validating GitHub sign-in. See the [local run guide](README.md#run-conn-locally) for the complete workflow.

Use `pnpm install` without `--frozen-lockfile` only when intentionally changing dependencies.

## Commands

```powershell
pnpm format
pnpm check
pnpm test
pnpm build
```

`pnpm check` runs Biome, TypeScript checking, Vitest, Go formatting validation, `go vet`, and `go test`.

## Testing

Keep tests limited to public contracts and fundamental invariants. Do not add repetitive generated tests, tests of trivial implementation details, or a coverage-percentage gate. Each new test must name the contract, invariant, security boundary, integration path, migration guarantee, or reproduced defect that it protects.

The complete testing policy is [decision 0003](docs/decisions/0003-minimal-fundamental-testing.md).

## Scope

Kiro is the only coding-agent integration in the MVP. Do not add Cursor, Claude Code, Codex, or another agent adapter before a recorded product decision expands that scope.

Do not introduce a new implementation language, runtime, package manager, framework, persistence library, API framework, deployment platform, repository topology, database library, or UI system without first recording an accepted decision under [`docs/decisions/`](docs/decisions/).
