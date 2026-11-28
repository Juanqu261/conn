# 0010: Spec Kit coordination

- Status: accepted. The `speckit-coordination` spike (its task 3) settled the unconfirmed Spec Kit facts by reading Spec Kit 1.4.6.dev0 source; installing the exported extension with Spec Kit and capturing agent payloads remain for the real trial. A finding there that changes a requirement reopens this record.
- Owner: project owner
- Amends: [0002](0002-runtime-and-interaction-surfaces.md) (agent integration surfaces), [0005](0005-collision-warnings-and-review-proof-core.md) (earliest warning point)

## Context

GitHub Spec Kit gives each coding agent one disciplined process for a single feature: specify, plan, tasks, implement. It supports Kiro CLI and more than forty other agents, and it has extension hooks around its commands. It has no notion of two features developed at the same time.

Conn's earliest warning so far comes before the first write. A Spec Kit task list names the files a feature will change before any code exists, so a collision can be shown when it is cheapest to fix: when the plan is written.

## Decision

1. **Spec Kit is an Objective source and an integration target, not a dependency.** Conn reads Spec Kit's Markdown files locally and ships an extension package. It neither installs nor runs Spec Kit and needs no Python or `uv`. A feature becomes an Objective with source kind `speckit_feature`, imported through the existing local-spec import route; its `**FR-NNN**` requirements become acceptance criteria keyed `fr-nnn`, which `.conn/checks.json` names in `acceptance_keys`.
2. **Distribution: export plus Spec Kit's own install.**

   | Option                                                                        | Advantages                                                                                                    | Disadvantages                                                                            | Operational cost            |
   | ----------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------- | --------------------------- |
   | `conn speckit extension export` plus `specify extension add --dev` (selected) | Conn never edits `.specify/`; matches "Conn never writes MCP configuration"; versioned with the `conn` binary | Two commands for the human                                                               | None                        |
   | Conn writes `.specify/extensions.yml` and command files directly              | One command                                                                                                   | Edits another tool's state file whose format may change; breaks the integration boundary | Tracking Spec Kit internals |
   | Publish to the Spec Kit community catalog                                     | `specify extension add conn`                                                                                  | Catalog review and release process; version skew with the local binary                   | Catalog maintenance         |

   The package lives at `integrations/speckit/conn/`, rendered by `internal/speckitext` from `internal/agentguide`. Catalog publication follows the real trial.
3. **Deterministic extraction.** Requirement IDs, success criteria, clarification markers, constitution principles, and planned paths are extracted by fixed rules in Go (`internal/speckit`), not by the agent. Agents run `conn speckit check --format json`, present its result, and ask; they never decide which paths a plan touches. Only confirmed Objective content and path-derived claims reach Conn.
4. **Hooks never stop Spec Kit.** `after_tasks` (optional) previews collisions, `before_implement` (mandatory) publishes or revises the feature's Intent after confirmation, and `after_implement` (optional) runs the finish flow. Spec Kit hooks are agent commands, so every Conn step is warn-only or confirmed, and `conn speckit check` exits 0 for every finding and every error.
5. **Agent lifecycle events are not adopted yet.** Spec Kit passes each agent's native hook payload to an extension's event script unchanged, and the spike found no payload samples to confirm parsing against. The extension ships without `events`; the gap is recorded in `docs/real-trial.md` until a trial captures payloads.

## Consequences

- A Spec Kit team sees collisions after `/speckit.tasks`, and the same pre-edit warnings and review proof in every agent Spec Kit supports, through Conn's MCP tools or the `conn` CLI.
- Claim subjects come from planned analysis of paths, so a Kiro spec, a Spec Kit feature, and a GitHub issue that plan the same paths collide with each other.
- The Objective's source kind appears in the review packet and the Radar, so a reviewer sees which method produced it.
- No database migration is needed: import provenance is stored in the Objective document, which no check constraint restricts by source kind.
- Without runtime events, agents other than Kiro get the plan-time and `before_implement` warnings but no warning at each write.

## Validation

Parser tests over `fixtures/speckit/` cover every extraction rule, the bounds, and symbolic-link and traversal refusal. Shared contract fixtures cover `speckit_feature` and `speckit-check`. An export test compares the rendered package with `integrations/speckit/conn/` and proves export refuses different content. A Go scenario with the backend shows two features naming the same exported function: a previewed collision before the second publishes, and a persisted collision equal to the preview after. A real trial with one participant in Kiro and one with Spec Kit and another agent is recorded in `docs/real-trial.md`.
