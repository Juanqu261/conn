# Contracts

## Responsibility

Hold the language-neutral source of truth for protocol objects shared by Go and TypeScript components.

## Accepted format

Canonical contracts use JSON Schema Draft 2020-12. Schemas must have stable `$id` values and should reject unknown properties for authoritative records unless a reviewed extensibility requirement says otherwise.

JSON and parsed YAML repository artifacts must validate against the same schemas. HTTP and MCP adapters consume these contracts rather than defining independent versions.

## Layout

```text
contracts/v1/common.schema.json              shared value definitions ($defs only)
contracts/v1/project.schema.json             urn:conn:schema:v1:project
contracts/v1/project-membership.schema.json  urn:conn:schema:v1:project-membership
contracts/v1/objective.schema.json           urn:conn:schema:v1:objective
contracts/v1/blueprint-revision.schema.json  urn:conn:schema:v1:blueprint-revision
contracts/v1/charter-decision.schema.json    urn:conn:schema:v1:charter-decision
contracts/v1/event-envelope.schema.json      urn:conn:schema:v1:event-envelope
contracts/v1/fixtures/cases.json             shared accepted and rejected fixtures
contracts/embed.go                           embeds v1 for Go components
```

Schemas reference each other only by `urn:conn:schema:v1:*` identifiers. Both validators register every file in `v1/` and refuse any other reference.

## Conventions

- Identities are canonical lowercase UUIDv7 strings.
- Timestamps are RFC 3339 UTC strings ending in `Z`. The pattern checks shape; the consuming parser checks calendar validity.
- Revisions are integers starting at 1. Event sequences are bounded to the JavaScript safe-integer range.
- Keys are lowercase slugs of at most 64 characters.
- Text fields must contain a non-whitespace character and have explicit maximum lengths.
- Schemas do not use the `format` keyword, because Ajv and the Go validator treat formats differently by default.

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

Model generation is deferred until the first artifact schemas and compatibility rules stabilize. Untyped parsed values must remain within validation boundaries and must not spread into domain logic.

## Deferred

- Compatibility policy and checks
- Normalized Conn validation error contract
- Typed event payload schemas per event type
- Model generation
- Contract, decision, and evidence schemas beyond the first slice

See decisions 0007, 0008, and 0009 under `docs/decisions/`.
