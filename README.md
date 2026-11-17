# Conn

Coordination layer for human and AI software teams.

Conn is intended to help teams turn a software objective into a versioned delivery blueprint, divide responsibility into accountable human-agent work cells, and verify that independently produced parts still fit together. It is a shared coordination protocol and evidence layer, not an agent launcher or central agent orchestrator.

## Project status

Planning and mechanical scaffolding are complete. The MVP will test three accountable human-agent work cells using Kiro only; other coding-agent integrations are deferred. Go 1.27.4 and Node.js 26.13.1 LTS share a pnpm-workspace monorepo. Frameworks, contract technology, persistence libraries, deployment, and component process boundaries remain undecided.

## Development scaffold

Prerequisites and contribution rules are documented in [CONTRIBUTING.md](CONTRIBUTING.md).

```text
pnpm install --frozen-lockfile
pnpm check
pnpm build
go run ./cmd/conn
```

The scaffold intentionally contains no artificial tests. Vitest and `go test` are configured for fundamental behavior as product implementation begins.
