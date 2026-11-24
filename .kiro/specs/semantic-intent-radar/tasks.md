# Tasks

- [x] 1. Review and approve the semantic intent and Radar boundaries
  - Confirm that agents may publish, revise, and release their own Intents immediately under an authenticated human-bound Participant identity.
  - Confirm that agents may only propose ResolutionProposals and cannot make binding decisions, change objectives, accept charters, or alter project membership.
  - Confirm local-first repository analysis, normalized-claim transmission, no raw prompts/source by default, advisory collision posture, and Conn as the objective source of truth after import.
  - Confirm that the protocol supports multiple repositories and participants without committing to immediate adapters for every host, language, or agent.
  - Do not implement a browser application, remote MCP, policy engine, GitHub App, or hosted analysis in this specification's first vertical slice.

- [x] 2. Define and review the v1 semantic coordination contracts
  - Add Draft 2020-12 schemas for RepositoryConnection, Participant, Intent input and record, IntentRevision, SemanticClaim, local analyzer report, CollisionAssessment, ConflictContextPack, ResolutionProposal, ResolutionDecision, EvidenceReference, RadarView, and Objective import input.
  - Define bounded enums for claim kinds, relations, provenance, collision severity, resolution kinds, evidence states, and event types.
  - Reject unknown authoritative properties, raw source fields, raw prompt fields, credential-shaped fields, and unbounded diagnostics.
  - Add representative accepted and rejected shared fixtures, including static-analysis and declared provenance distinctions.
  - Validate the fixtures through the existing Go and Node.js contract validators before persistent implementation.
  - Stop for schema and vocabulary review before adding migrations or public endpoints.

- [x] 3. Record implementation decisions that the contracts require
  - Compare whether claims are immutable records attached to an immutable IntentRevision or de-duplicated records with immutable revision associations.
  - Compare direct local Node subprocess invocation with a future local adapter boundary for TypeScript analysis.
  - Select the smallest design consistent with the existing one-binary Go architecture and local-first privacy requirement.
  - Record approved decisions under `docs/decisions/` before implementation depends on them.
  - Do not select a browser framework, graph database, vector store, workflow engine, OPA, GitHub App SDK, or remote MCP transport in this task.

- [x] 4. Add semantic ledger persistence and atomic events
  - Add Conn-owned Goose migrations and PostgreSQL constraints for RepositoryConnections, Participants, Intents and immutable IntentRevisions, Claims, CollisionAssessments, ResolutionProposals, ResolutionDecisions, EvidenceReferences, and their required associations.
  - Enforce Project scope, Human-to-Participant ownership, revision ordering, project-scoped foreign keys, and cross-project collision rejection.
  - Add append-only event records for repository connection, intent, claim, collision, resolution, evidence, and import changes.
  - Preserve the existing gap-free per-Project event-sequence and atomic state-plus-event transaction guarantees.
  - Add only focused persistence scenarios for fundamental constraints and atomic rollback.

- [x] 5. Implement RepositoryConnection and Participant application operations
  - Add owner-authorized repository connect/list operations, initially accepting a normalized generic Git remote and optional immutable host reference.
  - Add a locally derived Git metadata path that never sends credential-bearing remote URLs.
  - Create or resolve the caller's Participant from the authenticated Conn Human, adapter kind, and bounded opaque session reference.
  - Ensure participant identity cannot be chosen or overridden through public request fields.
  - Add HTTP client, CLI, and status-read support without direct presentation-to-PostgreSQL access.

- [x] 6. Implement direct agent Intent publication with bounded authority
  - Add application, persistence, HTTP, and CLI operations to publish, revise, list, read, and release Intents.
  - Require one exact Objective revision and optional Blueprint and Charter references on every IntentRevision.
  - Preserve immutable Intent revisions and apply optimistic concurrency to revisions.
  - Allow the authenticated Participant to change only its own Intent.
  - Compare an Intent with an associated accepted Charter and surface divergence as a coordination observation rather than an unauthorized implicit boundary change.
  - Add focused authorization scenarios proving that an agent participant can publish initially but cannot impersonate another Human or modify human-authoritative records.

- [x] 7. Implement the deterministic claim and collision assessor
  - Normalize and persist declared SemanticClaims from Intent input.
  - Build a bounded, idempotent assessor over active Intent revisions in the same Project and relevant repository or declared-concept scope.
  - Implement shared declared invariant, shared exported symbol, contract producer-consumer, and schema-resource overlap rules.
  - Store matching claims, rule identifier, severity, confidence, explanation data, and lifecycle state for every assessment.
  - Treat same-file or same-path information as supporting context only.
  - Add the fundamental two-intent scenarios for each rule, a same-file-only non-collision scenario, release/staleness behavior, and explanation provenance.

