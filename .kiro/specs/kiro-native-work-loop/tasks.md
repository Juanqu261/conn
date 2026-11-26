# Tasks

- [x] 1. Review and approve the Kiro-native work-loop scope
  - Confirm that ordinary Kiro use should not require launching `conn app` after one-time binary and MCP setup, while the authoritative backend remains required.
  - Confirm that MCP tools and prompts form the required integration core, that every optional resource has a focused read-tool fallback, and that hooks and steering are optional additions.
  - Confirm that the normal Kiro agent remains supported and a custom Conn agent is not required.
  - Confirm that participants retain direct authority over their own Intents, evidence, and non-binding proposals, while binding human actions remain outside ordinary MCP tools.
  - Confirm that stdio MCP, pull-by-cursor synchronization, local source-free analysis, and the existing Go and Node.js boundaries remain in force.
  - Do not begin implementation until the specification and architecture gate are approved. Requirements: 1, 9, 10, 11, 12.

- [x] 2. Record the Kiro integration-surface architecture decision
  - Add an accepted decision under `docs/decisions/` comparing MCP-only integration, MCP with optional hooks and steering, and a required custom agent.
  - Record MCP tools and prompts as the stable core, with focused read-tool fallbacks for optional resources, optional inspectable hooks and steering, and no required custom agent.
  - Record that hooks perform no silent authoritative writes, no background polling, and no employee or agent supervision.
  - Record that normal `conn mcp` operation never edits user or workspace MCP, hook, steering, skill, or custom-agent configuration.
  - Define the validation and rollback plan for Kiro-specific integration artifacts.
  - Update the accepted-decision index only after approval. Requirements: 1, 5, 9, 11.

- [x] 3. Prove official MCP prompt, resource, and elicitation support
  - Build a focused test or spike against the currently pinned official MCP Go SDK for server prompts, resource templates, structured tool content, URL or form elicitation during an active stdio tool call, cancellation, and client capability detection.
  - Verify the behavior in a supported interactive Kiro CLI session, including slash prompt discovery; record the missing CLI resource-template selector and prove the focused read-tool fallback.
  - Confirm from official Kiro documentation that headless sessions have no mid-session input, define unfulfilled interactive input as a safe `interactive login unavailable` error, and record direct headless client behavior as an accepted validation limitation.
  - Record the negotiated Kiro protocol and capability object; for protocol `2026-07-28`, prove form and URL interaction through multi-round-trip `InputRequests`, echoed `InputResponses`, and validated `RequestState` rather than a nested `ServerSession.Elicit` call.
  - If the pinned SDK lacks a required capability, compare two or three practical SDK upgrade or bounded fallback options, record compatibility and operational cost, recommend one, and stop for approval before changing the dependency.
  - Do not implement a custom MCP transport, browser framework, or credential flow during the spike. Requirements: 2, 5, 12.

- [ ] 4. Define the focused work-context and Kiro workflow contracts
  - Add a Draft 2020-12 schema for the transport-neutral `WorkContext` view, including exact focus revisions, own Charters, owned active Intents, related collisions, evidence coverage, updates, continuation, known unknowns, and selection metadata.
  - Add only any small input or view schemas required for analyzer invocation or handoff that cannot be represented safely by existing canonical contracts.
  - Reject unknown authoritative properties, authority selectors, credentials, raw source, prompts, transcripts, and unbounded diagnostics.
  - Add representative shared accepted and rejected fixtures and validate them through the existing Go and Node.js contract validators.
  - Add matching handwritten Go protocol types and hold them to the canonical fixtures through the existing conformance test.
  - Stop for schema and vocabulary review before adding the backend work-context operation. Requirements: 4, 6, 7, 8, 11.

- [ ] 5. Replace weak MCP nested inputs with precise schemas
  - Replace unconstrained nested maps for Intent publication and revision, evidence attachment, and any other canonical input with typed wrappers or explicit MCP input schemas.
  - Preserve existing tool names, adapter-generated UUIDv7 behavior, nested ID defaulting, successful inputs, and participant authority.
  - Expose required fields, enum values, descriptions, bounds, and unknown-property rejection without creating a competing protocol vocabulary.
  - Reject `human_id`, `participant_id`, credentials, binding-authority fields, and unknown properties before an HTTP call.
  - Add one focused schema-compatibility scenario instead of duplicating every canonical fixture at the MCP layer.
  - Correct tool annotations for reads, writes, idempotency, and destructive account actions. Requirements: 6, 10, 11.

