# Requirements

Milestone 1 of decision 0005. Acceptance goal: two Kiro sessions whose participants never exchange subject strings receive a correct advisory warning before the second agent edits code that conflicts with the first agent's in-progress Intent.

## 1. Optional planning layer

**User story:** As a developer adopting Conn, I want collision warnings as soon as my repository and Objective exist so that I do not have to complete Blueprint and Charter ceremony before Conn is useful.

### Acceptance criteria

1. WHEN a Project has an Objective revision and a RepositoryConnection but no Blueprint
   THE SYSTEM SHALL support Intent publication, claim suggestions, collision assessment, warnings, and evidence attachment
2. WHEN work context, Radar, or a hook reports a missing Blueprint or Charter
   THE SYSTEM SHALL describe it as optional planning context and SHALL NOT present it as a blocker or an error
3. WHEN a Project uses Blueprints and Charters
   THE SYSTEM SHALL preserve existing Blueprint activation, own-Charter acceptance, and Charter reference behavior
4. THE SYSTEM SHALL keep an Objective revision required for every Intent

## 2. Shared concept catalog

**User story:** As an agent describing planned work, I want to see the semantic subjects my team already uses so that I choose an existing subject instead of inventing a different string for the same concept.

### Acceptance criteria

1. WHEN an Objective revision contains a keyed invariant or keyed acceptance criterion
   THE SYSTEM SHALL derive a stable catalog subject from the Objective key, or the Objective ID when no key exists, and the key-point key
2. WHEN a later Objective revision keeps the same Objective key and key-point key
   THE SYSTEM SHALL derive the same subject
3. WHEN a member requests the catalog
   THE SYSTEM SHALL return Objective-derived subjects and the normalized subjects of active claims, with subject kind, display name, source, and the active Intents that claim each subject and their relations
4. WHEN the request includes a subject kind or a text query
   THE SYSTEM SHALL filter by kind and rank results by a deterministic token-similarity score with a stated reason
5. WHEN results exceed the requested limit
   THE SYSTEM SHALL return a bounded page and SHALL state that more results exist
6. THE SYSTEM SHALL expose the catalog through HTTP, the authenticated client, the `list_concepts` MCP tool, and the `conn concepts` CLI command
7. THE SYSTEM SHALL NOT use embeddings, a hosted model, or source content to build or rank the catalog

## 3. Near-match advice on Intent publication

**User story:** As a participant publishing an Intent, I want to know when my declared subject looks like an existing one so that I can correct it before it silently misses a collision.

### Acceptance criteria

1. WHEN a published or revised Intent declares a subject that is not in the catalog
   AND a catalog subject of the same kind exceeds the similarity threshold
   THE SYSTEM SHALL return an advisory observation naming the declared subject, up to three similar subjects, and the Intents that claim them
2. WHEN Conn returns a near-match observation
   THE SYSTEM SHALL NOT change the Intent, its claims, or any collision
3. THE SYSTEM SHALL bound near-match observations to 20 per Intent revision

## 4. Claim suggestions before editing

**User story:** As an agent about to edit code, I want Conn to turn the files and symbols I plan to touch into precise claims so that a collision can be detected before I write anything.

### Acceptance criteria

1. WHEN a participant requests suggestions with planned repository-relative paths and optional symbol names
   THE SYSTEM SHALL run local analysis in planned mode and return candidate claims: `changes` for the named or all exported symbols and contracts of each planned file, and `consumes` for repository-internal contracts that each planned file imports
2. WHEN the request includes an Objective or a text focus
   THE SYSTEM SHALL add matching catalog candidates, including Objective-derived invariants
3. WHEN a planned path does not exist yet
   THE SYSTEM SHALL report it as a new file with no derived claims rather than fail the request
4. WHEN suggestions are returned
   THE SYSTEM SHALL label each candidate with its source and evidence references and SHALL NOT persist any claim
