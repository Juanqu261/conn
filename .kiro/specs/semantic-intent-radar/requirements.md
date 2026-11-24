# Requirements

## 1. Shared semantic project radar

**User story:** As a human responsible for a software project, I want Conn to explain the current objectives, active work, assumptions, semantic interactions, decisions, and evidence so that I can understand project state without reconstructing it from agent chats, branches, and pull requests.

### Acceptance criteria

1. WHEN an authorized project member requests the Radar view
   THE SYSTEM SHALL return the current Objective revisions, active BlueprintRevision, active and recently released Intents, open Questions, active CollisionAssessments, recorded Resolutions, and evidence coverage
2. WHEN the Radar presents a conclusion about a collision or gap
   THE SYSTEM SHALL identify the underlying claims, their source, confidence where applicable, and the exact objective, intent, code, contract, schema, or evidence references that support it
3. WHEN the available information is incomplete, stale, inferred, or disputed
   THE SYSTEM SHALL label that condition plainly and SHALL NOT present it as an established project fact
4. WHEN a project has no active work or no recorded evidence
   THE SYSTEM SHALL present that absence explicitly rather than infer progress or completion
5. WHEN human-readable project state is requested from the CLI, terminal application, HTTP API, or MCP
   THE SYSTEM SHALL derive it from the same authoritative read model
6. THE SYSTEM SHALL center the Radar on project understanding and coordination and SHALL NOT produce individual productivity scores, agent rankings, or surveillance metrics

## 2. Agent-published change intent with bounded authority

**User story:** As a human working with a coding agent, I want my agent to publish its initial change intent directly so that the team receives coordination information before implementation without requiring a manual confirmation for every routine task.

### Acceptance criteria

1. WHEN an authenticated Conn participant invokes the intent-publish protocol through MCP or another authorized adapter
   THE SYSTEM SHALL allow the participant to create, revise, and release its own Intent without a per-intent human confirmation
2. WHEN Conn records an agent-published Intent
   THE SYSTEM SHALL attribute it to both the authenticated human and the bounded participant identity, including adapter type and an opaque local session reference where available
3. WHEN an Intent references an Objective or Charter
   THE SYSTEM SHALL preserve the exact referenced revision and SHALL NOT silently move the Intent to a later revision
4. WHEN a participant has an accepted Charter for the referenced Objective
   THE SYSTEM SHALL compare the Intent to that Charter and report scope divergence as coordination information
5. WHEN an agent attempts to change an Objective, BlueprintRevision, Charter acceptance, membership, high-risk policy, or an accepted conflict Resolution
   THE SYSTEM SHALL reject the operation
6. WHEN an Intent is revised or released
   THE SYSTEM SHALL retain the preceding version and append a project event
7. WHEN Conn cannot authenticate the human identity behind an agent request
   THE SYSTEM SHALL reject the write and SHALL NOT accept a human identifier supplied in an MCP tool argument as authority

## 3. Privacy-preserving local repository analysis

**User story:** As a team with proprietary source code and prompts, I want Conn to derive useful impact claims locally by default so that the shared coordination ledger does not require raw source code or agent transcripts.

### Acceptance criteria

1. WHEN a supported repository is analyzed
   THE SYSTEM SHALL run the analyzer in the participant's local repository environment by default
2. WHEN local analysis completes
   THE SYSTEM SHALL transmit only the normalized claims, stable repository and revision references, source locations, content hashes where needed for verification, diagnostics, and explicitly attached evidence
3. THE SYSTEM SHALL NOT transmit raw repository source, raw prompts, model transcripts, access tokens, or unredacted test payloads by default
4. WHEN local analysis cannot run or cannot resolve a claim
   THE SYSTEM SHALL report an unknown or incomplete result rather than fabricate a relationship
5. WHEN a future hosted-analysis option is introduced
   THE SYSTEM SHALL require an explicit project-level opt-in and document the additional data categories before enabling it

## 4. Portable semantic claim protocol

