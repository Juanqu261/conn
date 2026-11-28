# Design

## Purpose and scope

Conn's collision warnings are only as broad as its analyzers. Today that is TypeScript, through the compiler API. Writing and maintaining an analyzer per language is not a credible path. SCIP is: it is an established, language-neutral index format with maintained indexers, and it records exactly what Conn needs, namely which symbols each file defines and references, with ranges.

This milestone adds a SCIP claim source beside the TypeScript analyzer. It keeps the analyzer boundary from decision 0003 (credential-free subprocess, versioned JSON, bounded output) and the report contract from decision 0006, so the backend, the assessor, and every client are unchanged.

Not in scope: links across a language boundary (HTTP, RPC, shared database tables). SCIP cannot see those. They need claims from contract files and are deferred.

## Dependencies

- `pre-edit-collision-warnings`: planned and observed modes, `suggest_claims`, `analyze_intent`, the read-only Git rules, and the `local-analyzer-report` v2 contract.
- `review-proof`: the check runner rules (argument arrays, no shell, `CONN_` stripping, timeouts, log cap), which indexer commands reuse.
- `verification-ci-and-adoption`: temporary worktree handling from `verify-pair`, reused for `conn index --base`.

## Architecture decision gate

Decision 0012 must be accepted before implementation. It amends 0003 and 0006.

1. **How Conn reads SCIP.**

   | Option                                                                          | Advantages                                                                                           | Disadvantages                                                                                                | Operational cost              |
   | ------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------ | ----------------------------- |
   | Official Go bindings `github.com/scip-code/scip/bindings/go/scip` (recommended) | Streaming parse with bounded memory; maintained with the format; Apache-2.0; no extra tool for users | Adds the bindings module and `google.golang.org/protobuf` to `go.mod`                                        | Dependency updates            |
   | `scip print --json` subprocess                                                  | No new Go dependency                                                                                 | Users install another tool; whole-index JSON is very large and not streamed                                  | Tool installation per machine |
   | Parse in the Node analyzer package with a JavaScript protobuf library           | Keeps all analysis in Node                                                                           | Adds a JavaScript dependency; moves non-TypeScript work into the TypeScript package; slower on large indexes | Dependency updates            |

   Recommendation: the Go bindings.
2. **Process boundary.** Recommendation: the parent `conn` starts itself as `conn analyzer scip` with a minimized environment (no `CONN_` variables, no session token, no credential-store access) and exchanges versioned JSON. This matches the TypeScript analyzer's boundary and costs one process start. In-process parsing was rejected because it would put untrusted-file parsing in the process that holds the session token.
3. **Conn does not bundle or install indexers.** Repositories declare them in `.conn/scip.json`. Indexing runs only from the CLI, because indexers can execute build logic (for example, `scip-java` runs Gradle or Maven). It never runs from MCP, hooks, or CI.
4. **TypeScript stays on the TypeScript analyzer.** Its claims are richer (import resolution, export analysis), and keeping one source per language prevents duplicate subjects.

## SCIP facts this design relies on

Confirmed from `scip.proto` and the indexer READMEs:

- `Index { metadata, documents, external_symbols }`. Metadata comes first in the stream and includes `project_root` (a URI) and `tool_info { name, version }`.
- `Document { relative_path, occurrences, symbols, language, text, position_encoding }`. Indexers are not expected to include `text`. `language` is a free string.
- `Occurrence { symbol, symbol_roles, typed_range (single_line_range | multi_line_range), typed_enclosing_range, ... }`. The deprecated `range` and `enclosing_range` are 3 or 4 int32s. Lines and characters are 0-based and half-open. Typed ranges take precedence when both are set.
- `SymbolRole` bit flags: `Definition = 0x1`, `Import = 0x2`, `WriteAccess = 0x4`, `ReadAccess = 0x8`, `Generated = 0x10`, `Test = 0x20`, `ForwardDefinition = 0x40`.
- Symbol grammar: `<scheme> ' ' <manager> ' ' <package-name> ' ' <version> ' ' <descriptor>+`, or `local <id>`. Descriptor suffixes: namespace `/`, type `#`, term `.`, meta `:`, macro `!`, method `name(disambiguator).`, type parameter `[name]`, parameter `(name)`. Names that are not simple identifiers are backtick-escaped.
- `scip-go`: `go install github.com/scip-code/scip-go/cmd/scip-go@latest`, run from the module root. `scip-python index . --project-name=<name>`. `scip-java index` at a Gradle, Maven, or Bazel root writes `index.scip`. `scip-typescript index`.

Not confirmed, and resolved by the spike in task 3:

