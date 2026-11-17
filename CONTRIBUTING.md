# Contributing to Conn

Conn is in its planning and mechanical-scaffold phase. Product behavior and framework choices require accepted decision records before implementation.

## Prerequisites

- Git
- Go 1.27.4
- Node.js 26.13.1 LTS
- pnpm 12.21.0

## Setup

```text
pnpm install --frozen-lockfile
```

Use `pnpm install` without `--frozen-lockfile` only when intentionally changing dependencies.

## Commands

```text
pnpm format
pnpm check
pnpm test
pnpm build
```

`pnpm check` runs Biome, TypeScript checking, Vitest, Go formatting validation, `go vet`, and `go test`.

## Testing policy

Keep tests limited to public contracts and fundamental invariants. Do not add repetitive generated tests, tests of trivial implementation details, or coverage-percentage gates. Every new test should name the contract, invariant, security boundary, integration path, migration guarantee, or reproduced defect it protects.

## Kiro MVP

Kiro is the only coding-agent integration in the MVP. Do not add Cursor, Claude Code, Codex, or other agent adapters before the Kiro experiment is complete and a later decision expands the scope.
