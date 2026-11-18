# Requirements

## 1. Reproducible foundation

**User story:** As a Conn contributor, I want explicit production toolchain versions so that local and CI behavior can be reproduced.

### Acceptance criteria

1. WHEN a contributor inspects the repository
   THE SYSTEM SHALL declare Go 1.27.4, Node.js 26.13.1 LTS, and pnpm 12.21.0
2. WHEN CI validates the repository
   THE SYSTEM SHALL provision the declared Go and Node.js versions

## 2. Mixed-language monorepo

**User story:** As a contributor, I want one repository command surface so that Go and Node.js components can be checked consistently.

### Acceptance criteria

1. WHEN dependencies are installed
   THE SYSTEM SHALL use pnpm workspaces for Node.js packages and Go modules for Go packages
2. WHEN a contributor runs the root check command
   THE SYSTEM SHALL run Biome, TypeScript checking, Vitest, Go formatting validation, `go vet`, and `go test`
3. WHEN a contributor runs the root build command
   THE SYSTEM SHALL build the scaffolded Go and TypeScript packages

## 3. Minimal executable and package shells

**User story:** As a contributor, I want small buildable shells so that later components have a verified foundation without premature product behavior.

### Acceptance criteria

1. WHEN the Go scaffold is built
   THE SYSTEM SHALL produce a minimal Conn CLI entry point that reports development build information
2. WHEN the TypeScript scaffold is built
   THE SYSTEM SHALL compile a minimal TypeScript analyzer package
3. WHEN component boundaries are inspected
   THE SYSTEM SHALL document their responsibility and explicitly deferred decisions

## 4. Kiro-only MVP integration

**User story:** As the product owner, I want the MVP integration limited to Kiro so that the blueprint and work-cell workflow can be validated before supporting other coding agents.

### Acceptance criteria

1. WHEN project guidance is loaded by Kiro
   THE SYSTEM SHALL describe the Conn product boundary, Kiro-only MVP, accepted stack, and architecture decision policy
2. WHEN integration placeholders are inspected
   THE SYSTEM SHALL include Kiro and SHALL NOT include executable adapters for Cursor, Claude Code, Codex, or other coding agents
3. WHEN the scaffold is complete
   THE SYSTEM SHALL NOT create MCP runtime configuration, hooks, or a custom agent before their contracts and permissions are approved

## 5. Fundamental testing only

**User story:** As the project owner, I want a small test suite focused on important guarantees so that generated test volume does not become maintenance work.

### Acceptance criteria

1. WHEN the scaffold has no product behavior to test
   THE SYSTEM SHALL permit Vitest and Go test commands to pass without artificial tests
2. WHEN future tests are proposed
   THE SYSTEM SHALL require a named public contract, invariant, security boundary, integration path, migration guarantee, or reproduced defect
3. WHEN validation policy is inspected
   THE SYSTEM SHALL NOT require a coverage percentage or tests of trivial implementation details

## 6. Continuous integration

**User story:** As a contributor, I want CI to run the same fundamental checks as local development so that failures are reproducible.

### Acceptance criteria

1. WHEN a pull request or main-branch push occurs
   THE SYSTEM SHALL install the pinned toolchains and dependencies and run the documented check and build commands
2. WHEN third-party GitHub Actions are used
   THE SYSTEM SHALL pin them to reviewed commit hashes