- The Go bindings' streaming API (`ParseStreaming`, `IndexVisitor`) and symbol parser signatures.
- Which indexers emit enclosing ranges for definitions.
- The default output file name for `scip-go` and `scip-typescript`.
- The exact `language` strings each indexer writes.

## Configuration `.conn/scip.json`

```json
{
  "version": 1,
  "indexers": {
    "backend-go": {
      "command": ["scip-go", "--output", "index.scip"],
      "working_directory": "services/backend",
      "output": "services/backend/index.scip",
      "timeout_seconds": 900
    },
    "worker-python": {
      "command": ["scip-python", "index", ".", "--project-name=worker"],
      "working_directory": "services/worker",
      "output": "services/worker/index.scip",
      "timeout_seconds": 900
    }
  }
}
```

Schema `contracts/v1/scip-config.schema.json`. Rules match `checks-config`: keys are slugs, the command is an argument array of at most 64 items, `working_directory` and `output` must stay inside the repository, `env` holds literal values and no `CONN_` name. The output file should be ignored by Git; `conn doctor` warns when it is not.

## `conn index`

1. Load and validate `.conn/scip.json` from the working tree. This is the developer's own configuration on their own machine, as for `conn evidence run`.
2. For each selected indexer, run it through the shared check runner (no shell, stripped environment, timeout, output streamed, first 10 MiB kept in `<git dir>/conn/runs/`).
3. On exit 0, open the output with the bindings and read only the metadata and document paths. Reject the index if `project_root` does not resolve to the indexer's working directory, or any `relative_path` escapes the repository.
4. Copy the index to `<git dir>/conn/scip/<commit>/<key>.scip` and write `<key>.json`:

   ```json
   {
     "metadata_version": 1,
     "key": "backend-go",
     "commit": "<HEAD>",
     "worktree_dirty": true,
     "config_sha256": "<hex>",
     "tool": { "name": "scip-go", "version": "0.x" },
     "created_at": "...",
     "document_root": "services/backend",
     "documents": { "services/backend/payments/retry.go": "<sha256 of the file>" }
   }
   ```

   Document paths are stored repository-relative (index path joined with the working directory). Hashing is bounded to 50,000 documents. Beyond that the index is accepted without freshness data, and every file is treated as stale-unknown.
5. Keep the five newest commits per key and delete the rest.
6. `--base`: create a temporary worktree at the focused Intent's base revision, run the same indexers there, cache under the base commit, and remove the worktree on success, failure, or cancellation.

## Analyzer subprocess `conn analyzer scip`

Hidden subcommand. Request on standard input, `scip-analyzer-request` version 1:

```json
{
  "request_version": 1,
  "mode": "planned",
  "worktree": "<absolute path>",
  "canonical_remote": "github.com/owner/name",
  "indexes": [
    {
      "key": "backend-go",
      "head": "<path to cached .scip>",
      "head_metadata": "<path to .json>",
      "base": null,
      "base_metadata": null
    }
  ],
  "planned_paths": ["services/backend/payments/retry.go"],
  "base_revision_ref": null
}
```

In observed mode, `planned_paths` is absent and `base_revision_ref` is required. The subprocess runs `git diff --no-color --unified=0 --no-renames <base> --` with fixed arguments, no shell, a timeout, and a minimized environment, as the TypeScript analyzer does under decision 0006, and parses hunk headers into changed line ranges per file.

Response on standard output: `local-analyzer-report` version 2 with mode `planned` or `observed`. The contract gains one optional property, `sources`: an array of `{ analyzer: "scip", key, tool_name, tool_version, commit, stale_paths, truncated }`. That records where claims came from for diagnostics and the review packet's integrity notes.

### Reading an index with bounded memory

Two streaming passes per index, never materializing the whole index:

1. **Target pass.** For each document in the target set (planned paths, or changed files), skipping TypeScript and JavaScript documents:
   - Freshness: compare the file's current SHA-256 with the metadata. Mark mismatches stale and skip them.
   - Collect non-local definitions (`symbol_roles & Definition`), excluding `ForwardDefinition`-only entries, with their range and enclosing range. These form set D.
   - Collect non-local references (no Definition bit) with their line. These form set R.
2. **Index pass.** Over all documents:
   - For each symbol in D, record whether any _other_ document references it. That makes it a contract.
   - For each symbol in R, record the document that defines it. A symbol with no defining document in the index is external and is dropped.

Bounds: index file 2 GiB, documents scanned 200,000, D and R together 100,000 symbols, and 500 claims out. Exceeding a bound sets `truncated` and keeps what was collected in deterministic order.

### Claim rules

Planned mode, per target document:

- D → `code_symbol` `changes`.
- D symbols referenced from another document → also `contract` `changes`.
- R symbols defined in another repository document → `contract` `consumes`, with the defining document's subject path.

Observed mode, per changed document:

- A definition is changed when its enclosing range intersects a changed range. Without an enclosing range, each changed line is attributed to the nearest preceding top-level definition in the same document. Changed definitions follow the planned-mode rules.
- References on changed lines → `consumes` as above.
- With a fresh base index: definitions in the base document missing from the head document → `removes` (symbol and contract). Contract definitions in the head missing at the base → `produces` instead of `changes`.
- Without a base index, deletions are not reported, and `sources[].truncated` stays false while a `base_unavailable` note is added.

Evidence references: `source_location` with the repository-relative path, the 1-based line of the definition or reference, and the symbol's descriptor chain as the locator. No source text.

### Subject normalization

For a symbol defined in repository document `p`:

1. Parse the symbol with the bindings' parser. Local symbols are never claimed.
2. Name: descriptors in order, keeping type (`#`), term (`.`), method (`(...)` → name only), meta (`:`), and macro (`!`) names. Dropping namespace descriptors (the document path already locates the symbol), parameters, and type parameters. Joining with `.`. So Go `Client#Retry().` becomes `Client.Retry`.
3. Apply the TypeScript analyzer's `stableName` (characters outside `[A-Za-z0-9._~-]` become `_`, truncated to 160) and `subjectPath` (same per path segment).
4. Language: `Document.language` lowercased with characters outside `[a-z0-9_-]` removed. Empty becomes `unknown`. A fixed alias table maps known variants (for example `golang` → `go`). The table is completed in task 3.
5. Subjects: `symbol:<language>:<subjectPath(p)>/<name>` and `contract:<language>:<subjectPath(p)>/<name>`, at most 512 characters. Longer subjects are dropped and counted in a truncation note.

Method overloads with different disambiguators share a subject. That is deliberately coarse: an extra warning is cheaper than a missed collision.

## Integration with existing analysis

`internal/preedit` and the observed-analysis path split target paths by extension. TypeScript sources go to the TypeScript analyzer, and every other path goes to `conn analyzer scip` when a cached index for HEAD covers it. Both reports are merged: deduplicate by `(normalized_subject, relation)`, sort by the existing relation priority and then subject, and keep 500. `suggest_claims`, `analyze_intent`, `conn start`, and `conn intent analyze` need no interface change. Their summaries gain a line per uncovered or stale path.

The analyzer `diagnostics` mode used by `verify-pair` stays TypeScript-only. SCIP indexes rarely carry compiler diagnostics.

## Contracts summary

- New: `scip-config`, `scip-analyzer-request`, and the index metadata schema (`scip-index-metadata`).
- Changed: `local-analyzer-report` gains optional `sources`. That is backward-compatible for readers; Go types are updated.
- No backend route, migration, or event changes.

## Security and privacy

- Indexers run only when a human or agent explicitly runs `conn index` from a shell, under the same command rules as checks. MCP tools, hooks, and CI never index.
- The analyzer subprocess has no Conn credential and a minimized environment. It reads only cached index files and runs only read-only Git.
- Index files never leave the machine. Claims carry paths, lines, and descriptor names, the same class of data the TypeScript analyzer already sends.
- `Document.text`, documentation strings, and signature documentation are never read into claims.

## Error behavior

- No `.conn/scip.json`: SCIP analysis is silently absent. `conn doctor` mentions it when non-TypeScript files are planned.
- Indexer not on `PATH`: `conn index` names the indexer key and its program, and continues with the others.
- Index older than HEAD: treated as not fresh for planned and observed analysis. The summary says to run `conn index`.
- Malformed index: the subprocess reports a diagnostic for that index and returns claims from the others.

## Validation plan

- Synthetic index tests built with the bindings in Go (parameterized): descriptor-to-name normalization, locals and externals, cross-document contracts, missing enclosing ranges, stale hashes, bounds and truncation, order independence (shuffled documents give identical reports).
- `fixtures/scip/go-sample/`: a small Go module with two packages and a committed `index.scip` generated by a pinned `scip-go`, plus `regenerate.sh` and `regenerate.ps1`. Tests cover planned claims and observed claims against a fixture diff.
- One backend scenario: two Intents whose planned paths include the same Go file and function open a collision with `symbol:go:...` subjects, and an unrelated function in the same file does not collide on the symbol claim.
- `conn index` test with a fake indexer program that writes a synthetic index, covering environment stripping, rejection of an escaping document path, metadata, and retention.
- Real trial.

## Deferred

- Contract-file claim sources (OpenAPI with `oasdiff`, protobuf with `buf breaking`, database schemas with Atlas) for collisions across language boundaries.
- SCIP indexing in CI.
- Using SCIP for TypeScript when the TypeScript analyzer is unavailable.
- Remote or shared index caches.
