# Design

## Purpose and scope

Spec Kit (`github/spec-kit`, MIT, 1.4.5 at the time of writing) is a toolkit that gives many coding agents one spec-driven process. It supports Kiro CLI (`--integration kiro-cli`) and more than forty other agents, and it has an extension system with hooks around its commands. It does not model concurrent features.

This milestone adds three things and changes no product boundary:

1. A third Objective source, `speckit_feature`, beside `kiro_spec` and `github_issue`, so that `conn start` accepts a Spec Kit feature directory.
2. A deterministic path extractor for `tasks.md` and `plan.md`, so that a task list becomes planned paths for the existing `suggest_claims` and collision preview.
3. A Spec Kit extension package, rendered from embedded files, that calls Conn at `after_tasks`, `before_implement`, and `after_implement`. Conn exports the package; the human installs it with Spec Kit.

The result: the earliest Conn warning moves from "before the first write" to "when the plan is written".

## Dependencies

- `verification-ci-and-adoption`: `conn start`, `workstart` import and revision logic, the collision preview route, `AGENTS.md` guidance from `internal/agentguide`, and `mcp` participant attribution.
- `review-proof`: `.conn/checks.json` acceptance keys, `conn evidence run`, and the review packet.
- `pre-edit-collision-warnings`: `suggest_claims`, planned analysis, and the warn-only `conn hook` commands.

## Architecture decision gate

Decision 0010 must be accepted before implementation. It records:

1. **Spec Kit is an Objective source and an integration target, not a dependency.** Conn neither installs nor invokes Spec Kit and needs no Python or `uv`. Conn reads Spec Kit's Markdown files locally and ships an extension package.
2. **Distribution of the extension.**

   | Option                                                                      | Advantages                                                                                                    | Disadvantages                                                                            | Operational cost            |
   | --------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------- | --------------------------- |
   | `conn speckit extension export` plus Spec Kit's local install (recommended) | Conn never edits `.specify/`; matches "Conn never writes MCP configuration"; versioned with the `conn` binary | Two commands for the human                                                               | None                        |
   | Conn writes `.specify/extensions.yml` and command files directly            | One command                                                                                                   | Edits another tool's state file whose format may change; breaks the integration boundary | Tracking Spec Kit internals |
   | Publish to the Spec Kit community catalog                                   | `specify extension add conn`                                                                                  | Catalog review and release process; version skew with the local binary                   | Catalog maintenance         |

   Recommendation: export plus local install for this milestone; publish to the catalog after the real trial.
3. **Deterministic extraction.** Paths, requirement IDs, success criteria, and clarification markers are extracted by fixed rules in Go, not by the agent. Agents present results and ask; they do not decide which paths a plan touches.
4. **Agent lifecycle events** are adopted only if the spike in task 3 confirms that payloads are parseable. Otherwise the extension ships with hooks only.

## Spec Kit facts this design relies on

Confirmed from the Spec Kit documentation and templates:

- Feature directories are `specs/<NNN>-<name>/` with `spec.md`, `plan.md`, `research.md`, `data-model.md`, `quickstart.md`, `contracts/`, `tasks.md`, and `checklists/`. Numbers start at `001` and can exceed three digits.
- `spec.md` uses `**FR-001**: System MUST ...` for functional requirements, `**SC-001**: ...` for success criteria, user stories with Given/When/Then acceptance scenarios that have no IDs, and `[NEEDS CLARIFICATION: ...]` markers.
- `tasks.md` lines are `- [ ] T001 [P] [US1] Description with path/to/file.ext`. `[P]` marks parallelizable tasks and `[USn]` the user story.
- Checklist items (`CHK001`) review requirement quality, not implementation, so Conn does not import them.
- The extension manifest is `extension.yml` with `schema_version: "1.0"`, `extension`, `requires` (`speckit_version`, `tools`), `provides.commands` (names `speckit.<ext>.<cmd>`, each a Markdown file), `hooks`, and `events`.
- Hook events include `after_tasks`, `before_implement`, and `after_implement`. A hook entry has `command`, `optional`, `prompt`, `description`, `priority`, and `condition`. A hook runs an extension _agent command_, not a shell command. Optional hooks are offered to the agent; mandatory hooks emit `EXECUTE_COMMAND: <command>`.
- Runtime `events` (`session_start`, `pre_tool_use`, `post_tool_use`, `user_prompt_submit`, `stop`, `session_end`) execute the referenced command's `scripts` entry directly, and the integration translates them into the agent's native hook configuration.
- With `kiro-cli`, Spec Kit installs prompts under `.kiro/prompts/` with dotted names such as `speckit.specify`.

