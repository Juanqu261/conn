# Tasks

- [x] 1. Record accepted foundation decisions
  - Record Go and Node.js, monorepo, minimal testing, Biome and Vitest, pnpm root scripts, and exact toolchain versions.

- [x] 2. Add root repository metadata
  - Add version files, workspace manifests, formatting configuration, TypeScript configuration, ignore rules, and root scripts.

- [x] 3. Add the minimal Go shell
  - Create the Go module and development CLI entry point.
  - Document Go-side component boundaries without implementing behavior.

- [x] 4. Add the minimal TypeScript shell
  - Create the TypeScript analyzer package and build configuration.
  - Do not add artificial tests for the capability descriptor.

- [x] 5. Document remaining component boundaries
  - Add responsibility and deferral documents for web, contracts, fixtures, and Kiro integration.

- [x] 6. Add contributor and security guidance
  - Document prerequisites, root commands, minimal testing policy, secrets, and vulnerability reporting.

- [x] 7. Add pinned continuous integration
  - Configure GitHub Actions with exact toolchains and commit-pinned third-party actions.

- [x] 8. Validate the scaffold
  - Install dependencies and generate the lockfile.
  - Run formatting, checks, tests, builds, and the CLI shell with the pinned production toolchains.
