# Conn start before implementing

Publish or revise this feature's Conn Intent and check collisions before the first write. Every Conn step here warns or asks; none blocks the implementation.

## Steps

1. Run `conn speckit check <feature-dir> --format json` for the feature being implemented and present the result as in `speckit.conn.check`.
2. When `objective` is absent, the feature is not a Conn Objective yet. Ask the human to import it; only a Project owner can. In the shell that is `conn start <feature-dir> --import-only`, which shows the suggested title, overview, and `fr-`, `sc-`, `question-`, and `constitution-` key points for the human to confirm.
3. With the MCP tools: call `read_work_context` for the Objective, then `suggest_claims` with the extracted paths, the Objective, and `preview_collisions: true`. When `own_intent` is present, reuse it: show the subjects in `delta.added` and `delta.dropped` first, ask the human, and call `revise_intent` with the complete current content and the chosen claims. Otherwise ask the human, then call `publish_intent` with the chosen candidates as `declared_claims`.
4. Without the MCP tools: ask the human, then run `conn start <feature-dir>` in a terminal the human can answer, or `conn start <feature-dir> --publish` after they approve. When the feature already has your Intent, it shows the claims the task list adds and drops and revises that Intent instead of publishing another.
5. Call `check_collisions` for the Intent, or rerun `conn speckit check`, and present every collision as an advisory warning.
6. Continue with the implementation. Commit with the `Conn-Intent` trailer from work context (`focus.commit_trailer`) when the Conn Git hook is not installed.