Settled by the spike in task 3, from Spec Kit `1.4.6.dev0` source (commit `f2010547`, 2026-11-25). Spec Kit was read, not run; the trial confirms the install.

- Local install: `specify extension add --dev <path>`. `requires.tools` is shown by `specify extension info` and not enforced. Command names must match `^speckit\.([a-z0-9-]+)\.([a-z0-9-]+)$`, and `file` must be a relative forward-slash path.
- A failing hook does not stop the core command: hooks are dispatched by text in the core command template, a mandatory hook emits `EXECUTE_COMMAND:` and the agent waits for it, and no exit code reaches Spec Kit. `conn speckit check` exits 0 anyway.
- Current feature: `SPECIFY_FEATURE_DIRECTORY`, then `feature_directory` in `.specify/feature.json`. `SPECIFY_FEATURE` is only a label. Conn adds `specs/<branch>/` as a last fallback.
- Constitution: `.specify/memory/constitution.md` is what `/speckit.constitution` writes; command templates still name `/memory/constitution.md`. Conn reads `.specify/memory/constitution.md` first, then `memory/constitution.md`. Principles are `### I. Name` headings and the version line is `**Version**: X | **Ratified**: ...`.
- `plan.md`: the first block under "Project Structure" is the documentation tree (`specs/[###-feature]/`); the source tree is under "### Source Code (repository root)". Conn prefers a block under a "Source Code" heading.
- `spec.md` opens with `**Feature Branch**:`, `**Created**:`, `**Status**:`, and `**Input**: User description: "..."` metadata lines, so the overview skips them and falls back to the Input description.
- Runtime events: the generated dispatcher passes the agent's native hook payload on standard input unchanged and wraps script output as `additionalContext`. Claude Code, Copilot, Codex, Cursor, Gemini, Qwen, OpenCode, Devin, Tabnine, and Vibe are event-capable; `kiro-cli` is not. The source holds no payload samples, so parsing is unconfirmed and the extension ships without `events` (requirement 5.4).

## Implementation notes

Choices made during implementation that refine this design:

- No migration: import provenance is part of the Objective `jsonb` document, and no database constraint restricts its source kind. The contract change alone admits `speckit_feature`, through the existing local-spec import route (no new route), which also requires a Spec Kit `source_ref` to be a clean repository-relative `.../spec.md`.
- The source kind is shared as `objective#/$defs/sourceKind` and appears as the optional `source_kind` of the review packet's and the Radar's Objectives (requirement 6.3). The packet Markdown labels it, such as "(from a Spec Kit feature)".
- Path rules: a token without `/`, or without a file extension, is accepted only when the file exists, so prose such as `e.g.` or `and/or` never becomes a path and never fills `Unresolved`. Directories are never planned paths.
- Planned analysis accepts at most 50 paths, so `conn start` and `conn speckit check` suggest claims for the first 50 extracted paths and say so.
- `conn start <feature-dir>` revises the caller's active Intent for the feature, showing the added and dropped claims first, instead of publishing a second one. A revision of an Intent owned by another of the caller's participants is refused with a hint to use `revise_intent` there.
- `conn speckit extension export <dir>` has no override flag: a target with different content is a conflict and nothing is written (requirement 4.7). The committed package is `integrations/speckit/conn/`.
- Command bodies use the generic guidance rendering, which names prompts for any MCP client. The `AGENTS.md` section gained a generic-only Spec Kit line; the earlier section is kept as a known version so an unchanged install is replaced, not reported as a conflict. The Kiro steering and power are unchanged.
- `conn doctor` reports a Spec Kit project and whether `.specify/extensions/conn/extension.yml` matches this build's extension version, read-only.

## Objective import

`workstart.SpecKitInput(root, path)` mirrors `KiroSpecInput`:

1. Resolve the path with symlinks evaluated. A directory must contain `spec.md`. A file must be named `spec.md`.
2. Require the path inside the repository through `kirospec.SafeReference`. Read at most 1 MiB, refusing symlinks and non-regular files.
3. Title: first `# ` heading with `Feature Specification:` stripped. If empty or still a template placeholder (`[FEATURE NAME]`), humanize the directory name without its numeric prefix (`003-chat-system` → `Chat system`).
4. Overview: the first paragraph under the title, otherwise "Imported from the Spec Kit feature `<ref>`."
5. Key points through a new `speckit.KeyPoints(spec, constitution)`:
   - `^\s*-\s*\*\*FR-(\d+)\*\*:\s*(.+)$` → `acceptance_criterion`, key `fr-<digits>`.
   - `^\s*-\s*\*\*SC-(\d+)\*\*:\s*(.+)$` → `outcome`, key `sc-<digits>`.
   - `\[NEEDS CLARIFICATION:\s*([^\]]+)\]` → `question`, keys `question-1`, `question-2`, ... in order.
   - Constitution `### <name>` headings that are not template placeholders → `constraint`, key `constitution-<slug>`, statement = heading plus first sentence.
   - Statements are trimmed to the `statement` bound. Template placeholder lines (`[...]` only) are skipped.
6. Constitution lookup: `memory/constitution.md`, then `.specify/memory/constitution.md`, first regular file wins. The `**Version**:` line, when present, is appended to the overview as "Constitution version X."

Contract change: `objective-import-input.source_kind` and the Objective's source enum gain `speckit_feature`. The migration widens the database check constraint. The identity rule for reuse is unchanged: same source kind, source reference, and content hash.

Because FR keys become acceptance keys, a team names checks in `.conn/checks.json` with `acceptance_keys: ["fr-001"]`, and the review packet shows FR-001's evidence state. That is the link from a Spec Kit requirement to proof.

## Path extraction

`speckit.PlannedPaths(root, featureDir) (Extraction, error)`:

- Source: `tasks.md` when present, otherwise the first fenced code block under a `Project Structure` or `Source Code` heading in `plan.md`.
- Task lines: `^\s*-\s*\[[ xX]\]\s*T\d+\b(.*)$`. `[P]` and `[USn]` tokens are dropped.
- Candidate tokens: backtick-quoted spans, then whitespace-separated words with trailing punctuation `.,;:)` removed.
- A token is a path when it contains `/` or matches `\.[A-Za-z0-9]{1,8}$`, has no scheme (`://`), is not absolute, does not contain `..` after cleaning, and resolves inside the root with symlinks evaluated. Windows separators are normalized to `/`.
- Plan code blocks: tree characters (`├──`, `└──`, `│`) are stripped. A line ending in `/` sets a directory prefix for its deeper-indented children.
- Accept the token if the file exists, or if its parent directory exists (a new file). Otherwise list it as unresolved.
- Output: `Paths []string` (deduplicated, first-appearance order, max 200), `NewPaths`, `Unresolved`, `TasksWithoutPath []string` (task IDs only), and `Truncated bool`.

The extractor is pure apart from `os.Lstat`. The fixture in `fixtures/speckit/` covers each rule.

## CLI

