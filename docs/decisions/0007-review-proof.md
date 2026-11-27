# 0007: Review proof

- Status: accepted
- Owner: project owner
- Amends: [0002](0002-runtime-and-interaction-surfaces.md) (CLI check execution and Git hook installation), [0005](0005-collision-warnings-and-review-proof-core.md) (evidence trust levels)

## Context

Decision 0005 makes review proof the second half of the product core. After milestone 1, evidence state was whatever a participant typed into `attach_evidence`, a reviewer had to reconstruct intent from the diff, and nothing linked commits to Intents without a GitHub App. A check run from the developer's terminal could not attach to an Intent that the developer's Kiro participant owned.

The alternatives were running checks from MCP tools or hooks, a hosted runner, signed local attestations, a GitHub App for commit linking, and a persisted review record. They let an agent execute arbitrary commands without the shell permission prompt, sent source off the machine, added key management, added a hosted integration before the CI milestone, or created a second source of truth that could drift from the ledger.

## Decision

1. The Conn CLI executes check commands defined in the repository's `.conn/checks.json` only when a human or agent explicitly runs `conn evidence run <check-key>`. Commands are argument arrays run without a shell, in a working directory inside the repository, with a bounded timeout, and with the caller's environment minus every `CONN_` variable. MCP tools and hooks never run checks. Agents run the command through their shell tool, so the client's shell permission applies.
2. Evidence gains an optional observation record and the `check_run` kind. A local observation means "the Intent owner's machine ran this command at this commit". It is stronger than reported evidence and weaker than CI proof, and every view labels it as observed on the participant's machine. Only `runner: "local"` exists in this milestone. Raw output stays in a local log under the worktree's Git directory, and only its SHA-256 hash reaches Conn. The MCP `attach_evidence` schema cannot carry an observation.
3. Evidence may be attached by any Participant of the Human who owns the Intent, and the attaching Participant is recorded as producer. Intents stay owned by one Participant. Work context, and therefore review packets, may select an Intent owned by any Participant of the signed-in Human.
4. Commits name their Intent with the trailer `Conn-Intent: <intent-id>@<revision>`. `conn trailer` adds it from the local focus record without a network call and never fails a commit. Conn writes to the repository hooks directory only through explicit `conn git-hook install`, which targets the effective hooks directory including `core.hooksPath`, refuses to overwrite a different hook, and removes only byte-identical Conn content.
5. The review packet is a generated, non-persisted view with the versioned JSON Schema `review-packet` (`packet_version: 1`). It is built from Conn records and local Git facts by one shared loader, contains no source, diffs, prompts, transcripts, credentials, or logs, and renders to Markdown under 60,000 characters.

Scope drift compares declared and observed `code_symbol` and `contract` subjects with `changes` or `removes` relations. It is an advisory view and never changes an Intent, claim, or collision.

## Trust statement

A local observation is attested by the owner's machine. The owner, or an agent acting for them, can edit `.conn/checks.json` or the code under test. The packet therefore flags any change to `.conn/checks.json` on the branch, runs on uncommitted changes, evidence recorded at another commit, and reported evidence. Accepting an observation from any Participant of the owning Human adds no new trust: the same Human could already attach reported evidence from that Participant. Independent proof arrives with `runner: "ci"` in milestone 3.

## Consequences

- Reviewers see what a change intended, touched, and verified from one bounded document, and can tell observed runs from reported states.
- Evidence attached at one commit becomes visibly stale after a later commit; the packet and Radar compute staleness from the reviewed head.
- Check commands are repository content that agents can change, so trust in a run depends on the reviewed configuration.
- Each Intent revision accepts one analysis. The finish workflow records the head revision first, then analyzes that revision.
- Milestone 3 reuses the packet schema, the trailer parser, and the check configuration for CI.

## Validation

A parameterized Go test covers drift. A Go scenario on a temporary Git repository with a fake backend covers passing, failing, and timed-out runs, the stripped `CONN_` environment, the output hash, the bounded log, the dirty flag, and the evidence payload. A PostgreSQL scenario covers same-Human acceptance, other-Human rejection, observation storage, and listing order. A Go scenario covers the Git hook and trailers, including `core.hooksPath` and a conflicting hook. A Go test covers packet schema validity, the Markdown bound, sanitization, and integrity flags. An MCP scenario on `fixtures/pre-edit-two-sessions` covers drift, observed and stale evidence, the trailer, and the checks-configuration flag. A real reviewer reviews a pull request from the packet, and the open questions are recorded in `docs/real-trial.md`.