- [ ] 6. Add in-Kiro session diagnostics and account operations
  - Extend the MCP adapter dependency boundary so it can use the existing account service without duplicating Device Flow, credential precedence, keyring, or logout behavior.
  - Add `session_status`, including safe backend health, readiness, credential source and validity, authenticated Human, Project-selection state, analyzer availability, and supported capability reporting.
  - Add interactive `login` using the approved multi-round-trip MCP input capability, existing GitHub Device Flow, one-time backend exchange, and OS credential-store persistence.
  - Split account login into reusable begin and complete stages while preserving `conn login`; retain pending device authorization only in bounded process memory behind an opaque, expiring request-state identifier and erase it on every terminal outcome.
  - Keep the verification URI and one-time user code in direct interaction only; never return GitHub or Conn tokens to the model, logs, prompts, or resources.
  - Add confirmed `logout` that cannot take confirmation from model-supplied tool arguments and leaves an explicit `CONN_TOKEN` untouched.
  - Handle cancellation, expiry, denial, unavailable keyring, environment override, backend failure, and headless operation with actionable safe errors.
  - Add one fake-GitHub and MCP-client scenario protecting storage, revocation, cancellation, and redaction. Requirements: 2, 6, 11.

- [ ] 7. Implement deterministic Project discovery and session selection
  - Add a concurrency-safe process-local selection component with explicit source labels and no authoritative or credential data.
  - Add `list_projects` and `select_project` through the existing authenticated HTTP client.
  - Apply the reviewed precedence: explicit request, session selection, `CONN_PROJECT`, sole visible Project, exact safe repository match, then bounded ambiguity.
  - Reuse existing Git remote normalization and credential-removal behavior and never send an unnormalized credential-bearing remote to the backend or model.
  - Validate membership on every selection and retain backend authorization on every later operation.
  - Return human-readable bounded choices when Objectives, Projects, repositories, or Intents are ambiguous instead of guessing.
  - Add one parameterized selection scenario covering every precedence level, ambiguous repositories, unauthorized IDs, and process restart. Requirements: 3, 6, 11.

- [ ] 8. Implement the authoritative focused work-context read
  - Add one application operation that composes the selected Project, reader identity, exact Objective and Blueprint state, the reader's relevant Charters, owned active Intents, related collisions, evidence coverage, bounded updates, continuation, and known unknowns.
  - Add the versioned HTTP route and authenticated client operation using the existing response envelope and one `as_of_event` cursor.
  - Preserve own-Charter privacy while returning the related Project context already visible to authorized members.
  - Require an explicit Objective or Intent focus when several independent work contexts remain possible.
  - Report missing, proposed, rejected, superseded, stale, inaccessible, and incomplete information without inferring work completion.
  - Add `read_work_context` to MCP and retain `read_radar` for broad Project inspection.
  - Add one fundamental application/API/MCP scenario covering exact revisions, ambiguity, evidence gaps, related collisions, cursor consistency, and event continuation. Requirements: 4, 6, 8, 11.

- [ ] 9. Register the Kiro workflow prompts and focused resources
  - Register `conn-start`, `conn-sync`, `conn-resolve`, `conn-finish`, and `conn-handoff` as MCP prompts with concise descriptions and bounded arguments.
  - Make prompt instructions use `session_status`, explicit Project selection, `read_work_context`, existing participant-owned write tools, collision reads, and event cursors in the reviewed order.
  - Ensure prompt text stops on ambiguity or stale revisions and never represents tool approval as a binding human decision.
  - Register optional read-only resource templates for focused work context, collision context, and Intent handoff where supported, and verify each has an equivalent focused tool or prompt path used by Kiro CLI.
  - Render participant-authored and imported content as project data with a safe-handling note, not as workflow instructions.
  - Keep resource output bounded and source-free, and do not implement subscriptions or unsolicited notifications.
  - Add focused discovery and behavioral checks for tool and prompt names, arguments, required tool sequence, cursor handling, authority wording, safe rendering, and resource-to-tool fallback without snapshot-testing incidental prose. Requirements: 1, 4, 5, 8, 9, 10, 11.

- [ ] 10. Expose local TypeScript Intent analysis through MCP
  - Add `analyze_intent` as a participant-owned MCP operation using an exact Intent ID and expected revision.
  - Reuse `internal/analyzer` and the versioned Node.js subprocess rather than adding a second analyzer path.
  - Resolve the local worktree safely, constrain optional paths to it, reject traversal and unrelated worktrees, and verify that the selected RepositoryConnection has analysis enabled.
  - Give the analyzer no Conn token or database access and validate bounded JSON output before authenticated submission.
  - Reject or redact source, prompt-like, credential-shaped, unknown, or oversized output according to the canonical report contract.
  - Attach accepted observed claims to the exact Intent revision, run the existing deterministic assessor, and return incomplete diagnostics, resulting collisions, and the post-commit event cursor.
  - Add one subprocess scenario for valid source-free analysis and focused cases for traversal, stale revision, disabled analysis, forbidden output, and analyzer failure. Requirements: 7, 8, 11.

