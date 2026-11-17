# Contract validation for Node.js

## Responsibility

Compile and validate Conn's canonical JSON Schema Draft 2020-12 contracts in Node.js and TypeScript components.

## Dependency

Ajv provides runtime schema validation. The adapter uses the Draft 2020-12 entry point in strict mode and does not configure network-based schema loading.

## Boundary

- JSON Schema remains the source of truth.
- Parsed values remain unknown outside an accepted schema and future typed-model boundary.
- Validation does not perform authorization or lifecycle checks.
- Model generation is intentionally deferred.

## Usage

`createRegistry(schemas)` compiles schemas that reference each other by `$id`. `validate(id, document)` returns either `{ valid: true }` or leaf violations as JSON Pointer and message pairs. `test/shared-fixtures.test.ts` runs the shared fixture table in `contracts/v1/fixtures`.

## Deferred

- Normalized Conn validation error contract
- Generated TypeScript models
