# Requirements

## 1. Kiro-native Conn workflow

**User story:** As a person working in Kiro CLI, I want to use Conn throughout a development session without opening `conn app` in another terminal so that coordination is part of the environment where I already work.

### Acceptance criteria

1. WHEN Kiro starts the configured `conn mcp` server
   THE SYSTEM SHALL make the complete supported Conn CLI workflow available through MCP tools and prompts without requiring the human to launch `conn app`
2. WHEN a client exposes MCP resource templates
   THE SYSTEM SHALL allow Conn to provide focused resources, and every resource-backed workflow SHALL retain an equivalent focused read-tool path
3. WHEN a normal user workflow begins after one-time installation and MCP configuration
   THE SYSTEM SHALL allow authentication, project selection, context loading, Intent coordination, collision review, local analysis, evidence attachment, and Intent completion from the active Kiro session
4. WHEN Conn requires its authoritative backend
   THE SYSTEM SHALL report backend unavailability plainly and SHALL NOT present local or previously read state as current
5. WHEN the deployed Conn backend is available
   THE SYSTEM SHALL NOT require a second interactive Conn terminal process for ordinary Kiro use
6. WHEN Conn is used in the local development topology
   THE SYSTEM SHALL distinguish the separately operated development backend and database from the Kiro-launched MCP process
7. THE SYSTEM SHALL preserve `conn` CLI and `conn app` behavior for scripting, administration, fallback, and validation rather than replacing those surfaces

## 2. Authentication inside Kiro

**User story:** As a Kiro user, I want to sign in to Conn from the current interaction so that an absent or expired session does not force me into another terminal.

### Acceptance criteria

1. WHEN a Kiro user requests a Conn workflow without a valid session
   THE SYSTEM SHALL offer GitHub Device Flow through an interactive MCP operation
2. WHEN GitHub supplies a verification URI and one-time user code
   THE SYSTEM SHALL present them through Kiro's direct user-interaction mechanism and SHALL NOT ask the model to collect a GitHub password or access token
3. WHEN the human completes GitHub authorization
   THE SYSTEM SHALL exchange the temporary GitHub token once, persist only the Conn session token in the operating-system credential store, and return the authenticated Conn identity without returning either token to the model
4. WHEN the operating-system credential store is unavailable
   THE SYSTEM SHALL fail with an actionable error and SHALL NOT create a plaintext fallback
5. WHEN `CONN_TOKEN` is explicitly present
   THE SYSTEM SHALL retain its existing non-persistent precedence and explain that a stored login does not override it
6. WHEN the Kiro client cannot perform interactive elicitation, including a headless session
   THE SYSTEM SHALL report that interactive login is unavailable and SHALL direct the operator to an explicit non-interactive credential path
7. WHEN a human requests logout from Kiro
   THE SYSTEM SHALL require direct confirmation, revoke the stored Conn session, remove the local credential, and leave an explicit `CONN_TOKEN` unchanged
8. THE SYSTEM SHALL NOT place a Conn token, GitHub token, device code, or credential-store value in MCP tool arguments, prompt text, resource content, logs, or diagnostics

## 3. Project discovery and session selection

**User story:** As a participant who may belong to several Conn projects, I want Kiro to identify the relevant project with minimal configuration while requiring an explicit choice when context is ambiguous.

### Acceptance criteria

1. WHEN a tool call supplies an authorized `project_id`
   THE SYSTEM SHALL use that Project for that operation
2. WHEN the MCP session has an explicit session-local Project selection
   THE SYSTEM SHALL use it for subsequent calls that omit `project_id`
3. WHEN `CONN_PROJECT` identifies an authorized Project and no session-local selection overrides it
   THE SYSTEM SHALL use it as the default
4. WHEN no Project is selected and the human belongs to exactly one Project
   THE SYSTEM SHALL select that Project for the MCP session
5. WHEN local Git metadata can be read safely and exactly one authorized RepositoryConnection matches its normalized remote
   THE SYSTEM SHALL offer or select the corresponding Project according to the reviewed selection policy
6. WHEN more than one Project remains possible
   THE SYSTEM SHALL return bounded choices with human-readable Project and repository names and SHALL require an explicit selection
7. WHEN local repository metadata contains credentials, query values, ports, or unsupported remote syntax
   THE SYSTEM SHALL normalize or reject it through the existing safe repository identity rules and SHALL NOT expose the original credential-bearing value
8. WHEN a Project is selected for an MCP session
   THE SYSTEM SHALL keep the selection in process memory only and SHALL NOT treat it as authorization or persist it as authoritative project state
