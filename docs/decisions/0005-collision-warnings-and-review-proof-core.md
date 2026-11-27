# 0005: Collision warnings and review proof as the product core

- Status: accepted
- Owner: project owner

## Context

The original product idea put semantic collision warnings before edits at the center of Conn. The implemented MVP centered Objectives, Blueprints, and Cell Charters, and product steering described collision detection as secondary. A review of the implementation found that:

- the planning layer delays the first useful warning and makes Conn resemble a general collaboration tool;
- collisions match only identical subject strings, and agents receive no shared vocabulary;
- local analysis runs after implementation, cannot produce a contract producer-consumer collision, and ignores the Intent base revision;
- human decisions never close a collision;
- the optional Kiro hooks start credit-consuming agent turns and never run before a write;
- evidence state is reported by the agent and never observed by Conn.

## Decision

- Conn's purpose: Conn tells a coding agent, before it edits, when other in-progress work changes the same behavior, contract, or data, and gives the reviewer proof of what each change intended, touched, and verified.
- A feature belongs in Conn only if it reduces rework caused by concurrent changes or reduces the time a human needs to trust an agent's change.
- Objectives remain required context for Intents. Blueprints and Cell Charters remain supported but optional. No workflow requires them before Intent publication, collision warnings, or evidence.
- Hooks in this version only warn. No hook blocks a tool call, denies a permission, or waits for an answer before a write. Gating requires a later decision.
- Conn provides a CI add-on that it installs into the project that uses it, from MCP or the CLI. A project-scoped CI credential is accepted in principle. Its scope, creation surface, and distribution are recorded by the CI specification's decision.
- Delivery follows three specifications: `pre-edit-collision-warnings`, `review-proof`, and `verification-ci-and-adoption`.

## Consequences

- This decision replaces the product steering statement that collision detection is not the primary organizing feature.
- For this version, the CI add-on replaces the GitHub App sequence in the `semantic-intent-radar` design. A GitHub App may be reconsidered later.
- Decision 0001 authority rules still apply. Agents cannot make binding decisions, and warnings never become decisions.

## Validation

Each milestone specification defines one acceptance scenario:

1. Two Kiro sessions that never exchange subject strings receive a correct warning before the second agent edits conflicting code.
2. A reviewer can decide whether to trust a pull request from the Conn review packet alone.
3. A team can test two collided changes together and see Conn warnings and the review packet on every pull request.
