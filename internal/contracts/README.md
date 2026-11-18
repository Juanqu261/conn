# Contract validation for Go

## Responsibility

Compile and validate Conn's canonical JSON Schema Draft 2020-12 contracts in Go components.

## Dependency

`github.com/santhosh-tekuri/jsonschema/v6` provides runtime validation. The adapter defaults to Draft 2020-12 and refuses unresolved external references rather than fetching schemas over the network.

## Boundary

- JSON Schema remains the source of truth.
- Parsed values remain inside this validation boundary until typed models are selected.
- Validation does not perform authorization or lifecycle checks.
- Model generation is intentionally deferred.

## Usage

`Load(contracts.V1, "v1/*.schema.json")` compiles every embedded v1 schema. `ValidateJSON` returns `*ValidationError` with leaf violations as JSON Pointer and message pairs, without exposing validator library types.

## Deferred

- Normalized Conn validation error contract
- Generated Go models