- [x] 8. Implement the local TypeScript analyzer boundary
  - Replace the `packages/typescript-analyzer` placeholder with a bounded TypeScript compiler API implementation that resolves configured TypeScript projects, exported declarations, imports, and limiting diagnostics.
  - Define stable symbol identifiers and safe source-location evidence references without emitting source text or excerpts.
  - Add a local CLI invocation path that validates analyzer output against the canonical report schema before authenticated submission.
  - Attach observed claims with `static_analysis` provenance and retain analyzer diagnostics as explicit unknown or incomplete context.
  - Prove through focused tests that reports containing source, prompt-like input, credential-shaped values, or oversized diagnostics are refused or redacted.
  - Do not introduce a hosted source index, repository upload, Tree-sitter dependency, model inference, or a persistent sidecar.

- [x] 9. Implement the Radar read model and terminal/CLI views
  - Add one application read operation that assembles the governing objective, active blueprint, repositories, active intents, claims, collisions, dependencies, decisions, assumptions, questions, evidence coverage, and known unknowns at a shared `as_of_event` cursor.
  - Add `conn radar`, `conn conflicts`, and `conn explain` commands with human and `--json` rendering.
  - Add a Radar section and focused drill-down states to `conn app`, preserving keyboard operation, explicit labels, and color-independent status text.
  - Ensure missing, stale, inaccessible, model-inferred, and disputed information is explicit in every surface.
  - Add one read-model scenario proving no active work and missing evidence are shown as absence, not inferred project completion.

- [x] 10. Implement the decentralized resolution protocol and context pack
  - Add ConflictContextPack generation as bounded JSON and Markdown from the same authoritative Radar and collision records.
  - Include objective and charter references, Intent versions, claims, evidence, uncertainty labels, existing decisions, unresolved questions, and allowed proposal kinds.
  - Add participant-authored ResolutionProposal operations and human-authorized ResolutionDecision operations.
  - Implement dependency, sequencing, contract agreement, accepted risk, false positive, objective revision, and blueprint revision proposal/decision vocabulary.
  - Require human authorization and rationale for binding contract decisions, accepted risk, and approved-boundary changes.
  - Add `conn conflict context` and `conn resolution decide` commands.
  - Verify that an agent proposal remains non-binding and a human decision is immutable, attributable, and evented.

- [x] 11. Extend the Kiro MCP adapter
  - Keep all existing read tools compatible.
  - Add `publish_intent`, `revise_intent`, `release_intent`, `check_collisions`, `read_radar`, `read_conflict_context`, `propose_resolution`, and bounded `attach_evidence` tools.
  - Derive human authority from the authenticated Conn session and reject `human_id` authority inputs.
  - Keep MCP on stdio, preserve standard-output protocol isolation, and route calls through the existing authenticated HTTP client.
  - Add subprocess scenarios for agent intent publication, collision context retrieval, non-binding resolution proposal, and forbidden authority escalation.
  - Do not add MCP configuration, remote MCP transport, a custom agent, or automatic hooks in this task.

- [x] 12. Implement objective imports and evidence references
  - Add a human-authorized Kiro-spec import path that creates or proposes a versioned Conn Objective with source path/reference and imported-content hash.
  - Add a GitHub-issue import boundary that can be enabled only after GitHub repository authorization is selected and implemented.
  - Ensure external-source changes never mutate a Conn Objective automatically; refresh creates a new proposed or authorized Objective revision.
  - Add participant-authorized bounded evidence attachment and Radar evidence-coverage calculation against keyed acceptance criteria.
  - Preserve safe links, identifiers, hashes, and summaries rather than raw logs, traces, screenshots, or source artifacts.
  - Add import immutability and evidence-state scenarios.

- [ ] 13. Validate the semantic coordination vertical slice
  - Create a reproducible fixture TypeScript repository or controlled local worktree with concurrent Intents that independently pass while sharing an invariant, exported symbol, contract relationship, or schema resource.
  - Run multiple authenticated Kiro participants against one or more Project RepositoryConnections.
  - Confirm agents publish initial Intents directly, Conn detects only supported semantic overlaps, the Radar explains project state and unknowns, and a human can use a context pack with an agent of their choice to record a resolution.
  - Confirm raw source, prompts, transcripts, and secrets do not enter Conn records or diagnostics by default.
  - Run `pnpm check`, `pnpm test`, `pnpm build`, and frozen dependency installation.
  - Document observed warning precision, unresolved usability questions, and data-minimization gaps before considering GitHub App checks or additional analyzers.

- [ ] 14. Plan, but do not implement, the GitHub App and later adapters
  - After the local vertical slice is validated, prepare a separate decision and specification for a least-privilege GitHub App, installation binding, webhook signature verification, delivery deduplication, pull-request checks, and CI evidence references.
  - Prepare separate specifications for OpenAPI extraction, SQL schema extraction, browser Radar, GitLab, issue trackers, notifications, remote MCP, and policy enforcement only when validated user needs justify them.
  - Do not let later integrations alter the authoritative semantic contracts or bypass human authority.
