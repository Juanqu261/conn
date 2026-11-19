# Conn architecture

This document describes the implemented MVP. Conn coordinates human work around a versioned blueprint. It does not launch agents, assign work automatically, or let agents approve changes.

## System shape

```text
human ── conn CLI ──┐
                    ├── Conn HTTP API ── application rules ── PostgreSQL
Kiro ── conn mcp ───┘          │
                               └── GitHub Device Flow and identity check
```

`conn` is one Go executable. It provides the human CLI, the Kiro MCP adapter over standard input and output, and the backend service through `conn server`. The CLI and MCP adapter call the backend API. They do not access PostgreSQL directly.

For local development, PostgreSQL runs in Docker Compose and `conn server` listens on loopback at `http://127.0.0.1:8080`. The deployed topology is intended to use HTTPS between local Conn clients and the authoritative backend.

## Authority and lifecycle

A project is the authorization and event boundary. It has one owner and zero or more members. Owners create projects, add members, create objective revisions, and propose blueprint revisions. All people added to a project must first sign in and obtain a Conn human ID.

An Objective is an immutable versioned description of the goal. It contains a required Markdown overview and can contain keyed outcomes, acceptance criteria, invariants, constraints, and questions.

A BlueprintRevision is an immutable snapshot for one Objective revision. It contains every CellCharter in the proposed division of work. A charter assigns one accountable human and describes the cell through a required Markdown overview plus optional boundary points and dependencies.

```text
objective revision
       │
       ▼
blueprint proposed ── each accountable human accepts own charter ──► active
       │                                   │
       │                                   └── rejection ──► rejected
       │
       └── newer proposal leaves the current active revision in force
```

A charter decision is append-only and final for its blueprint revision. Only the human assigned to that charter can make it. When a new blueprint revision is proposed, the active revision stays in force until every assigned human accepts the new one. Rejected or superseded revisions cannot become active.

Objectives and blueprints use integer revisions for optimistic concurrency. A revise command includes the latest revision read by its caller. If that is stale, Conn returns conflict exit code `5` rather than silently overwriting a newer revision.

## Sign-in and sessions

`conn login` performs GitHub Device Flow directly from the CLI. The backend exposes its public GitHub client ID, receives the temporary GitHub token once, calls GitHub's `GET /user` endpoint to identify the person, then discards the GitHub token.

The backend returns an opaque Conn session token. The CLI stores it in the operating-system credential store, with one entry for each server URL. The stored token is a 256-bit secret whose SHA-256 hash is retained server-side. Sessions expire after 30 days and `conn logout` immediately revokes the current session.

`CONN_TOKEN` takes precedence over the credential store but is never persisted. Conn has no plaintext credential fallback. Credentials and request bodies are not logged.

## Write path

Structured CLI writes use a JSON file or standard input. The client validates the document against the canonical schema before sending it. The HTTP API authenticates the Conn session, validates the JSON document again, then passes the request to the application layer.

The application layer checks membership, owner authority, lifecycle state, and optimistic-concurrency expectations inside the same PostgreSQL transaction that records the operation. PostgreSQL migrations enforce the matching append-only, membership, revision, decision, and event invariants.

Successful project-scoped responses use this envelope:

```json
{"data": {}, "as_of_event": 42}
```

Each committed project change receives the next gap-free event cursor. `conn sync` retrieves committed events after a cursor, oldest first. Conn has no background polling timer in the MVP.

## Contracts and API

The versioned JSON Schema Draft 2020-12 files in [`contracts/v1`](../contracts/v1/) are the language-neutral protocol source of truth. They define human-authored inputs, authoritative records, views, errors, and event envelopes. Schemas reject unknown fields for authoritative records.

Go validates contracts with `santhosh-tekuri/jsonschema/v6`; Node and TypeScript use Ajv. Both validators run the same accepted and rejected fixture table. Handwritten Go protocol types are constrained by that same schema and fixture conformance test.

The backend is a Go `net/http` service under `/v1`. It exposes health/readiness checks, GitHub exchange and session operations, project/member operations, objective and blueprint revisions, charter decisions, status, and events. Request bodies are JSON, limited to 1 MiB, and response documents are validated before they are sent.

## Kiro integration

Kiro starts `conn mcp` as a stdio process. The adapter resolves the currently signed-in human for every call and uses the same backend client as the CLI. Standard output carries only MCP messages; diagnostics use standard error.

The adapter is deliberately read-only:

| Tool | Behavior |
| --- | --- |
| `read_objective` | Reads an Objective revision, defaulting to the only Objective and its latest revision. |
| `read_blueprint` | Reads a BlueprintRevision and status, defaulting to the active revision or latest proposal. |
| `read_my_charter` | Reads only the signed-in human's charters and their decisions. |
| `check_updates` | Reads project events after an event cursor. |

The project is supplied per tool call or by `CONN_PROJECT`. The adapter never accepts a human ID from a tool call, and it has no write, approval, or rejection tool.

## Deferred work

The MVP has no web application, agent-authored writes, additional coding-agent integrations, local SQLite outbox, persistent sidecar, background update polling, contract/evidence schemas beyond the first slice, or TypeScript protocol models. The TypeScript analyzer package is a buildable boundary only; it does not inspect a repository yet.

See [code structure](code-structure.md) for module ownership, [the real-trial guide](real-trial.md) for manual validation, and [accepted decisions](decisions/README.md) for the choices behind this architecture.
