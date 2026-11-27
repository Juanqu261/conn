# Design

## Purpose and scope

This specification implements milestone 2 of decision 0005: review proof. After an agent finishes, Conn tells the reviewer what the change intended, what it touched, and what was verified, in one bounded packet.

The milestone adds:

- a scope drift report comparing declared and observed claims;
- `.conn/checks.json` and `conn evidence run`, so evidence state comes from an observed command;
- evidence provenance and staleness in every view;
- evidence attachment from any Participant of the Intent's Human;
- `Conn-Intent` commit trailers and an optional Git hook;
- the review packet as CLI output, an MCP read tool, and a prompt.

Out of scope: CI execution, combined-branch verification, and posting to GitHub. Those belong to `verification-ci-and-adoption`, which reuses the packet schema, trailer parser, and check configuration defined here.

## Dependencies on milestone 1

| From `pre-edit-collision-warnings`                     | Used for                                                  |
| ------------------------------------------------------ | --------------------------------------------------------- |
| Observed analysis with `changes` and `removes`         | Drift comparison                                          |
| `base_revision_ref` and `branch_ref` filled on Intents | Diff range and packet header                              |
| `<git-dir>/conn/focus.json`                            | Default Intent for `conn evidence run` and `conn trailer` |
| `<git-dir>/conn/written.jsonl`                         | Marking files written by an agent session                 |
| Collision states and `decision_ref`                    | Packet collision section                                  |

## Architecture decision gate

Before implementation, record decision 0007 under `docs/decisions/`:

1. The Conn CLI may execute check commands defined in `.conn/checks.json` when a human or agent explicitly runs `conn evidence run`. Commands are argument arrays run without a shell. MCP tools and hooks never run checks.
2. Evidence gains an optional observation record. A local observation means "the Intent owner's machine ran this command at this commit". It is stronger than a reported state and weaker than CI proof, and every view labels it that way.
3. Evidence may be attached by any Participant of the Human who owns the Intent. Intents stay owned by one Participant.
4. Commits name their Intent with the `Conn-Intent: <intent-id>@<revision>` trailer. Conn writes to the repository hooks directory only through explicit, conflict-safe installation.
5. The review packet is a generated, non-persisted view with a versioned JSON Schema.

## Scope drift

`internal/review` holds pure functions and loaders shared by the CLI, MCP, and the later CI check.

Inputs:

- declared claims of the current Intent revision, with evidence paths;
- observed `static_analysis` claims of the same revision and their `observed_at`;
- the analyzer's incomplete flag, from the diagnostics stored in `intent_analysis_reports` for that revision;
- the changed file list for `base..head`, plus working-tree changes when the head is the working tree;
- `written.jsonl` entries for the worktree.

Algorithm:

1. Restrict the comparison to `code_symbol` and `contract` subjects with relation `changes` or `removes` on either side.
2. `matched` is the intersection of declared and observed subjects. `declared_not_observed` and `observed_not_declared` are the two differences.
3. `changed_files_without_claim` holds changed paths that appear in no declared claim's evidence. Each item carries `written_by_agent`.
4. `not_observable` holds declared claims of kind `behavior`, `invariant`, `data_resource`, `infrastructure_resource`, `dependency`, `assumption`, `question`, or `acceptance_criterion`. An `acceptance:<objective-ref>:<key>` subject links to that acceptance key's evidence.
5. `analysis` is `not_analyzed`, `complete`, or `incomplete`, with the revision and time.

Each list is bounded to 100 items and carries an omitted count. A `consumes` difference is not drift, because using an existing contract is not a change to it.

## Check configuration

New contract `contracts/v1/checks-config.schema.json`:

```json
{
  "version": 1,
  "checks": {
    "gateway-tests": {
      "command": ["pnpm", "--filter", "gateway", "test"],
      "working_directory": ".",
      "timeout_seconds": 600,
      "acceptance_keys": ["gateway-tests"],
      "env": { "CI": "1" }
    }
  }
}
```

Rules:

- check keys use the existing `key` pattern, with at most 50 checks;
- `command` has 1 to 64 items of at most 1,024 characters each, with no shell interpretation;
- `working_directory` is relative and must resolve inside the repository;
- `timeout_seconds` is between 1 and 3,600;
- `env` names match `^[A-Z_][A-Z0-9_]*$`, must not start with `CONN_`, and have literal values of at most 1,024 characters (the file is committed, so values are not secrets);
- `acceptance_keys` has at most 20 keys.

Go and Node.js validate the file with the existing validators. The schema is also the source for the M3 CI pair checks.

## `conn evidence run`

```text
conn evidence run <check-key> [--intent <id>] [--acceptance <key>]... [--worktree <path>]
```

Sequence:

1. Resolve the worktree and Intent from `--intent` or `focus.json`. Read work context and verify that the Intent is active, that the worktree remote matches its repository, and that every acceptance key exists on its Objective revision.
2. Load and validate `.conn/checks.json`. Compute `config_sha256` over the file bytes and `command_sha256` over the canonical JSON of the check definition.
3. Record `commit` (`git rev-parse HEAD`) and `worktree_dirty` (non-empty `git status --porcelain`).
4. Start the command with `exec.CommandContext`: the argument array, the configured directory, the environment without `CONN_` variables plus the configured values, and the timeout. On timeout, kill the process group.
5. Stream combined output to the terminal and through a SHA-256 hasher. Write the first 10 MiB to `<git-dir>/conn/runs/<evidence-id>.log`.
6. Attach one evidence reference per acceptance key, or one without a key when none is configured. Use kind `check_run`, state `passing` or `failing`, locator `conn-check:<check-key>@<commit-12>`, and the observation record.
7. Print the evidence IDs, state, and log path. Exit with the check's exit code so scripts can react.

Agents run this command through their shell tool, so Kiro's shell permission applies. `/conn-finish` asks the human before suggesting each run.

## Evidence model changes

`evidence-attachment-input` and `evidence-reference` gain kind `check_run` and an optional `observation`:

```json
{
  "method": "conn_check_run",
  "runner": "local",
  "check_key": "gateway-tests",
  "config_sha256": "…",
  "command_sha256": "…",
  "commit": "4f1c2e9a7b3d",
  "worktree_dirty": false,
  "exit_code": 0,
  "timed_out": false,
  "duration_ms": 18234,
  "output_sha256": "…",
  "started_at": "2026-01-01T00:00:00Z"
}
```

`runner` allows only `local` in this milestone. The MCP `attach_evidence` input schema omits `observation` and rejects it as an unknown property. The HTTP route accepts it from any authenticated Participant of the owning Human. The trust statement in the decision explains why that is acceptable.

Authority change in `AttachEvidence`: replace the same-Participant check with a same-Human check. The producer remains the calling Participant.

Migration `00009_evidence_observation.sql` adds a nullable `observation jsonb` column, the `check_run` kind, and an index on `(project_id, intent_id, observed_at DESC)`.

New read route `GET /v1/projects/{project_id}/intents/{intent_id}/evidence` returns the Intent's evidence references, newest first, bounded to 200.

Radar `evidence_coverage` items gain `provenance` (`observed_run` or `reported`) and `commit`. Staleness against a head is computed where the head is known: by the packet from local Git, and by Radar from the Intent's `head_revision_ref` when it is set.

## Commit trailers

Format: `Conn-Intent: <intent-id>@<revision>`.

`conn trailer <message-file> [<source>]`:

1. Read `focus.json`. Make no network call, so commits stay fast.
2. When an Intent is focused, run `git interpret-trailers --in-place --where end --if-exists doNothing --trailer "Conn-Intent: <id>@<rev>" <message-file>`.
3. On any failure, print one warning to standard error and exit 0.

`conn git-hook preview|install|remove` manages `commit-msg` in `git rev-parse --git-path hooks`:

```sh
#!/bin/sh
# conn commit-msg v2
"<conn-executable>" trailer "$1" || true
```

`conn trailer` leaves a message that is empty apart from comments and the `--verbose` diff untouched, so Git still aborts it. Version 1 used `prepare-commit-msg`, which runs before the editor opens: its trailer made an abandoned message non-empty and Git committed it. Install and removal delete an unchanged version 1 hook and report a modified one for manual review.

Install refuses when a different hook exists and prints the line to add to it. Removal deletes only byte-identical Conn content. The structure mirrors the Kiro bundle installer.

Work context gains `focus.commit_trailer` for an owned focused Intent. The schema and Go type change together. Steering tells agents to use `git commit --trailer "<commit_trailer>"` when the Git hook is absent.

`internal/review/trailers.go` parses `git log --format=%H%x00%B%x00 base..head` and returns, per Intent ID, the latest revision and the commits that named it. Milestone 3 uses it in CI.

## Review packet

### Schema

New contract `contracts/v1/review-packet.schema.json`, `packet_version: 1`:

```text
ReviewPacket
  generated_at_event, project {id, name}
  intent {id, revision, participant_id, owner_login, summary (sanitized, <= 4,000 chars), assumptions}
  objective {id, revision, title}
  blueprint? {id, revision}, charter? {id, accepted}
  git {branch, base, head, head_is_worktree, dirty, changed_file_count, checks_config_changed}
  acceptance [{key, statement, state, provenance, evidence [{id, kind, locator, commit, stale, dirty, exit_code, duration_ms}]}]
  drift {analysis, matched, declared_not_observed, observed_not_declared, changed_files_without_claim, not_observable, omitted}
  collisions [{id, rule_id, state, matched_subjects, counterpart {intent_id, revision}, decision? {kind, binding, decided_by_login, rationale_excerpt <= 500}, proposal_count}]
  integrity_notes [string], open_questions [string], known_unknowns [string]
  truncated [string]
```

`checks_config_changed` is true when `.conn/checks.json` appears in the `base..head` file list.

### Loading

`internal/review.Load` reads work context for the Intent, Radar, the Intent's evidence list, the Objective revision, project members (for logins), and the conflict context of up to 10 related collisions. It confirms that every response has the same `as_of_event` and retries once when they differ. Local Git supplies the branch, base, head, dirty state, and changed files. `internal/mcp/handoff.go` moves to the same loader.