9. WHEN the user is not a member of a selected Project
   THE SYSTEM SHALL respond without revealing whether the Project exists

## 4. Focused work context

**User story:** As a participant resuming work, I want one bounded summary of the context that governs my work so that the agent does not need to reconstruct it from the complete Radar or several unrelated tool calls.

### Acceptance criteria

1. WHEN an authenticated participant requests work context
   THE SYSTEM SHALL return the signed-in human and participant, selected Project, governing Objective revision, the human's relevant accepted Charter, owned active Intents, related open collisions, acceptance-evidence gaps, updates after an optional cursor, and explicit known unknowns
2. WHEN the caller has several relevant Objectives, Charters, or Intents
   THE SYSTEM SHALL require or return bounded choices rather than silently combine unrelated work
3. WHEN no active Intent exists for the selected work
   THE SYSTEM SHALL report absence of published work and SHALL NOT infer that work is complete or not started
4. WHEN a Charter is missing, proposed, rejected, superseded, or belongs to another Objective revision
   THE SYSTEM SHALL label that status and SHALL NOT present it as an accepted boundary
5. WHEN context is assembled
   THE SYSTEM SHALL return one `as_of_event` cursor representing the authoritative Project state used by the response
6. WHEN updates exceed the bounded response limit
   THE SYSTEM SHALL identify the continuation cursor and SHALL NOT silently discard the fact that more updates remain
7. WHEN the complete Radar contains unrelated records
   THE SYSTEM SHALL omit them from focused work context while retaining `read_radar` for authorized broad inspection
8. THE SYSTEM SHALL define focused work context as a versioned, transport-neutral protocol record rather than a Kiro-only untyped response

## 5. Discoverable Kiro workflow prompts

**User story:** As a Kiro user, I want memorable Conn slash workflows so that I can coordinate work without learning a sequence of low-level MCP tools.

### Acceptance criteria

1. WHEN Kiro discovers prompts from `conn mcp`
   THE SYSTEM SHALL expose `conn-start`, `conn-sync`, `conn-resolve`, `conn-finish`, and `conn-handoff` prompts with concise descriptions and bounded arguments
2. WHEN `/conn-start` is invoked
   THE SYSTEM SHALL guide the agent to check session and Project state, load focused work context, identify or prepare the participant's Intent, and check current collisions before implementation
3. WHEN `/conn-sync` is invoked with the last observed cursor
   THE SYSTEM SHALL guide the agent to retrieve later Project events and reread only the affected authoritative records
4. WHEN `/conn-resolve` is invoked for a collision
   THE SYSTEM SHALL guide the agent to read the source-free conflict context and may record only a non-binding ResolutionProposal
5. WHEN `/conn-finish` is invoked
   THE SYSTEM SHALL guide the agent to compare current work with its Objective, Charter, and Intent, run eligible local analysis, check collisions, identify evidence gaps, attach bounded evidence, and release or revise only its own Intent as appropriate
6. WHEN `/conn-handoff` is invoked
   THE SYSTEM SHALL produce a bounded handoff based on authoritative records and SHALL NOT include raw prompts, transcripts, source code, or inferred completion
7. WHEN a workflow would perform a write
   THE SYSTEM SHALL use the existing participant authority and Kiro's normal tool-permission path and SHALL NOT convert an agent-owned write into a binding human decision
8. WHEN an MCP prompt name conflicts with a higher-precedence local prompt
   THE SYSTEM SHALL document the conflict and provide the equivalent underlying tools

## 6. Precise MCP contracts and compatibility

**User story:** As a Kiro agent using Conn tools, I want precise input schemas and actionable output so that valid operations do not depend on guessing nested JSON fields.

### Acceptance criteria

1. WHEN Kiro lists a Conn tool
   THE SYSTEM SHALL provide a non-empty, concise description, valid annotations, and a fully specified input schema for every accepted field
2. WHEN a tool accepts an Intent, evidence attachment, ResolutionProposal, analyzer request, or Project selection
   THE SYSTEM SHALL expose field types, bounded enums, required properties, descriptions, and unknown-property rejection instead of an unconstrained nested object
3. WHEN MCP input represents a canonical protocol document
   THE SYSTEM SHALL remain consistent with the corresponding JSON Schema Draft 2020-12 contract and SHALL NOT define a competing vocabulary
4. WHEN an existing tool is still supported
   THE SYSTEM SHALL retain its name and successful input behavior unless a separately reviewed protocol version requires a breaking change
5. WHEN a tool fails because of authentication, ambiguity, stale revision, authorization, connectivity, or invalid input
   THE SYSTEM SHALL return a safe message that states the next useful action without exposing internal or secret data