- [ ] 11. Complete the start, sync, finish, and handoff work loop
  - Exercise `/conn-start` from unauthenticated and authenticated states through Project selection, focused context, owned Intent reuse or publication, and initial collision check.
  - Exercise `/conn-sync` from a supplied cursor through pagination and selective rereads without a polling timer.
  - Exercise `/conn-finish` through exact revision comparison, eligible local analysis, collision recheck, evidence-gap explanation, bounded evidence attachment, and participant-owned Intent revision or release.
  - Ensure finish output states that Intent release does not prove Objective, Charter, pull-request, deployment, or Project completion.
  - Implement bounded JSON and Markdown handoff rendering from authoritative records, including decisions, evidence state, relevant updates, continuation cursor, unresolved items, and known unknowns.
  - Keep handoffs generated rather than persisted in this slice and exclude raw prompts, transcripts, source, diffs, and unbounded logs.
  - Add one end-to-end MCP scenario covering a complete participant work loop rather than separate tests for every prompt step. Requirements: 1, 4, 5, 8, 9, 11.

- [ ] 12. Provide an optional, inspectable Kiro integration bundle
  - After the integration-surface decision is accepted, prepare optional steering and prompt-hook templates for `SessionStart` and `Stop`.
  - Keep hook content bounded to availability, resume, active-Intent, collision, evidence, and `/conn-finish` guidance.
  - Ensure hooks perform no hidden writes, background polling, blocking supervision, or binding decisions.
  - Provide an explicit installation or copy workflow that first reads existing configuration, shows the proposed changes, and never overwrites unrelated user content.
  - Do not edit user-level or workspace MCP, hook, steering, skill, or custom-agent configuration as part of normal build, startup, tests, or MCP operations.
  - Verify that the complete tools-and-prompts workflow still works with the optional bundle absent.
  - Do not add a required custom agent or skill in this task. Requirements: 1, 5, 9, 11.

- [ ] 13. Decide the future direct-human binding-action boundary without implementing it
  - Compare elicitation-gated human review, a browser review page, and retaining CLI or terminal-application approval on directness, auditability, headless behavior, security, implementation cost, and fit with Conn's authority model.
  - Prototype only enough interaction to determine whether the model can be prevented from supplying the outcome and whether the exact immutable record and consequences can be shown directly to the human.
  - Define required audit provenance, cancellation, stale-revision behavior, tool permission behavior, and unsupported-client behavior.
  - Recommend one option and ask the project owner to approve or reject it.
  - If accepted, record a separate architecture decision and create a focused specification before production implementation.
  - Do not add MCP Charter acceptance, Objective or Blueprint approval, membership mutation, accepted risk, or binding ResolutionDecision tools in this specification. Requirements: 10, 11, 12.

- [ ] 14. Align maintained documentation and integration guidance
  - Correct stale README, package comment, CLI help, architecture, code-structure, and accepted-decision text that still describes MCP as read-only or the analyzer as a placeholder.
  - Document one-time binary and MCP setup separately from normal terminal-free Kiro session use.
  - Document interactive login, environment-token headless use, Project-selection precedence, slash workflows, resources, local analysis, event cursors, and the absence of idle MCP push.
  - Extend the real-trial guide with a Kiro-native path that does not launch `conn app` for participant Intent, collision, analysis, evidence, and handoff operations.
  - Retain a clearly labeled human-authoritative fallback for actions intentionally outside this specification.
  - Document read-only versus mutating tool permission recommendations without silently enabling auto-approval.
  - Do not add or edit an actual workspace or user MCP configuration unless the user explicitly requests configuration. Requirements: 1, 2, 3, 5, 9, 10, 11.

- [ ] 15. Validate the Kiro-native vertical slice with three independent sessions
  - Complete or extend the existing semantic-coordination trial with three distinct GitHub-authenticated humans and three independent Kiro sessions against one Project.
  - Confirm each session can authenticate from Kiro, select the correct Project, run `/conn-start`, read only its own Charter focus, publish or resume its participant-owned Intent, and preserve a usable event cursor.
  - Create concurrent semantic claims, run local analysis from Kiro, detect only supported advisory collisions, read a source-free context pack, and record a non-binding proposal.
  - Run `/conn-sync`, `/conn-finish`, and `/conn-handoff`; confirm exact revisions, evidence gaps, unresolved collisions, and unknowns remain explicit.
  - Confirm no session can impersonate a Human or Participant, perform a binding decision, escape its authorized worktree, or place source, prompts, transcripts, or credentials in Conn records.
  - Confirm ordinary participant operations require no `conn app` terminal while separately documenting any local-development backend processes.
  - Record observed authentication friction, Project-selection mistakes, prompt ambiguity, context size, tool-permission friction, analyzer limitations, and privacy gaps.
  - Run `pnpm install --frozen-lockfile`, `pnpm check`, `pnpm test`, and `pnpm build`, and stop when the requirements pass without adding unrelated adapters or refactors. Requirements: 1 through 12.
