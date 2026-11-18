# Conn

Coordination layer for human and AI software teams.

Conn is intended to help teams turn a software objective into a versioned delivery blueprint, divide responsibility into accountable human-agent work cells, and verify that independently produced parts still fit together. It is a shared coordination protocol and evidence layer, not an agent launcher or central agent orchestrator.

## Development scaffold

Prerequisites and contribution rules are documented in [CONTRIBUTING.md](CONTRIBUTING.md).

# To try the CLI:

```bash
pnpm db:start
pnpm db:migrate
go run ./cmd/conn server        # terminal 1

go run ./cmd/conn login         # terminal 2
go run ./cmd/conn whoami
go run ./cmd/conn doctor
go run ./cmd/conn logout
```
