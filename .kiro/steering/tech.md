---

## inclusion: always

# Accepted technical direction

- Conn uses Go and Node.js with TypeScript.
- Go is the default for local and service-oriented components.
- Node.js with TypeScript is the default for TypeScript-specific analysis and browser-facing development.
- Conn uses one monorepo with language-neutral, versioned contracts between Go and TypeScript components.
- Use Go 1.27.4, Node.js 26.13.1 LTS, and pnpm 12.21.0.
- Use pnpm workspaces and root package scripts without a separate task runner.
- Use JSON Schema Draft 2020-12 as the canonical, language-neutral protocol contract format.
- Validate schemas with Ajv 8.20.0 in Node.js and `santhosh-tekuri/jsonschema/v6` 6.0.3 in Go.
- Defer model generation until the first artifact schemas and compatibility rules stabilize.
- Use Biome for JavaScript, TypeScript, and JSON formatting and linting.
- Use Vitest for fundamental Node.js and TypeScript behavior tests.
- Start with `gofmt`, `go vet`, and `go test` for Go.

## Testing constraint

Keep the automated suite as small as practical. Test public contracts and fundamental invariants rather than implementation details. Prefer parameterized tests, avoid duplicate assertions across layers, and do not add a coverage-percentage gate during the MVP.

The detailed policy is `docs/decisions/0003-minimal-fundamental-testing.md`.

Root task orchestration uses pnpm workspace scripts. Frameworks, model generation, persistence libraries, deployment, and exact component process boundaries remain undecided. Ask before selecting them and record each accepted choice.
