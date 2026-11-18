# Protocol types

## Responsibility

Hold Conn's v1 protocol records, human-authored inputs, the blueprint view, and event payloads as Go types.

## Boundary

- `contracts/v1` JSON Schemas remain the source of truth. These types conform to them and never extend them.
- `Decode[T]` validates against T's schema before decoding and rejects unknown fields. `Encode` validates its output, so no non-conforming document leaves the process.
- Identities use `uuid.UUID`. `Timestamp` stores UTC with microsecond precision and rejects impossible dates.
- Each event payload type fixes its own `event_type` and `entity_type`; `EventEnvelope` derives both from its payload.
- No untyped JSON value leaves this package.

## Changing a schema

Change the schema, the matching Go type, and at least one valid fixture in `contracts/v1/fixtures/cases.json` together. `TestConformance` round-trips every fixture through these types and fails when they disagree.