**User story:** As a team using different repositories, agents, and development tools, I want one versioned vocabulary for semantic claims so that Conn can coordinate across adapters without requiring a new IDE or agent runtime.

### Acceptance criteria

1. THE SYSTEM SHALL model declared and observed claims for behavior, invariant, contract, data resource, infrastructure resource, code symbol, dependency, assumption, question, and acceptance criterion
2. WHEN a claim is stored
   THE SYSTEM SHALL include its kind, normalized subject, source category, confidence where the source is inferential, producer identity, observed time, and evidence references
3. WHEN two adapters use different native identifiers for the same repository concept
   THE SYSTEM SHALL preserve each native reference and may associate them only through an explainable normalized relationship
4. WHEN a language, repository host, or analyzer is unsupported
   THE SYSTEM SHALL still allow a participant to publish declared claims through the common protocol
5. THE SYSTEM SHALL keep protocol records transport-neutral so that MCP, CLI, GitHub, GitLab, issue trackers, CI systems, and future adapters can use the same contracts

## 5. Deterministic collision assessment

**User story:** As a participant beginning concurrent work, I want Conn to identify explainable semantic interactions so that I can coordinate before independent changes become an integration problem.

### Acceptance criteria

1. WHEN an active Intent is created, revised, analyzed, or receives new evidence
   THE SYSTEM SHALL compare it with the other active Intents in the same Project and any explicitly connected repository scope
2. THE SYSTEM SHALL initially assess declared-invariant overlap, exported-symbol overlap, declared or observed contract producer-consumer overlap, and database-schema or migration overlap
3. WHEN an assessment is created
   THE SYSTEM SHALL include both Intent versions, the matching claims, the rule that matched, severity, confidence, and a plain-language explanation
4. WHEN the only relationship is a shared path or file without a supported semantic claim
   THE SYSTEM SHALL present it as context and SHALL NOT classify it as a semantic collision
5. WHEN a claim came from model inference
   THE SYSTEM SHALL NOT make that inference the sole basis for a blocking or high-risk assessment
6. WHEN no supported evidence establishes a collision
   THE SYSTEM SHALL return no collision or an explicitly low-confidence observation; it SHALL NOT manufacture a warning
7. THE SYSTEM SHALL begin in advisory mode and SHALL NOT block repository writes, pull requests, or merges solely because of a CollisionAssessment

## 6. Decentralized conflict-resolution protocol

**User story:** As a participant in a collision, I want Conn to supply the shared facts and a portable resolution context so that I can resolve the issue with the human and coding agent of my choice rather than through a master agent.

### Acceptance criteria

1. WHEN Conn detects a CollisionAssessment
   THE SYSTEM SHALL make an immutable context pack available through CLI, MCP, and the HTTP API
2. THE context pack SHALL include the Objective and Charter context, each Intent's declared scope and assumptions, overlapping claims, evidence, uncertainty labels, open Questions, and permitted Resolution options
3. THE SYSTEM SHALL support proposals for dependency, sequencing, contract agreement, accepted risk, false-positive classification, and escalation to an Objective or Blueprint revision
4. WHEN an agent proposes a Resolution
   THE SYSTEM SHALL record it as an unaccepted proposal and SHALL NOT treat it as a final team decision
5. WHEN a Resolution changes an accepted objective boundary, accepts a high-risk collision, or records a binding contract decision
   THE SYSTEM SHALL require an authorized human decision and append the rationale to the event history
6. WHEN participants resolve a collision in an external agent session or meeting
   THE SYSTEM SHALL allow an authorized human to record the decision, rationale, and external references without storing the raw conversation
7. THE SYSTEM SHALL NOT assign work, command participants, or select an agent to resolve a collision

## 7. Objective imports and immutable source provenance

**User story:** As a team using GitHub issues and Kiro specifications, I want Conn to import objectives from existing tools while keeping a durable, reviewable record of the objective version that authorized work.

### Acceptance criteria

