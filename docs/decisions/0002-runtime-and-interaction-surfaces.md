# 0002: Runtime and interaction surfaces

- Status: accepted
- Owner: project owner

## Context

Humans need a guided interface, scripts need stable commands, and Kiro needs native coordination without turning Conn into an agent runtime.

Alternatives were interactive Cobra prompts, a required custom Kiro agent, or an immediate web application. They offered either too little persistent context or too much framework and deployment cost for the MVP.

## Decision

- One Go executable provides CLI, `conn app`, `conn mcp`, and `conn server` modes.
- `conn app` uses Bubble Tea, Bubbles, and Lip Gloss. Drafts remain in memory, writes show a read-only review, and explicit confirmation precedes the authenticated API call.
- Raw Cobra commands remain the stable scripting surface. Presentation layers do not own domain, authorization, persistence, or protocol rules.
- Kiro uses the normal agent with `conn mcp` over stdio. Tools and prompts are the required core.
- Focused resources, steering, and `SessionStart`/`Stop` hooks are optional. Hooks perform no silent authoritative write, polling, supervision, or binding decision.
- Every resource-backed workflow has a focused tool fallback. Synchronization is pull-by-cursor during explicit operations; there is no idle push.
- Normal startup never edits MCP, hook, steering, skill, permission, or agent configuration. The optional Kiro bundle requires an explicit conflict-safe install command.
- CLI/TUI remain available for administration and human-authoritative actions outside MCP.

## Consequences

Conn supports interactive humans, automation, and Kiro without a sidecar or custom agent. Kiro-specific optional files require compatibility maintenance but can be removed without changing authoritative state.

## Validation

Smoke-test every executable mode. Exercise reviewed TUI writes, MCP discovery and the complete participant work loop, optional-bundle installation/removal, cursor synchronization, authority boundaries, and operation with the optional bundle absent.