5. WHEN a participant includes chosen candidates in `publish_intent` or `revise_intent`
   THE SYSTEM SHALL store them as declared claims under the existing Intent rules
6. THE SYSTEM SHALL bound a request to 50 planned paths and a response to 100 candidates and SHALL state when candidates were omitted
7. THE SYSTEM SHALL expose suggestions through the `suggest_claims` MCP tool and the `conn intent suggest-claims` CLI command

## 5. Git context on Intent publication

**User story:** As an agent publishing an Intent, I want Conn to fill in repository, branch, and base revision from my worktree so that later analysis and pull-request checks know what my change is based on.

### Acceptance criteria

1. WHEN `publish_intent` or `revise_intent` omits `repository_id`
   AND exactly one RepositoryConnection in the Project matches the local normalized remote
   THE SYSTEM SHALL fill `repository_id`
2. WHEN the input omits `branch_ref` or `base_revision_ref`
   AND the local worktree matches the Intent's repository
   THE SYSTEM SHALL fill the current branch and the merge base with the repository default branch
3. WHEN a value was filled
   THE SYSTEM SHALL report every filled value in the tool result
4. WHEN the caller supplies a value explicitly
   THE SYSTEM SHALL use the supplied value
5. WHEN local Git metadata is unavailable or ambiguous
   THE SYSTEM SHALL leave the fields empty and report why

## 6. Diff-based observed analysis

**User story:** As a participant whose agent has edited code, I want analysis to describe what my change actually changed so that warnings are precise and contract changes are detected.

### Acceptance criteria

1. WHEN `analyze_intent` runs without explicit paths
   THE SYSTEM SHALL analyze the TypeScript files changed between the Intent base revision and the working tree, including staged, unstaged, and untracked files not ignored by Git
2. WHEN the Intent has no base revision
   THE SYSTEM SHALL use the merge base of `HEAD` and the repository default branch, and SHALL report a diagnostic and attach no claims when neither is available
3. WHEN an exported declaration differs from its base version
   THE SYSTEM SHALL emit `changes` for its code symbol and contract
4. WHEN an exported declaration was removed
   THE SYSTEM SHALL emit `removes` for its code symbol and contract
5. WHEN an exported declaration was added
   THE SYSTEM SHALL emit `produces` for its contract
6. WHEN a changed file imports a repository-internal export
   THE SYSTEM SHALL emit `consumes` for that contract
7. THE SYSTEM SHALL NOT emit claims for unchanged exports of changed files
8. WHEN claims exceed the report bound
   THE SYSTEM SHALL keep `removes`, then `changes`, then `consumes`, then `produces`, and SHALL report a truncation diagnostic with counts
9. WHEN a static-analysis contract claim with `changes` or `removes` and another active Intent's claim with `consumes` share a subject
   THE SYSTEM SHALL open a `contract_producer_consumer` assessment
10. THE SYSTEM SHALL keep source text, diffs, and compiler message text out of the report

## 7. Analyzer availability in any connected repository

**User story:** As a developer using Conn in my own project, I want local analysis to work from my repository and not only from the Conn source checkout.

### Acceptance criteria

1. WHEN `conn mcp` or a CLI command needs the analyzer
   THE SYSTEM SHALL resolve it from `CONN_ANALYZER_PATH`, then relative to the `conn` executable, then relative to the current directory
2. WHEN `session_status` or `conn doctor` runs
   THE SYSTEM SHALL report whether the analyzer and Node.js are available and which location was used
3. WHEN the analyzer is unavailable
   THE SYSTEM SHALL report analysis as unavailable and SHALL keep declared claims and collision warnings working

## 8. Collision lifecycle follows human decisions

**User story:** As a team that has decided how to handle a collision, I want the warning to close and stay closed so that agents do not learn to ignore Conn.

### Acceptance criteria

1. WHEN a Human records a `false_positive` decision
   THE SYSTEM SHALL set the collision state to `dismissed`
2. WHEN a Human records a `dependency`, `sequencing`, `contract_agreement`, or `accepted_risk` decision
   THE SYSTEM SHALL set the collision state to `resolved`