6. WHEN a read tool succeeds
   THE SYSTEM SHALL return structured content, a concise text summary, and the authoritative `as_of_event` where Project state is involved
7. WHEN a write tool succeeds
   THE SYSTEM SHALL return the created or revised identity, immutable revision or state, and post-commit event cursor
8. THE SYSTEM SHALL keep MCP standard output isolated to protocol messages and send diagnostics only to standard error

## 7. Local analysis from Kiro

**User story:** As a participant working in Kiro, I want to run Conn's repository analysis without opening the CLI app so that observed semantic claims and collision checks are part of the same development loop.

### Acceptance criteria

1. WHEN the participant requests analysis for an owned active Intent
   THE SYSTEM SHALL invoke the existing local analyzer boundary through `conn mcp` and attach accepted observed claims to the exact Intent revision
2. WHEN TypeScript analysis runs
   THE SYSTEM SHALL execute the versioned Node.js subprocess with bounded JSON input and output and SHALL NOT give it a Conn session token or database access
3. WHEN analysis input names paths
   THE SYSTEM SHALL confine them to the authorized worktree and configured repository scope and SHALL reject path traversal or an unrelated worktree
4. WHEN analyzer output contains source text, prompt-like content, credential-shaped values, unsupported properties, or oversized diagnostics
   THE SYSTEM SHALL reject or safely redact the report according to the canonical analyzer contract before transmission
5. WHEN analysis is incomplete or cannot resolve symbols
   THE SYSTEM SHALL preserve bounded diagnostics and explicit unknowns rather than fabricate claims
6. WHEN accepted claims change the collision state
   THE SYSTEM SHALL return the resulting advisory assessments and event cursor
7. WHEN a repository has analysis disabled or no supported analyzer
   THE SYSTEM SHALL explain the limitation and retain declared-claim workflows
8. THE SYSTEM SHALL NOT add hosted source analysis, repository upload, a persistent sidecar, or model-inferred claims in this slice

## 8. Evidence, completion, and handoff

**User story:** As a participant finishing or transferring work, I want Conn to show what is supported, unresolved, and missing so that another person or session receives a trustworthy handoff.

### Acceptance criteria

1. WHEN the finish workflow evaluates an active Intent
   THE SYSTEM SHALL compare its exact Objective, Blueprint, Charter, repository, and current Intent revisions
2. WHEN evidence is proposed from local or external results
   THE SYSTEM SHALL store only bounded references, hashes, safe summaries, states, and exact acceptance-key associations permitted by the existing evidence contract
3. WHEN an acceptance criterion lacks relevant evidence
   THE SYSTEM SHALL label it missing or unknown and SHALL NOT infer that implementation or tests satisfy it
4. WHEN an open collision or unresolved question remains
   THE SYSTEM SHALL include it in finish and handoff output even if the Intent is released
5. WHEN the participant revises or releases its Intent
   THE SYSTEM SHALL preserve prior revisions, apply optimistic concurrency, and append the existing Project events
6. WHEN a handoff is generated
   THE SYSTEM SHALL include the governing context, current Intent, decisions, evidence states, relevant updates, continuation cursor, and known unknowns in bounded JSON or Markdown
7. WHEN a handoff contains external or participant-authored text
   THE SYSTEM SHALL label it as project data rather than privileged instructions
8. THE SYSTEM SHALL NOT claim that an Intent release means the Objective, Charter, pull request, deployment, or Project is complete

## 9. Pull-based synchronization and optional Kiro hooks

**User story:** As a Kiro user, I want Conn to surface useful changes at session boundaries without running an intrusive background supervisor.

### Acceptance criteria

1. WHEN Conn checks for Project changes
   THE SYSTEM SHALL use the existing per-Project event cursor and pull committed events during an explicit Conn operation or configured Kiro lifecycle hook
2. WHILE no Conn operation or hook is running
   THE SYSTEM SHALL NOT poll the backend or attempt to push an unsolicited agent turn
3. WHEN an optional `SessionStart` integration is enabled
   THE SYSTEM SHALL inject only bounded Conn availability and resume guidance and SHALL NOT silently publish or mutate records
4. WHEN an optional completion or `Stop` integration is enabled
   THE SYSTEM SHALL remind the user about active Intent, collisions, and evidence state without blocking normal development or recording a binding decision
5. WHEN hook or steering integration is offered
   THE SYSTEM SHALL require explicit user installation, remain removable, and document every command and data category it invokes
