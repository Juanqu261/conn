---

## inclusion: always

# Conn product direction

Conn tells a coding agent, before it edits, when other in-progress work changes the same behavior, contract, or data, and gives the reviewer proof of what each change intended, touched, and verified.

Conn serves three moments in a human-and-agent workflow:

- Before editing: is anyone else changing what this work is about to change?
- During work: did another change affect this work?
- Before merging: did this change do what it said, and what is proven?

## Feature test

A feature belongs in Conn only if it reduces rework caused by concurrent changes or reduces the time a human needs to trust an agent's change. Chat, task assignment, agent status boards, agent launching, and per-person metrics fail this test.

## Product boundaries

- Conn is a coordination protocol and evidence layer, not an agent launcher or supervising agent.
- Semantic collision warnings and review proof are the product core. Objectives are required context for Intents. Blueprints and Cell Charters are optional planning layers and are never a precondition for warnings or evidence.
- Humans approve Objectives, decide binding resolutions, and remain accountable for agent-assisted work. Where a team uses Blueprints, each Human accepts their own Cell Charter.
- Agents may publish, revise, and release their own change intents and may propose clarifications, contracts, evidence, and conflict resolutions under their authenticated human participant identity. They may not approve binding proposals or silently change accepted boundaries.
- Cell Charters are advisory work boundaries, not file locks. Boundary changes must be proposed and recorded.
- Hooks and CI checks warn in the current version. They do not block work.
- Conn should complement the team's IDE, issue tracker, Git host, CI, and review tools rather than replace them.
- Conn must not implement employee productivity scoring. Team insights are Project-level only.

## Delivery boundary

- Deliver the milestone specifications in order: `pre-edit-collision-warnings`, `review-proof`, then `verification-ci-and-adoption`.
- Implement and validate the Kiro client integration first. Keep protocol concepts transport-neutral so other coding agents can use the MCP server and CLI.
- The existing three-session Kiro trial remains the acceptance test for the current MVP.
- Retain GitHub as the initial repository host unless a later product decision changes it.