- `conn start <feature-dir | spec.md>`: detection order becomes objective UUID, GitHub issue reference, Spec Kit feature (directory with `spec.md`, or a `spec.md` file), Kiro spec. When no `--paths` is passed, the planned paths come from `PlannedPaths`, shown for confirmation with the option to drop paths. Everything after that is the existing flow: suggest claims, preview collisions, publish or quit.
- `conn speckit check [<feature-dir>] [--format text|json]`: resolve the feature (argument, then the spike's current-feature mechanism, then a `specs/<branch-name>/` directory matching the current Git branch), resolve the Project and Objective without importing, extract paths, call `suggest_claims` logic and the collision preview, print. If the feature's Objective has a focused or active Intent owned by the caller's Human, exclude it and print the claim delta. Exit 0 always. JSON output follows a new `speckit-check` schema: feature reference, extraction, candidate count, previewed collisions, delta, and errors.
- `conn speckit extension export <dir> [--force-same]`: write the embedded package. Refuse when a target file exists with different content. Print the Spec Kit install command confirmed in task 3.

## MCP

No new tools. The extension commands use existing tools: `read_work_context`, `suggest_claims` with `preview_collisions: true`, `publish_intent`, `revise_intent`, `check_collisions`, `analyze_intent`, `attach_evidence`, and `read_review_packet`. Paths come from `conn speckit check --format json`, which the command tells the agent to run first, so extraction stays deterministic for agents without a shell-free path parser.

## Extension package

Embedded under `internal/speckitext/templates/` and exported as:

```
conn/
  extension.yml
  commands/
    check.md
    start.md
    finish.md
    review.md
  scripts/            # only if task 3 confirms runtime events
    hook.sh
    hook.ps1
```

`extension.yml`:

```yaml
schema_version: "1.0"
extension:
  id: conn
  name: Conn
  version: 1.0.0
  description: Warns when another in-progress feature changes what this plan changes, and attaches review proof.
  repository: https://github.com/Juanqu261/conn
  license: MIT
requires:
  speckit_version: ">=1.4.0,<2.0.0"
  tools:
    - name: conn
      required: true
provides:
  commands:
    - name: speckit.conn.check
      file: commands/check.md
      description: Check this feature's plan against teammates' in-progress work.
    - name: speckit.conn.start
      file: commands/start.md
      description: Publish or revise this feature's Conn Intent and check collisions before implementing.
    - name: speckit.conn.finish
      file: commands/finish.md
      description: Record the head revision, analyze it, offer evidence runs, and produce review proof.
    - name: speckit.conn.review
      file: commands/review.md
      description: Produce the Conn review packet as a pull-request description.
hooks:
  after_tasks:
    command: speckit.conn.check
    optional: true
    prompt: "Check this plan against teammates' in-progress work with Conn?"
  before_implement:
    command: speckit.conn.start
    optional: false
    description: Conn publishes or revises this feature's Intent and warns about collisions. It never blocks.
  after_implement:
    command: speckit.conn.finish
    optional: true
    prompt: "Record review proof for this feature with Conn?"
tags: [coordination, collisions, review]
```

Each command file is generated from `internal/agentguide`, the same source as the Kiro steering and the `AGENTS.md` section, so the three surfaces never disagree. Rules every command carries:

- Run `conn speckit check --format json` for the feature directory first and use its paths. Do not invent paths.
- Use Conn MCP tools when connected, otherwise the `conn` CLI.
- A collision is advisory. Show it with names and ask the human. Never record a decision, accept a proposal, or create a credential.
- `speckit.conn.start` asks before publishing. If a revision only adds claims, show the added subjects first.
- `speckit.conn.finish` asks before each `conn evidence run`, matching `/conn-finish`.

`before_implement` is mandatory so that the check is never skipped silently, and it is safe because every Conn step it triggers is warn-only or confirmed.

## Agent lifecycle events

Only after the spike confirms parseable payloads. `conn hook` gains a `--client auto|kiro|claude|copilot|...` input adapter. Kiro's format stays the default. Each adapter maps the agent's tool event to the existing `hooks.Event` (event kind, tool name, written paths). Unknown formats allow the event and record a diagnostic. `scripts/hook.sh` and `hook.ps1` call `conn hook <event> --client auto`, reading the payload from standard input. No hook writes to Conn, matching decision 0006.

## Contracts and persistence summary

- `objective-import-input`, `objective`: add `speckit_feature` to the source kind enum.
- New `speckit-check` schema for the CLI JSON output, with accepted and rejected fixtures.
- Migration: widen the Objective source-kind check constraint.
- No new routes or events.

## Security and privacy

- Spec Kit files are read locally with the same bounds as Kiro specs. Only confirmed Objective content and path-derived claims reach Conn.
- Conn never executes text from Spec Kit files and never writes into `.specify/`.
- Extension commands contain no secrets and grant the agent no new authority.

## Error behavior

- Missing `spec.md`: usage error naming the expected layout.
- No `tasks.md` and no plan structure block: continue with no planned paths and say so. Planned claims can still be added with `--paths`.
- No Conn connectivity in `conn speckit check`: print the reason and exit 0.
- More than 200 paths or 50 key points: truncate, report what was dropped.

## Validation plan

- Parser tests over `fixtures/speckit/` (parameterized by rule): FR/SC/question/constitution extraction, placeholders skipped, task path tokens, plan tree blocks, new versus unresolved paths, traversal and symlink refusal, bounds.
- Contract fixtures for `speckit_feature` and `speckit-check`.
- One Go scenario with the backend: two Spec Kit features in one repository whose tasks name the same exported function. Feature A's check previews nothing and publishes. Feature B's check previews a collision with A. After B publishes, the persisted collision equals the preview.
- An export test: the package matches committed golden files and export refuses different content. Conn adds no YAML dependency; Spec Kit accepting the manifest is checked in the spike and the real trial.
- Real trial in `docs/real-trial.md`.

## Deferred

- Publishing the extension to the Spec Kit community catalog.
- Importing `contracts/` and `data-model.md` as contract and data claims. That belongs with contract-diff claim sources.
- Using `[P]` markers to suggest which tasks of one feature can safely run in parallel against teammates' work.
