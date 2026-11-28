# Requirements

Milestone 7. Acceptance goal: Conn opens pre-edit and observed collisions for code in languages other than TypeScript, by reading SCIP indexes that standard indexers already produce, instead of Conn writing an analyzer per language.

SCIP (`scip-code/scip`, Apache-2.0) is a language-neutral code-intelligence index format. Maintained indexers exist for Go (`scip-go`), Python (`scip-python`), Java and Kotlin (`scip-java`), TypeScript and JavaScript (`scip-typescript`), and others. Each index records the definitions and references of every symbol with source ranges. Conn turns those into the same normalized claims the TypeScript analyzer produces today.

Scope boundary: SCIP links symbols within a language's program. It does not link a TypeScript HTTP client to the Go handler it calls. Collisions across a language boundary need claims from a shared contract file (OpenAPI, protobuf, database schema) and are deferred to a later milestone.

This milestone depends on `pre-edit-collision-warnings` (planned and observed analysis, `suggest_claims`, `analyze_intent`) and `review-proof` (the check runner rules, which indexer commands reuse).

## 1. Indexer configuration

**User story:** As a maintainer, I want to declare once how my repository is indexed, so that every teammate's Conn uses the same indexers.

### Acceptance criteria

1. THE SYSTEM SHALL read a committed `.conn/scip.json` with version 1 and up to 10 indexers
2. EACH indexer SHALL have a key, a command argument array, a working directory inside the repository, a timeout of 1 to 3,600 seconds, the repository-relative path of the index file the command writes, and optional literal environment values that name no `CONN_` variable
3. THE SYSTEM SHALL validate the file against a versioned JSON Schema and SHALL report every violation with its location

## 2. Local indexing

**User story:** As a developer, I want to index my working tree on demand so that Conn's analysis reflects my code.

### Acceptance criteria

1. WHEN a human or agent runs `conn index [--key <key>]` from a shell
   THE SYSTEM SHALL run each selected indexer without a shell, without `CONN_` variables, and with its timeout, streaming its output
2. WHEN an indexer succeeds
   THE SYSTEM SHALL copy its index into `<git dir>/conn/scip/` with metadata: the commit, the worktree dirty flag, the configuration hash, the indexer's tool name and version from the index, the time, and the SHA-256 of each indexed document file
3. THE SYSTEM SHALL keep cached indexes for at most five commits per indexer and SHALL delete older ones
4. WHEN `--base` is passed
   THE SYSTEM SHALL also index the focused Intent's base revision in a temporary worktree and SHALL remove the worktree afterward
5. THE SYSTEM SHALL never run an indexer from the MCP adapter, a hook, or CI
6. THE SYSTEM SHALL reject an index whose project root is not the indexed worktree, or that names a document outside it

## 3. Planned claims for SCIP languages

**User story:** As a developer planning a change to a Go, Python, or Java file, I want the same pre-edit claim suggestions TypeScript files get.

### Acceptance criteria

1. WHEN `suggest_claims` or `conn start` receives a planned path that a fresh cached index covers and that is not a TypeScript or JavaScript file
   THE SYSTEM SHALL suggest a `changes` `code_symbol` claim for each non-local symbol defined in the file
2. THE SYSTEM SHALL also suggest a `changes` `contract` claim for each such symbol that another document in the index references
3. THE SYSTEM SHALL suggest a `consumes` `contract` claim for each non-local symbol the file references that another document in the same index defines
4. THE SYSTEM SHALL produce no claim for symbols outside the repository, such as standard-library or dependency symbols
5. WHEN a planned path has no fresh index
   THE SYSTEM SHALL say which indexer covers its language and that `conn index` refreshes it, and SHALL still return the other candidates

## 4. Observed claims for SCIP languages

**User story:** As a developer finishing a change, I want observed analysis to report what my diff touched in every indexed language.

### Acceptance criteria

1. WHEN observed analysis runs for an Intent
   THE SYSTEM SHALL compute the changed line ranges between the Intent base revision and the working tree with read-only Git
2. THE SYSTEM SHALL report `changes` claims for definitions whose enclosing range intersects a changed range, or, when the indexer gives no enclosing range, for the nearest preceding definition in the same document
3. THE SYSTEM SHALL report `consumes` claims for references on changed lines to symbols another document defines
4. WHEN a fresh index of the base revision is cached
   THE SYSTEM SHALL report `removes` for definitions present at the base and absent from the working tree, and `produces` for contract definitions absent at the base
5. WHEN a changed file's current hash differs from the hash recorded with the index
   THE SYSTEM SHALL skip that file's claims and report it as stale rather than guess

## 5. Stable subjects

**User story:** As a team, we want the same symbol to get the same subject on every machine, so that our claims collide.

### Acceptance criteria

1. THE SYSTEM SHALL name a symbol `symbol:<language>:<path>/<name>` and its contract `contract:<language>:<path>/<name>`, where the path is the repository-relative document that defines it and the name is its descriptor chain without namespaces, parameters, type parameters, or method disambiguators
2. THE SYSTEM SHALL map paths and names onto the semantic-subject alphabet with the same rules as the TypeScript analyzer
3. THE SYSTEM SHALL derive the language from the document's language, lowercased and normalized
4. THE SYSTEM SHALL produce identical subjects for identical source on any machine, independent of indexer output order
5. THE SYSTEM SHALL keep TypeScript and JavaScript files on the TypeScript analyzer and SHALL ignore those documents in SCIP indexes

## 6. Credential-free, bounded analysis

### Acceptance criteria

1. THE SYSTEM SHALL read SCIP indexes in a subprocess of the `conn` executable that receives no Conn credential and a minimized environment, and that exchanges versioned JSON over standard input and output
2. THE SYSTEM SHALL return the existing `local-analyzer-report` shape, so that Conn's backend and collision assessor do not change
3. THE SYSTEM SHALL read only the documents it needs, SHALL never read or forward `Document.text`, signature documentation, or hover documentation, and SHALL send Conn only claims and location references
4. THE SYSTEM SHALL bound index size, document count, symbols held in memory, and report size, and SHALL report a truncation rather than fail
5. THE SYSTEM SHALL merge SCIP and TypeScript claims deterministically, keeping at most 500 claims with the existing relation priority

## 7. Diagnostics

### Acceptance criteria

1. WHEN `conn doctor` or `session_status` runs
   THE SYSTEM SHALL report each configured indexer, whether its program is on `PATH`, and each cached index with its commit, tool version, and freshness
2. THE SYSTEM SHALL report, per analyzed path, whether its claims came from the TypeScript analyzer, a SCIP index, or no analyzer

## 8. Milestone validation

### Acceptance criteria

1. Tests SHALL cover synthetic indexes for subject normalization, cross-document contracts, local and external symbols, missing enclosing ranges, stale files, bounds, and order independence
2. A committed index generated by `scip-go` from a fixture Go module SHALL prove planned and observed claims for real indexer output, with a script that regenerates it
3. A Go scenario with the backend SHALL prove that two Intents changing the same Go function open a collision through SCIP claims
4. A real trial on a repository with Go or Python code SHALL record the pre-edit warning and the observed claims in `docs/real-trial.md`
