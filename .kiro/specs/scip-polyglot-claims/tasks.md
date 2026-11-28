# Tasks

- [ ] 1. Review and approve the SCIP claims scope
  - Confirm the acceptance goal: collisions for languages other than TypeScript through SCIP indexes, with the backend and assessor unchanged.
  - Confirm the scope boundary: no collisions across language boundaries in this milestone.
  - Confirm that indexing runs only from the CLI and never from MCP, hooks, or CI.
  - Do not begin implementation until the specification and decision gate are approved. Requirements: 2, 5, 6.

- [ ] 2. Record decision 0012
  - Add `docs/decisions/0012-scip-claims.md`: the Go bindings with `google.golang.org/protobuf`, the `conn analyzer scip` subprocess boundary, repository-declared indexers, CLI-only indexing, and TypeScript staying on its analyzer. Record that it amends 0003 and 0006.
  - Update `docs/decisions/README.md`, `.kiro/steering/tech.md`, and the delivery order in `.kiro/steering/product.md`. Requirements: 1, 2, 6.

- [ ] 3. Spike SCIP on real indexers
  - Run `scip-go` and `scip-python` on small fixture projects. Confirm the bindings' streaming and symbol-parsing API, enclosing-range availability per indexer, default output names, and `language` strings.
  - Record the findings in the design's "SCIP facts" section, complete the language alias table, and stop for review if a finding changes a requirement. Requirements: 4, 5.

- [ ] 4. Define the contracts
  - Add `scip-config`, `scip-analyzer-request`, `scip-index-metadata`, and optional `sources` on `local-analyzer-report`.
  - Add accepted and rejected fixtures and Go types. Requirements: 1, 6.

- [ ] 5. Implement subject normalization and claim rules
  - Add `internal/scipclaims` with descriptor-to-name normalization, the shared `stableName` and `subjectPath` rules (moved to one Go helper and checked against the TypeScript analyzer's outputs for shared cases), the two-pass reader, and the planned and observed claim rules with bounds.
  - Add the synthetic index tests. Requirements: 3, 4, 5, 6.

- [ ] 6. Implement `conn analyzer scip`
  - Hidden subcommand with the minimized environment, request validation, read-only Git diff parsing, and the report with `sources`.
  - Add the `go-sample` fixture with its regeneration scripts and the fixture tests. Requirements: 4, 6, 8.

- [ ] 7. Implement `conn index`
  - Configuration loading, the shared runner, index validation, caching with metadata and retention, and `--base` with temporary worktree cleanup.
  - Add the fake-indexer test. Requirements: 1, 2.

- [ ] 8. Integrate with planned and observed analysis
  - Split targets by language, run both analyzers, merge deterministically, and report uncovered and stale paths in `suggest_claims`, `analyze_intent`, `conn start`, and `conn intent analyze` summaries.
  - Add the backend collision scenario. Requirements: 3, 4, 6, 7, 8.

- [ ] 9. Diagnostics and documentation
  - `conn doctor` and `session_status` report indexers, `PATH` availability, cached indexes, freshness, and an untracked-output warning.
  - README "Other languages with SCIP" section with example configurations for `scip-go`, `scip-python`, and `scip-java`. Requirements: 7.

- [ ] 10. Run the real trial
  - On a repository with Go or Python code, two participants plan overlapping changes. Record the pre-edit warning, observed claims after the change, and any indexer friction in `docs/real-trial.md`. Requirements: 8.
