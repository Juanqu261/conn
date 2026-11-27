# 0008: Verification, CI, and adoption

- Status: accepted. Item 4 records the option the `verification-ci-and-adoption` design assumed; the owner confirms it before the real-repository trial.
- Owner: project owner
- Amends: [0002](0002-runtime-and-interaction-surfaces.md) (CI add-on, other MCP clients), [0003](0003-local-typescript-analysis.md) (analyzer `diagnostics` mode), [0005](0005-collision-warnings-and-review-proof-core.md) (CI credential scope and distribution)
- Replaces for this version: the GitHub App sequence in the `semantic-intent-radar` design

## Context

Milestone 3 of decision 0005 must prove whether two collided changes break each other, show Conn results on every pull request through a CI add-on that Conn installs, and lower the cost of adopting Conn outside Kiro. Decision 0005 accepted a project-scoped CI credential in principle and left its scope, creation surface, and distribution open.

The alternatives were a GitHub App, CI writes to Conn, failing pull requests on findings, a hosted pair-verification runner, and a hosted analyzer. They needed repository authorization infrastructure, turned an advisory warning into a gate, or moved source and command execution off the developer's machine before the protocol was validated.

## Decision

1. The CI add-on is a GitHub Actions workflow that Conn renders from an embedded template and installs into the user's repository at `.github/workflows/conn-check.yml`, from `conn ci install` or the `install_ci_check` MCP tool. Installation refuses to overwrite different content, and removal deletes only byte-identical Conn content. The installer never creates credentials, sets secrets or variables, commits, pushes, or calls GitHub.
2. The CI credential is created by a Project owner only through `conn ci credential create`, a human-direct surface. It is bound to one Project and one RepositoryConnection, reads through an explicit route allowlist plus the collision preview, expires after 1 to 365 days (90 by default), is revocable, is stored only as a SHA-256 hash, and has the scannable prefix `conn_ci_`. The token reaches `gh secret set` through standard input with `--github-secret`, or standard output with `--print`; without either the command refuses. The backend rejects creation from the MCP adapter and from a CI credential.
3. CI reports findings and never fails a pull request for them. `conn ci check` exits 0 for every finding and every Conn, network, or configuration error, and CI writes nothing to Conn.
4. CI distribution builds `conn` and the analyzer from the pinned Conn source in the workflow, with the repository's pinned toolchains and dependency caching. It needs no release pipeline and costs one to two minutes per run. The step is isolated so that release artifacts or a container image can replace it later.

   | Option                                                  | Advantages                       | Disadvantages                                            | Operational cost            |
   | ------------------------------------------------------- | -------------------------------- | -------------------------------------------------------- | --------------------------- |
   | Build from pinned source (selected)                     | No release infrastructure; exact | One to two minutes per run; Conn source must be readable | None beyond Actions minutes |
   | Release artifacts (GitHub Release binary, npm analyzer) | Fast                             | Needs a release pipeline, signing, and versioning        | Release maintenance         |
   | Container image                                         | Reproducible                     | Needs a registry and image maintenance                   | Registry and rebuilds       |

5. `conn verify-pair` runs code from both branches of a collision on the developer's machine only after explicit confirmation or `--yes`. It reads the pair commands from `.conn/checks.json` on the repository default branch, never from either feature branch, runs them through the `review-proof` check runner, and records an advisory, append-only combination check. A combination check never changes a collision state. The analyzer gains a `diagnostics` mode that returns aggregated `(code, path, count)` compiler diagnostics for every discovered program and no claims. Its reports are never attached to an Intent.
6. GitHub issue import calls the GitHub REST API from the CLI, with `GH_TOKEN` or `GITHUB_TOKEN` only when one is set. The token stays in the local process: it is never persisted, logged, or sent to Conn. Imports record source kind `github_issue` with the issue update time and a content hash.
7. Project insights are Project-level only. Their contract has no property that can hold a Human, Participant, or login, the route accepts no grouping parameter, and every ratio whose denominator is below 5 reports insufficient data.
8. MCP clients other than Kiro are attributed with participant adapter kind `mcp`, detected from `clientInfo.name`. Attribution changes and authority does not. Conn still never writes MCP client configuration: `conn mcp config` only prints it, and `conn agents install` manages one marker-delimited section of `AGENTS.md` from the same source as the Kiro steering.

## Consequences

- Reviewers see Conn warnings, unregistered work, and review packets on every pull request without asking the author, and a failing CI step never comes from a Conn finding.
- The CI principal is a second authentication path. Its allowlist is enforced in one middleware and covered by a route-parameterized test.
- The default workflow needs a Conn backend reachable from GitHub-hosted runners over HTTPS and a readable Conn source repository. Teams with a private backend use a self-hosted runner.
- Pair verification executes teammates' code locally. The confirmation, the default-branch configuration, and the stripped `CONN_` environment are the safety boundary; the combination check is labelled as run on the developer's machine.
- Client detection depends on `clientInfo.name`. A client that reports no name keeps the `kiro` attribution until the real trial confirms Kiro's reported names.

## Validation

A PostgreSQL scenario proves that a collision preview equals the persisted assessor result and writes nothing. A Go scenario on a temporary Git repository covers a failing interaction, a merge conflict, default-branch configuration, environment stripping, and cleanup after cancellation. A PostgreSQL and HTTP scenario parameterized over routes covers the CI allowlist, Project binding, expiry, revocation, hashing, and audit events. A Go scenario with a fake backend covers `conn ci check`. Template and installer tests cover workflow safety and the `AGENTS.md` lifecycle. A fake GitHub server covers issue import, and a PostgreSQL scenario covers insights suppression. The add-on is then demonstrated on a real GitHub repository, including a fork pull request, and the sanitized results are recorded in `docs/real-trial.md`.
