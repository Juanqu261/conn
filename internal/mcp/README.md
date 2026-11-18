# MCP adapter boundary

## Responsibility

Expose approved Conn protocol operations to Kiro without owning domain semantics.

## Dependencies

`github.com/modelcontextprotocol/go-sdk` 1.8.0. SDK types stay inside this package. Kiro launches `conn mcp`, which serves one session over stdio (decision 0010).

## Tools

All tools are read-only, idempotent, and closed-world. They call the same authenticated backend client as the CLI and never touch the database.

| Tool              | Reads                                                                                    |
| ----------------- | ---------------------------------------------------------------------------------------- |
| `read_objective`  | One Objective revision; defaults to the project's only Objective and its latest revision |
| `read_blueprint`  | One BlueprintRevision with status; defaults to the active revision, else the latest      |
| `read_my_charter` | The session human's own charters in a revision in force, with each decision              |
| `check_updates`   | Project events after an `after` cursor                                                   |

- Results use the API envelope `{"data": ..., "as_of_event": n}` as structured content, with a one-line summary first. Every `data` document is validated against `contracts/v1` before it is returned; `read_my_charter` returns `charter-view`.
- The project comes from `project_id` input, else `CONN_PROJECT`.
- No input names a human. `read_my_charter` reads `GET /v1/humans/me` for the session's identity, and tool input schemas reject unknown properties such as `human_id`.
- Summaries say plainly when a revision is unapproved, and `read_my_charter` reports a newer unapproved proposal.
- Credentials are resolved on every call, so a `conn login` during the Kiro session takes effect without a restart. Failures become tool errors that name the human's next step, such as `conn login` or `conn doctor`.
- Diagnostics go to standard error; standard output carries only MCP messages.

## Test

`TestMCPStdio` in `cmd/conn` starts the real `conn mcp` subprocess against an in-test backend. It checks the tool list, a charter read, rejection of `human_id`, an event cursor that sees a change made elsewhere, and a clean exit.

## Deferred

- Write tools and agent-authored proposals
- Output schemas in tool metadata; outputs are validated against the canonical schemas instead