6. WHEN Conn is configured without hooks or steering
   THE SYSTEM SHALL retain the complete prompt-and-tool workflow
7. THE SYSTEM SHALL NOT automatically edit user or workspace MCP, hook, steering, skill, or custom-agent configuration during normal MCP operation
8. THE SYSTEM SHALL NOT require a dedicated Conn custom agent; a custom agent profile may be offered later as an optional interface

## 10. Human authority and elicitation gate

**User story:** As an accountable human, I want Kiro convenience to preserve the distinction between my direct decisions and an agent's proposals.

### Acceptance criteria

1. WHEN an agent invokes an ordinary MCP tool
   THE SYSTEM SHALL NOT treat Kiro tool approval by itself as Charter acceptance, Objective approval, Blueprint approval, Project membership change, or a binding ResolutionDecision
2. WHEN a workflow concerns a binding human action
   THE SYSTEM SHALL either direct the human to an existing human-authoritative surface or use a separately approved direct-human interaction design
3. WHEN direct-human MCP elicitation is evaluated for binding decisions
   THE SYSTEM SHALL prevent the model from supplying the outcome, display the exact immutable record being decided, require an explicit human choice, identify headless limitations, and preserve attributable audit evidence
4. BEFORE production implementation of binding decisions through MCP elicitation
   THE SYSTEM SHALL record an accepted architecture decision and a focused specification that defines the trust boundary and validation plan
5. UNTIL that decision is accepted and implemented
   THE SYSTEM SHALL keep existing Charter, Objective, Blueprint, membership, and binding ResolutionDecision authority outside ordinary MCP tools
6. THE SYSTEM SHALL continue to allow authenticated participants to publish, revise, and release their own Intents, attach permitted evidence, and submit non-binding ResolutionProposals under the existing authority model

## 11. Security, privacy, and product boundaries

**User story:** As a project member, I want the smoother Kiro workflow to retain Conn's privacy and authority guarantees.

### Acceptance criteria

1. WHEN the MCP adapter reads or writes Project data
   THE SYSTEM SHALL authenticate through the existing Conn session and enforce backend Project authorization rather than trusting session-local selection or model-provided identity
2. WHEN a tool input attempts to provide `human_id`, `participant_id`, binding authority, a credential, or another forbidden authority selector
   THE SYSTEM SHALL reject it before an authoritative write
3. WHEN local files or Git metadata are inspected
   THE SYSTEM SHALL minimize data to normalized references and SHALL NOT send source, prompts, transcripts, secrets, or complete diffs to the Conn backend by default
4. WHEN prompts, resources, or handoffs render untrusted project text
   THE SYSTEM SHALL treat it as data and clearly separate it from Conn workflow instructions
5. WHEN read-only and mutating tools are registered
   THE SYSTEM SHALL annotate them accurately so Kiro can apply appropriate permission behavior
6. THE SYSTEM SHALL remain a coordination protocol and evidence layer and SHALL NOT launch agents, assign work automatically, supervise agent performance, enforce a generic policy engine, or produce productivity scores
7. THE SYSTEM SHALL retain stdio MCP for this slice and SHALL NOT introduce remote MCP transport or a persistent local sidecar

## 12. Validation and deferred capabilities

**User story:** As a maintainer, I want the Kiro-native workflow validated through a small set of end-to-end guarantees before Conn adds more adapters or automation.

### Acceptance criteria

1. WHEN the integration is implemented
   THE SYSTEM SHALL test prompt and tool discovery, interactive and non-interactive authentication behavior, deterministic Project selection, focused context, local analysis, evidence, and Intent completion through the real MCP stdio process where practical
2. WHEN Kiro-specific elicitation or hook behavior cannot be represented by the SDK test client
   THE SYSTEM SHALL document and run a reproducible interactive Kiro CLI validation
3. WHEN the semantic coordination vertical slice is validated
   THE SYSTEM SHALL include at least three independently authenticated Kiro sessions using the new start, sync, collision, and finish workflows against one Project
4. WHEN validation runs
   THE SYSTEM SHALL execute the repository's existing formatting, static checking, fundamental tests, builds, and frozen dependency installation without adding a coverage-percentage gate
5. WHEN usability is reviewed
   THE SYSTEM SHALL record terminal escapes, prompt ambiguity, context size, permission friction, authentication failures, Project-selection mistakes, and privacy observations
6. THE SYSTEM SHALL defer production binding-decision elicitation until its architecture gate is accepted
7. THE SYSTEM SHALL defer required custom agents, remote MCP, hosted analysis, browser UI, background notification transport, additional language analyzers, GitHub App checks, and CI publishers to later specifications