1. WHEN a human imports a Kiro specification or GitHub issue
   THE SYSTEM SHALL create or propose a Conn Objective revision with the external source reference and imported-content hash
2. WHEN an external ticket or specification changes after import
   THE SYSTEM SHALL NOT silently mutate the Conn Objective revision
3. WHEN a human elects to refresh imported content
   THE SYSTEM SHALL create a new Objective revision subject to Conn's existing authority and revision rules
4. WHEN an imported Objective is displayed
   THE SYSTEM SHALL show the source system, stable reference, import time, and whether Conn has since diverged from the external source
5. THE SYSTEM SHALL treat Conn's accepted Objective revisions as the coordination source of truth after import

## 8. Evidence coverage and project understanding

**User story:** As a reviewer or project owner, I want evidence connected to acceptance criteria and active decisions so that Conn can identify what remains unverified without pretending that an agent's summary is proof.

### Acceptance criteria

1. WHEN a participant, CI system, or repository integration attaches evidence
   THE SYSTEM SHALL link it to exact Intent, Objective revision, acceptance criterion, commit, pull request, build, test, trace, or artifact references as applicable
2. WHEN evidence is displayed
   THE SYSTEM SHALL distinguish passing, failing, missing, stale, inaccessible, and unknown evidence states
3. WHEN an acceptance criterion has no relevant attached evidence
   THE SYSTEM SHALL show it as unverified
4. THE SYSTEM SHALL store references, hashes, summaries, and access-controlled links by default and SHALL NOT require large logs, screenshots, traces, or source code to enter the Conn database
5. WHEN evidence comes from CI or a Git host webhook
   THE SYSTEM SHALL authenticate the integration, deduplicate deliveries, and retain enough provenance to explain the result

## 9. Initial adapter and scalability boundaries

**User story:** As a product owner, I want the semantic protocol to support more than a single scripted experiment without prematurely claiming universal language or tool support.

### Acceptance criteria

1. THE SYSTEM SHALL not impose a protocol-level limit of one repository, one objective, three participants, or three Kiro sessions per Project
2. THE SYSTEM SHALL retain Kiro as the first coding-agent adapter and GitHub as the first repository-host adapter
3. THE SYSTEM SHALL implement TypeScript symbol analysis as the first observed-claim analyzer while retaining declared-claim support for every repository
4. THE SYSTEM SHALL allow one Project to bind multiple RepositoryConnections and SHALL scope assessments to explicit repository relationships, Objective relationships, or declared shared concepts
5. THE SYSTEM SHALL defer executable adapters for other coding agents, GitLab, Jira, Linear, Slack, Teams, and a browser application until the common protocol and Kiro/GitHub paths prove useful
6. THE SYSTEM SHALL NOT introduce a dedicated desktop application, central agent orchestrator, autonomous merge capability, or employee analytics in this slice

## 10. Security, consistency, and observability

**User story:** As an operator, I want semantic coordination records to be trustworthy and safely attributable while remaining understandable to project members.

### Acceptance criteria

1. WHEN an authoritative Intent, Claim, CollisionAssessment, Resolution, or Evidence reference changes
   THE SYSTEM SHALL commit the state change and its project event atomically
2. WHEN concurrent updates target the same versioned Intent or Resolution
   THE SYSTEM SHALL use optimistic concurrency and return a conflict rather than overwrite data
3. WHEN a GitHub webhook or CI delivery is retried
   THE SYSTEM SHALL process the delivery idempotently
4. WHEN Conn logs an operation
   THE SYSTEM SHALL record safe identifiers, operation category, outcome, duration, and request identifier, and SHALL NOT log raw source, prompts, tokens, or request bodies by default
5. WHEN a user lacks Project membership
   THE SYSTEM SHALL not reveal project, intent, collision, or evidence existence
6. WHEN the Radar exposes AI-derived information
   THE SYSTEM SHALL label the producer and confidence and provide the static or declared evidence available for human review
