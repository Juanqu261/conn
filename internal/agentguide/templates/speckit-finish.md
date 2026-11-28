# Conn finish after implementing

Record what this feature changed and produce review proof. Run it only when the human agrees.

## Steps

1. Call `read_work_context` for this feature's Intent. Stop and ask when the Intent is ambiguous.
2. When the changes are committed, call `revise_intent` with the complete current content and `head_revision_ref` set to the current commit (`git rev-parse HEAD`). Record the head before analyzing.
3. Call `analyze_intent` with the revision you just recorded and without paths. Without MCP tools, run `conn intent analyze` in the shell. Report analysis as unavailable when it cannot run.
4. Call `check_collisions` and keep every collision that needs attention explicit.
5. Read `.conn/checks.json` when it exists and list the checks whose `acceptance_keys` name this feature's requirements, such as `fr-001`. Ask the human before each one, then run it as `conn evidence run <check-key>`. Never run a check unasked.
6. Produce the review packet with {{prompt:conn-review}}, `read_review_packet`, or `conn review`, and present each `fr-` and `sc-` key point with its evidence state as the packet states it.
7. Intent release does not prove the feature, the Objective, the pull request, or the Project complete. Release only when the human asks.
