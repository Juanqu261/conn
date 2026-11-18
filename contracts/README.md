# Contracts

## Responsibility

Hold the language-neutral source of truth for protocol objects shared by Go and TypeScript components.

## Accepted format

Canonical contracts use JSON Schema Draft 2020-12. Schemas must have stable `$id` values and should reject unknown properties for authoritative records unless a reviewed extensibility requirement says otherwise.

JSON and parsed YAML repository artifacts must validate against the same schemas. HTTP and MCP adapters consume these contracts rather than defining independent versions.

## Layout

Authoritative records:

```text
contracts/v1/project.schema.json                  urn:conn:schema:v1:project
contracts/v1/project-membership.schema.json       urn:conn:schema:v1:project-membership
contracts/v1/objective.schema.json                urn:conn:schema:v1:objective
contracts/v1/blueprint-revision.schema.json       urn:conn:schema:v1:blueprint-revision
contracts/v1/charter-decision.schema.json         urn:conn:schema:v1:charter-decision
contracts/v1/event-envelope.schema.json           urn:conn:schema:v1:event-envelope
```

Human-authored inputs, which omit every server-assigned field:

```text
contracts/v1/project-create.schema.json           urn:conn:schema:v1:project-create
contracts/v1/project-member-add.schema.json       urn:conn:schema:v1:project-member-add
contracts/v1/objective-revision-input.schema.json urn:conn:schema:v1:objective-revision-input
contracts/v1/blueprint-proposal.schema.json       urn:conn:schema:v1:blueprint-proposal
contracts/v1/charter-rejection.schema.json        urn:conn:schema:v1:charter-rejection
```

Identity and responses:

```text
contracts/v1/human.schema.json                    urn:conn:schema:v1:human
contracts/v1/session-grant.schema.json            urn:conn:schema:v1:session-grant
contracts/v1/github-login.schema.json             urn:conn:schema:v1:github-login
contracts/v1/error.schema.json                    urn:conn:schema:v1:error
```

Read models and shared definitions:

```text
contracts/v1/blueprint-revision-view.schema.json  urn:conn:schema:v1:blueprint-revision-view
contracts/v1/project-status.schema.json           urn:conn:schema:v1:project-status
contracts/v1/common.schema.json                   urn:conn:schema:v1:common ($defs only)
contracts/v1/fixtures/cases.json                  shared accepted and rejected fixtures
contracts/embed.go                                embeds v1 for Go components
```

Schemas reference each other only by `urn:conn:schema:v1:*` identifiers. Both validators register every file in `v1/` and refuse any other reference.

## Record rules

- Inputs carry client-generated UUIDv7 identities for safe retries. The server adds the Project from the request path, the actor from the session, revision numbers, and timestamps. It then validates the complete record against the record schema before storing it.
- A BlueprintRevision document never changes after proposal. Its lifecycle status is served beside it through `blueprint-revision-view`.
- Each event type fixes its `entity_type` and a closed payload shape. Payloads carry only identity, revision, and cell references. Membership events use entity type `project`, with the member in the payload.
- Size limits start tight because raising a limit later is compatible and lowering one is not.
## Conventions

- Identities are canonical lowercase UUIDv7 strings.
- Timestamps are RFC 3339 UTC strings ending in `Z` with at most microsecond precision, which PostgreSQL stores without loss. The pattern checks shape; the consuming parser checks calendar validity.
- Revisions are integers starting at 1. Event sequences are bounded to the JavaScript safe-integer range.
- Keys are lowercase slugs of at most 64 characters.
- Text fields must contain a non-whitespace character and have explicit maximum lengths counted in Unicode code points.
- Source URIs are absolute `http` or `https` URIs with non-ASCII characters percent-encoded.
- Schemas do not use the `format` keyword, because Ajv and the Go validator treat formats differently by default.

## Cross-language rules

- Keep schema and fixture files ASCII. Write other characters as `\u` escapes.
- Do not use `\s`, `\d`, `\w`, or `\b` in patterns. Go's RE2 engine matches only ASCII for these classes while Ajv matches Unicode. Write explicit character classes instead.
- Every schema change adds or adjusts a fixture in `fixtures/cases.json` that both validators run.

## Validation

- Node.js and TypeScript components use Ajv 8.20.0 through `packages/contracts`.
- Go components use `github.com/santhosh-tekuri/jsonschema/v6` 6.0.3 through `internal/contracts`.
- Both run the same `fixtures/cases.json` table. Each rejected case names the JSON Pointer where the failure must be reported, so a case cannot pass by failing for an unrelated reason.
- JSON Schema validation covers document structure, not authority or lifecycle correctness.

Rules enforced by application validation rather than schema:

- unique Objective key-point keys and unique charter boundary-point keys;
- unique `cell_id` values within one BlueprintRevision;
- charter Objective references that exist in the referenced Objective revision;
- dependency targets that exist in the same BlueprintRevision and are not the charter's own cell;
- project membership, authority, lifecycle, and optimistic concurrency.

## Model strategy

Go components use the handwritten types in `internal/protocol`, held to these schemas by a conformance test over the shared fixtures (decision 0033). TypeScript models stay deferred until a TypeScript component consumes protocol records. Untyped parsed values must remain within validation boundaries and must not spread into domain logic.

## Deferred

- Compatibility policy and checks
- Normalized Conn validation error contract
- TypeScript protocol models
- Contract, decision, and evidence schemas beyond the first slice

See decisions 0007, 0008, 0009, and 0033 under `docs/decisions/`.