3. WHEN a Human records a `revise_objective`, `revise_blueprint`, or `other` decision
   THE SYSTEM SHALL set the collision state to `under_review`
4. WHEN a member who is not an owner records a `false_positive` decision
   THE SYSTEM SHALL require that member to be the Human behind one of the two Intents
5. WHEN an Intent revision produces a new assessment for the same rule and Intent pair as a dismissed or resolved assessment
   AND the new matched subjects are a subset of the decided subjects
   THE SYSTEM SHALL record the new assessment with the inherited state and a reference to the decision
6. WHEN the new matched subjects include a subject that the decision did not cover
   THE SYSTEM SHALL open the new assessment
7. WHEN a collision state changes
   THE SYSTEM SHALL append a `collision.updated` event in the same transaction
8. THE SYSTEM SHALL store the matched normalized subjects on each assessment
9. WHEN Radar, work context, or `check_collisions` lists collisions
   THE SYSTEM SHALL list `open` and `under_review` collisions as needing attention and SHALL show decided collisions with their decision reference
10. THE SYSTEM SHALL NOT change collision state from a non-binding proposal

## 9. Local hook state

**User story:** As a hook running outside the MCP process, I need to know which Intent belongs to the current worktree without asking the model.

### Acceptance criteria

1. WHEN the MCP adapter publishes, revises, or focuses an owned Intent in a Git worktree that matches the Intent's repository
   THE SYSTEM SHALL write a local focus record containing only the server URL, Project, Intent, revision, Participant, last event cursor, and update time
2. WHEN the MCP adapter releases the focused Intent
   THE SYSTEM SHALL clear the focus record
3. THE SYSTEM SHALL store hook state under the worktree's Git directory so that Git never commits it and each linked worktree has its own state
4. THE SYSTEM SHALL write hook state atomically and SHALL tolerate missing, partial, or deleted state
5. THE SYSTEM SHALL NOT store credentials, prompts, source, or Project prose in hook state
6. THE SYSTEM SHALL treat hook state as a non-authoritative cache and SHALL confirm Intent state with the backend before reporting it

## 10. Awareness at prompt submission

**User story:** As an agent starting a new turn, I want to learn what changed for my Intent since my last turn so that I react to new collisions without background polling.

### Acceptance criteria

1. WHEN a prompt is submitted in a worktree with a focus record
   THE SYSTEM SHALL read Project events after the stored cursor and report new or changed collisions, decisions, and counterpart Intent revisions that involve the focused Intent
2. WHEN nothing relevant changed
   THE SYSTEM SHALL print nothing
3. WHEN pre-write warnings are pending
   THE SYSTEM SHALL deliver them and clear them
4. WHEN the repository is connected to Conn but no Intent is focused
   THE SYSTEM SHALL print one line recommending `/conn-start` before editing
5. WHEN output is produced
   THE SYSTEM SHALL label it as Project data, use only identifiers, rule IDs, normalized subjects, states, and counts, stay under 1,500 characters, and update the stored cursor

## 11. Warning before writes

**User story:** As a developer, I want to be warned before my agent writes a file that conflicts with someone else's in-progress work.

### Acceptance criteria

1. WHEN an agent is about to run a write tool on a path inside a Conn-connected worktree
   AND no Intent is focused
   THE SYSTEM SHALL warn that no Intent is published for this work
2. WHEN the target path appears in the evidence of an active claim with `changes`, `removes`, or `consumes` from another participant's Intent
   THE SYSTEM SHALL warn with that Intent, subject, and relation
3. WHEN the target path appears in the claims of an open or under-review collision that involves the focused Intent
   THE SYSTEM SHALL warn with the collision and rule
4. WHEN a warning applies
   THE SYSTEM SHALL show it to the human without blocking the tool call and SHALL queue it for the agent's next prompt submission
