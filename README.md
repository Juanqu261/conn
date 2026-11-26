# Conn

Conn turns a software objective into a versioned team blueprint, divides accountability into human-agent work cells, and records evidence that independently produced work still fits together.

Conn coordinates work. It does not launch agents, supervise people, score productivity, or let an agent make a binding human decision.

## Implemented MVP

- GitHub Device Flow login with Conn sessions stored in the operating-system credential store.
- Projects with owner/member authorization, versioned Objectives, immutable Blueprint revisions, and human-accepted Cell Charters.
- Participant-owned Intents, declared and locally observed semantic claims, deterministic advisory collisions, non-binding proposals, human decisions, and reference-first evidence.
- One Go executable with CLI, `conn app`, `conn server`, and a Kiro stdio MCP adapter.
- A local TypeScript analyzer subprocess that receives no Conn credential and returns bounded, source-free JSON.
- Kiro session diagnostics, login/logout, Project selection, focused work context, workflow prompts, optional resources, local analysis, and generated handoffs.

The MVP requires the authoritative backend. It has no offline outbox, background polling, idle MCP push, or production web application.

## Local development

Requirements: Go 1.27.4, Node.js 26.13.1, pnpm 12.21.0, Docker, and PostgreSQL 18.7 through the supplied Compose file.

```powershell
pnpm install --frozen-lockfile
pnpm db:start
pnpm db:migrate
pnpm build
go run ./cmd/conn server
```

Set `CONN_GITHUB_CLIENT_ID` to a GitHub OAuth application with Device Flow enabled. Normal database commands stop the container without deleting its named volume.

Human-authoritative work is available through the CLI and terminal application:

```powershell
go run ./cmd/conn login
go run ./cmd/conn app
```

## One-time Kiro setup

Build an absolute executable path, then explicitly add the local stdio server from the workspace that should use Conn:

```powershell
go build -o conn.exe ./cmd/conn
$conn = (Resolve-Path .\conn.exe).Path

kiro-cli mcp add `
  --name conn `
  --scope workspace `
  --command $conn `
  --env "CONN_SERVER_URL=http://127.0.0.1:8080" `
  --args mcp

kiro-cli mcp list workspace
```

This Kiro command edits `.kiro/settings/mcp.json`; Conn itself never edits MCP configuration. Use `/mcp` inside Kiro to check startup and `/tools` to inspect tools. Remove the entry with:

```powershell
kiro-cli mcp remove --name conn --scope workspace
```

The optional steering and lifecycle-hook bundle is separate from MCP setup:

```powershell
conn kiro integration preview --scope workspace --root .
conn kiro integration install --scope workspace --root .
# Later: conn kiro integration remove --scope workspace --root .
```

Preview performs no writes. Installation refuses to overwrite different content and preserves unrelated Kiro files.

## Daily Kiro workflow

Use the normal Kiro agent. A custom agent is not required.

- `/conn-start`: authenticate if needed, select a Project, load exact context, publish or resume an Intent, and check collisions.
- `/conn-sync <cursor>`: pull committed events and reread affected context. Conn does not poll while idle.
- `/conn-resolve <collision-id>`: inspect source-free context and optionally record a non-binding proposal.
- `/conn-finish <intent-id>`: verify revisions, run eligible local analysis, recheck collisions, attach approved evidence references, and revise or release owned work.
- `/conn-handoff <intent-id>`: generate bounded JSON or Markdown without persisting a handoff record.

Project resolution uses explicit tool input, process-local selection, `CONN_PROJECT`, the sole visible Project, or an exact credential-free Git remote match, in that order. Ambiguity stops the workflow.

Interactive Kiro can run Device Flow through `login`. Headless sessions have no mid-session input; use an explicit non-persistent `CONN_TOKEN` and narrowly trusted tools. Do not use `--trust-all-tools` for normal Conn operation.

Read-only tools may be allowed after inspection. Keep login, logout, Intent writes, analysis, evidence, and proposal tools on `ask` unless a specific automation has a reviewed reason to allow them. Kiro permission allows a tool call; it is never a Charter, Objective, Blueprint, membership, risk, or binding resolution decision.

## Validation and design

```powershell
pnpm check
pnpm test
pnpm build
```

See [architecture](docs/architecture.md), [code structure](docs/code-structure.md), [three-session trial](docs/real-trial.md), and [accepted decisions](docs/decisions/README.md).
