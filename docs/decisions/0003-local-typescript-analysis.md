# 0003: Local TypeScript analysis

- Status: accepted
- Owner: project owner

## Context

Conn needs observed TypeScript impact Claims without uploading source or giving analysis code credentials, database access, or a persistent process.

Alternatives were a long-running sidecar, hosted analysis, the root TypeScript package, or another parser. They added lifecycle/privacy cost or lacked the required TypeScript project and symbol resolution.

## Decision

- Go invokes `packages/typescript-analyzer/dist/cli.js` as a direct local Node.js subprocess.
- The boundary is versioned, bounded JSON on standard input and one `local-analyzer-report` on standard output.
- The analyzer receives a trusted worktree, normalized repository identity, and optional relative paths. It receives no Conn token or database access.
- Source, diffs, prompts, transcripts, credentials, and unbounded diagnostics are excluded or rejected.
- Go enforces timeout, cancellation, environment minimization, worktree containment, schema validation, and exact Intent-revision attachment before authenticated submission.
- `@conn/typescript-analyzer` pins TypeScript 5.7.3 for the compiler API. Root tooling retains TypeScript 7.0.2.
- A sidecar, hosted analyzer, additional language analyzer, or compiler API upgrade requires separate validation and, when it changes this boundary, a new decision.

## Consequences

Analysis stays local and source-free at the cost of Node.js startup and two TypeScript versions. Declared Claims remain usable when analysis is unavailable.

## Validation

Fixtures prove project discovery, exports/imports, bounded reports, source and credential exclusion, traversal rejection, timeout/cancellation, exact revision checks, deterministic collision assessment, and workspace build/type-check compatibility.
