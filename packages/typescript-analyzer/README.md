# TypeScript analyzer

## Responsibility

Provide TypeScript-specific static evidence for future blueprint and intent analysis. The scaffold exports only a buildable capability descriptor.

## Dependencies

This package may use the TypeScript compiler ecosystem after analyzer requirements and output contracts are approved.

## Deferred

- Compiler API or Tree-sitter selection
- Symbol and dependency graph extraction
- OpenAPI and SQL concept extraction
- Incremental indexing
- Evidence confidence and provenance

## Scaffold check

Run `pnpm --filter @conn/typescript-analyzer build` from the repository root.