### Markdown layout

1. Title line with the Intent and Objective.
2. "What this change set out to do": the sanitized summary and assumptions.
3. "Acceptance evidence": a table of key, state, provenance, commit, and stale flag.
4. "Scope drift": matched, undeclared, unobserved, and files without claims.
5. "Collisions and decisions".
6. "Integrity notes" and "Known unknowns".
7. A footer with the cursor and the statement that the packet is generated from Conn records and contains no source.

Participant text is sanitized: control characters are removed, `<` and `>` are escaped, and each line is bounded. Rendering keeps sections in order and truncates lists from the end, recording each truncation, until the output is under 60,000 characters. GitHub limits a pull-request body to 65,536 characters.

### Surfaces

- `conn review [--intent <id>] [--base <ref>] [--format markdown|json] [--output <file>] [--analyze]`. `--analyze` runs observed analysis first, which is an explicit write.
- MCP `read_review_packet {project_id?, intent_id?, format}`. Read-only. It never runs analysis.
- Prompt `/conn-review`: run `analyze_intent` with approval, run `check_collisions`, then `read_review_packet`, and offer to write the Markdown to a file for `gh pr create --body-file`.

## Workflow changes

`intent_analysis_reports` allows one analysis per Intent revision, and observed claims belong to that revision. The finish order therefore records the head first and analyzes the resulting revision:

1. Read the focused work context and verify exact revisions.
2. When the agent's changes are committed, revise the Intent with `head_revision_ref` set to the current commit.
3. Run `analyze_intent` (observed) on the current revision.
4. Run `check_collisions`.
5. List checks in `.conn/checks.json` mapped to the Objective's acceptance keys. Ask the human before running each with `conn evidence run` through the shell.
6. Attach reported evidence only for references the human supplied.
7. Run `read_review_packet` and present drift, stale evidence, and open collisions.
8. Release only when the human asks, and state that release does not prove Objective, pull-request, or Project completion.

Further changes after analysis require a new Intent revision before analysis can run again. The packet reports the analyzed revision so a reviewer can see when analysis predates the head.

## Security and privacy

- Check commands receive no `CONN_` variables, so the Conn session cannot leak into test code.
- Raw output stays in the local log, and only its hash leaves the machine.
- `.conn/checks.json` is repository content that an agent can edit. The packet flags any change to it on the branch, so a reviewer sees when the proof definition moved.
- An observation is attested by the owner's machine. CI proof arrives in milestone 3 with `runner: "ci"`.
- Trailers carry identifiers only.
- Packets contain identifiers, subjects, paths, logins, states, and bounded sanitized prose. They exclude source, diffs, prompts, transcripts, credentials, and logs.

## Error behavior

| Condition                              | Behavior                                                                                        |
| -------------------------------------- | ----------------------------------------------------------------------------------------------- |
| Missing or invalid `.conn/checks.json` | `conn evidence run` fails before execution with the schema error                                |
| Unknown acceptance key                 | Fails before execution                                                                          |
| Intent not active or worktree mismatch | Fails before execution                                                                          |
| Backend unavailable after the run      | Prints the result and log path, exits non-zero, and records nothing. The human can rerun later. |
| Packet responses at different cursors  | One retry, then failure with a stale-context message                                            |
| No observed analysis                   | Drift section reads "not analyzed"                                                              |
| Trailer failure                        | Warning only. The commit proceeds.                                                              |

## Validation plan

1. **Drift (Go, parameterized pure function):** matched, both differences, files without claims with the written-by-agent flag, not observable, not analyzed, incomplete, bounds.
2. **Check run (Go, temporary Git repository, fake backend):** passing, failing, and timeout. The child process proves that no `CONN_` variable is present. Covers the output hash, bounded log, dirty flag, configuration hash, and the evidence payload.
3. **Evidence authority (PostgreSQL):** another Participant of the same Human is accepted, another Human is rejected, and the MCP schema rejects `observation`.
4. **Trailer (Go, temporary Git repository):** hook install, trailer added once, existing trailer kept, missing focus still commits, a conflicting existing hook preserved, `core.hooksPath` respected.
5. **Packet (Go):** schema-valid JSON, Markdown under 60,000 characters with oversized inputs, no source marker, sanitized prose, flags for checks-configuration change and stale evidence.
6. **Milestone scenario (MCP):** on `fixtures/pre-edit-two-sessions`, declare a change to `shouldRetryCharge`, also change an undeclared export, run observed analysis and a configured check, generate the packet, commit again, and confirm stale evidence.
7. **Manual:** open a real pull request with the packet as its description. A reviewer who did not see the session reviews it and records the open questions in `docs/real-trial.md`.

Run `pnpm check`, `pnpm test`, and `pnpm build`.

## Deferred

- Running checks in CI and `runner: "ci"` (milestone 3).
- Posting packets to GitHub (milestone 3).
- Signed attestations for local runs.
- Mapping individual test cases to acceptance keys automatically.
