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

## Deferred

- Schema registry and reference loading
- Normalized Conn validation errors
- Generated TypeScript models
- First domain schema and shared fixtures
