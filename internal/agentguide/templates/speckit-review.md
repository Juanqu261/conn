# Conn review packet

Produce the Conn review packet for this feature's Intent as a pull-request description.

## Steps

1. Call `read_work_context` for this feature's Intent. Stop and ask when the Intent is ambiguous.
2. When the current Intent revision has no observed analysis, ask the human, then call `analyze_intent`. Skip it when they decline.
3. Call `check_collisions`, then `read_review_packet` with format `markdown`. Without MCP tools, run `conn review --output <file>` in the shell.
4. Present the packet without rewording its facts: each acceptance criterion with its evidence state, scope drift, changed files without a declared claim, and collisions with their decisions.
5. Offer to save it to a file the human names for `gh pr create --body-file <file>`. Never present the packet as approval, CI proof, or completion.
