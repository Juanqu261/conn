# 0006: Pre-edit coordination

- Status: accepted
- Owner: project owner
- Amends: [0002](0002-runtime-and-interaction-surfaces.md) (bundle content and hook boundary), [0003](0003-local-typescript-analysis.md) (analyzer input and Git access)

## Context

Decision 0005 makes the warning before an edit the product core. The implemented loop rarely warned: participants invented different subject strings, local analysis ran only after implementation and could not produce a producer-consumer collision, human decisions never closed a collision, and the optional hooks were credit-consuming agent prompts that never ran before a write.

The alternatives were a new provenance kind for pre-edit claims, a hosted or model-based concept matcher, a persistent local sidecar that watches the worktree, and blocking hooks. They added a new trust level, sent data off the machine, added a background process, or crossed the warn-only boundary in decision 0005.

## Decision

1. Pre-edit claims are declared claims. `suggest_claims` returns candidates, and the participant declares the chosen ones. No provenance kind is added.
2. The analyzer has `planned` and `observed` modes and request and report version 2. In observed mode it may run local, read-only `git` commands with fixed arguments, no shell, a timeout, and a minimized environment to read the base versions of changed files. Base content stays in memory, and output remains source-free. Version 1 is removed because the analyzer and the Go adapter ship together.
3. The analyzer resolves from `CONN_ANALYZER_PATH`, then the directory of the `conn` executable, then the current directory. Node.js resolves from `CONN_NODE_PATH`, then `PATH`.
4. Human decisions set collision states: `false_positive` dismisses; `dependency`, `sequencing`, `contract_agreement`, and `accepted_risk` resolve; other kinds put the collision under review. An assessment for a later Intent revision inherits a dismissed or resolved state only when its matched subjects are a subset of the decided subjects. A member who is not an owner may record `false_positive` only for a collision on their own Intent.
5. The optional Kiro bundle contains command hooks for SessionStart, UserPromptSubmit, PreToolUse on writes, and PostToolUse on writes, and no agent-prompt hooks. Hooks read from the backend and never write to it. Hooks only warn: none exits 2, prints a permission decision, or asks for permission, and every failure allows the event silently.
6. Hooks and the MCP adapter share small, non-authoritative local state under the worktree's Git directory. Process-local Project selection keeps its precedence. The focus record never grants access and contains no credential, prompt, source, or Project prose.

The concept catalog is a deterministic read model over current Objective revisions and active claims. It uses token similarity on subjects and display names only, never embeddings, a hosted model, or source content.

## Consequences

- Agents get a shared vocabulary and precise claims before they write, so collisions open before conflicting edits exist.
- A dismissed warning stays dismissed across revisions that add no new matched subject, and a new subject reopens it.
- The analyzer gains a read-only Git dependency, and observed analysis needs a base revision. Intents record one when the worktree allows it.
- Hook behavior depends on Kiro's documented exit-code semantics, which must be verified in Kiro CLI and the IDE. Exit code 1 is the selected non-blocking warning channel, and `--warn-exit-code 0` keeps warnings in the prompt-submit queue only.
- The CLI and TUI keep working without the bundle; the bundle remains removable without changing authoritative state.

## Validation

PostgreSQL scenarios cover the catalog, near-match advice, the decision-to-state mapping, non-owner dismissal authority, carry-forward, and reopening. Vitest covers both analyzer modes on a temporary Git repository. A Go scenario with a fake backend covers the hook warnings, one-time reporting, fail-open behavior, and the absence of exit code 2 and permission decisions. A two-participant MCP scenario on `fixtures/pre-edit-two-sessions` proves both assessments open before any file write. Two real Kiro sessions repeat the scenario manually, and the results are recorded in `docs/real-trial.md`.
