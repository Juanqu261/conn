---

## inclusion: always

# Architecture decision policy

Do not introduce an implementation language, runtime, package manager, framework, repository topology, database library, API framework, deployment platform, or UI system unless an accepted decision record selects it.

When a new stack decision blocks progress:

1. State the decision and the requirements that constrain it.
2. Offer two or three practical options.
3. Compare their advantages, disadvantages, operational cost, and fit with existing decisions.
4. Recommend one option and explain the trade-off.
5. Ask the user to select or approve it.
6. Record the accepted choice under `docs/decisions/` before scaffolding code that depends on it.

Do not treat TypeScript repository support as a decision to implement Conn in TypeScript.
Do not create or edit MCP configuration unless the user explicitly asks for MCP configuration.
