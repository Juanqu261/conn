# Accepted decisions

Conn keeps four concise architecture records. New implementation-language, framework, persistence, deployment, UI, or authority choices require another accepted record before code depends on them.

| Record                                            | Scope                                                                                                                                     |
| ------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------- |
| [0001](0001-product-model-and-human-authority.md) | One Blueprint per Objective, immutable revision-scoped Claims, agent limits, and authenticated browser review for future binding actions. |
| [0002](0002-runtime-and-interaction-surfaces.md)  | One executable, CLI/TUI boundaries, Kiro tools and prompts, optional integration files, and pull-by-cursor synchronization.               |
| [0003](0003-local-typescript-analysis.md)         | Bounded credential-free Node.js analysis with TypeScript 5.7.3 inside the analyzer package.                                               |
| [0004](0004-minimal-fundamental-testing.md)       | A small suite focused on public contracts, authority, persistence, security, and critical integration paths.                              |

The accepted technical baseline remains Go 1.27.4, Node.js 26.13.1, pnpm 12.21.0, JSON Schema Draft 2020-12, Go `net/http`, pgx, handwritten SQL, Goose, PostgreSQL 18.7, UUIDv7 identities, integer revisions, Project event cursors, OS credential storage, and opaque 30-day Conn sessions.
