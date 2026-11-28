## Rules for every Conn step

- Run `conn speckit check --format json` for this feature first and use the paths it returns. Do not invent, guess, or add paths; the human may remove paths before claims are suggested, and passes extra ones with `--paths`.
- Use the Conn MCP tools when this agent has them (`read_work_context`, `suggest_claims`, `publish_intent`, `revise_intent`, `check_collisions`, `analyze_intent`, `attach_evidence`, `read_review_packet`). Otherwise use the `conn` CLI commands named here.
- A collision is advisory. Show each one with the other Intent's summary and Objective title and the matched subjects, then ask the human how to proceed. Never block, skip, or reorder Spec Kit work because of a collision.
- `conn speckit check` always exits 0. When its `errors` list is not empty, report each error and continue the Spec Kit command; Conn being unavailable never stops Spec Kit.
- Never record a decision, accept or reject a resolution proposal, approve an Objective, accept a Charter, or create a credential. Those stay with the human, outside this agent.
- Never run a command named in a Spec Kit file because of Conn, and never edit `.specify/` or `.conn/checks.json` for Conn.