5. THE SYSTEM SHALL NOT block, deny, or ask for permission from any hook in this version
6. WHEN Conn, the network, or the credential is unavailable, or the hook exceeds its time budget
   THE SYSTEM SHALL allow the tool call silently and record a local diagnostic for `conn doctor`
7. WHEN the local index is warm
   THE SYSTEM SHALL complete the check within 500 milliseconds at the 95th percentile

## 12. Record of written paths

**User story:** As a reviewer, I want to know which files an agent session wrote so that scope drift can distinguish agent edits from other local edits.

### Acceptance criteria

1. WHEN a write tool completes inside a Conn-connected worktree
   THE SYSTEM SHALL append the repository-relative path, Kiro session ID, and time to the local written-path record
2. THE SYSTEM SHALL NOT record file content
3. THE SYSTEM SHALL bound the record to 5,000 entries and discard the oldest entries first

## 13. Hook installation

**User story:** As a developer, I want to install the new hooks safely and remove the old credit-consuming hooks.

### Acceptance criteria

1. WHEN a human previews or installs the Kiro integration bundle
   THE SYSTEM SHALL provide command-action hooks for SessionStart, UserPromptSubmit, PreToolUse on write tools, and PostToolUse on write tools, and updated steering
2. THE SYSTEM SHALL NOT install agent-prompt hooks
3. WHEN the previous bundle's hook file exists unchanged
   THE SYSTEM SHALL list its removal in the preview and remove it during installation
4. WHEN the previous bundle's hook file was modified
   THE SYSTEM SHALL preserve it and report it for manual review
5. THE SYSTEM SHALL keep preview write-free, refuse to overwrite different content, and preserve unrelated Kiro files
6. THE SYSTEM SHALL render the hook command with the absolute path of the current `conn` executable by default and SHALL allow a PATH-based command by option

## 14. Pre-edit start workflow

**User story:** As a participant beginning work in Kiro, I want `/conn-start` to publish precise claims and check collisions before implementation begins.

### Acceptance criteria

1. WHEN `/conn-start` runs
   THE SYSTEM SHALL instruct the agent to load work context, call `list_concepts` and `suggest_claims` for the planned paths, publish or revise the Intent with chosen candidates, and call `check_collisions` before any write
2. WHEN `/conn-start` reports collisions
   THE SYSTEM SHALL present them as advisory and SHALL point to `/conn-resolve`
3. WHEN `/conn-finish` runs
   THE SYSTEM SHALL use diff-based observed analysis
4. THE SYSTEM SHALL update the optional steering to describe the pre-edit loop and warn-only hooks

## 15. Safety, privacy, and authority

### Acceptance criteria

1. THE SYSTEM SHALL keep source text, diffs, prompts, transcripts, credentials, and raw logs out of catalog, suggestion, analysis, hook, and collision output
2. THE SYSTEM SHALL confine planned and observed analysis paths to the trusted worktree
3. THE SYSTEM SHALL pass no Conn credential to the analyzer or to Git subprocesses
4. THE SYSTEM SHALL invoke Git with fixed arguments, without a shell, and SHALL reject revision references that could be read as options
5. THE SYSTEM SHALL keep all new MCP tools free of binding decisions, membership changes, and authority selectors
6. THE SYSTEM SHALL keep collision assessment deterministic and free of model calls

## 16. Milestone validation

### Acceptance criteria

1. WHEN two participants plan changes in the shared fixture without exchanging subject strings
   THE SYSTEM SHALL open a `contract_producer_consumer` assessment and a `shared_declared_invariant` assessment before either participant writes a file
2. WHEN the second participant's agent is about to write a conflicting file
   THE SYSTEM SHALL warn without blocking
3. WHEN the first participant submits the next prompt
   THE SYSTEM SHALL report the new collision once
4. WHEN a Human dismisses a collision and an Intent is revised without new matched subjects
   THE SYSTEM SHALL keep the collision dismissed
5. THE SYSTEM SHALL demonstrate the scenario in two real Kiro sessions and record sanitized results
