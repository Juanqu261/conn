# MCP adapter boundary

## Responsibility

Expose approved Conn protocol operations to Kiro without owning domain semantics.

## Dependencies

`github.com/modelcontextprotocol/go-sdk` 1.8.0. SDK types stay inside this package. Kiro launches `conn mcp`, which serves one session over stdio (decision 0010).

## Current state

`Serve` completes MCP initialization and exposes no tools. Diagnostics go to standard error.

## Deferred

- Read-only tools: `read_objective`, `read_blueprint`, `read_my_charter`, `check_updates`
- Backend client and credential resolution
- Error mapping
- Kiro configuration
