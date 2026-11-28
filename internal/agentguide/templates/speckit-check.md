# Conn plan check

Check this feature's plan against teammates' in-progress work, before any code exists. This step publishes nothing.

## Steps

1. Identify the feature directory: the user input when it names one, otherwise the directory the core command just used, such as `specs/001-feature-name`.
2. Run `conn speckit check <feature-dir> --format json` in the shell.
3. Report the planned paths and their source (`tasks.md` or the `plan.md` source structure), the paths marked as new files, `unresolved` tokens, and `tasks_without_path`. Ask the human whether to remove any path; when they do, rerun with `--paths` set to the kept paths.
4. Report each entry in `collisions` with `counterpart.summary`, `counterpart.objective_title`, `matched_subjects`, and `state`. A state other than `open` means an earlier human decision would be inherited.
5. When `own_intent` is present, this feature already has an Intent. List `delta.added` and `delta.dropped`, and say that {{prompt:conn-start}} or the `speckit.conn.start` command revises it.
6. When `publish_offered` is true, offer to publish this feature's Intent now so that later plans collide with this one. Publish only after the human agrees, using the `speckit.conn.start` steps.
