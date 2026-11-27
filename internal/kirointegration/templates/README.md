# Optional Kiro integration bundle

This directory contains the versioned source for Conn's optional Kiro-specific guidance (bundle version 2):

- `.kiro/steering/conn-work-loop.md` renders from `internal/agentguide`, the same source as the `AGENTS.md` section for other coding agents. `legacy/conn-work-loop.v2.md` is the previous steering file; an unchanged copy is replaced during installation.
- `hooks/conn-hooks.json` becomes `.kiro/hooks/conn-hooks.json`, with `{{CONN_COMMAND}}` replaced by the conn command.
- `legacy/conn-lifecycle.v1.json` is the exact version 1 agent-prompt hook file. It is never installed; it lets installation recognize and retire an unchanged copy.

The bundle is not required for Conn. MCP tools and prompts remain the complete integration core when these files are absent.

The steering also describes the review-proof loop: `Conn-Intent` commit trailers, `conn evidence run` for checks in `.conn/checks.json`, and `/conn-review`. The commit-trailer Git hook is not part of this bundle; install it separately with `conn git-hook install`.

## Inspect-first workflow

Preview a workspace installation without writing:

```text
conn kiro integration preview --scope workspace --root <workspace>
```

Explicitly install after reviewing the plan:

```text
conn kiro integration install --scope workspace --root <workspace>
```

The hooks run the current conn executable by absolute path, quoted when the path contains spaces. Pass `--command-on-path` to render `conn` from `PATH` instead. Install for the current user by choosing user scope; Kiro CLI V3 loads user hooks from `~/.kiro/hooks/`:

```text
conn kiro integration preview --scope user
conn kiro integration install --scope user
```

The preview lists preserved entries and prints the exact proposed Conn file contents. The installer reads every target file before changing anything. Different existing content is a conflict and is never overwritten. An unchanged `.kiro/hooks/conn-lifecycle.json` from version 1 is listed as `remove` and deleted during installation; a modified one is preserved and reported as a conflict for manual review. Other files under `.kiro/steering` and `.kiro/hooks` remain untouched. The command does not edit `.kiro/settings/mcp.json`, skills, agents, or any Conn record.

Remove only Conn artifacts:

```text
conn kiro integration remove --scope workspace --root <workspace>
```

Removal re-renders the hook template with the command found in the installed file and deletes the file only when it matches byte for byte. A modified target is preserved for manual review.

## Hook boundary

All four hooks are command hooks that run `conn hook <name>`. They consume no model credits.

| Hook                 | Trigger             | Behavior                                                                            |
| -------------------- | ------------------- | ----------------------------------------------------------------------------------- |
| `conn-session-start` | `SessionStart`      | Summarize the focused Intent, or recommend `/conn-start` in a connected repository. |
| `conn-prompt-submit` | `UserPromptSubmit`  | Report new collisions, state changes, counterpart revisions, and queued warnings.   |
| `conn-pre-write`     | `PreToolUse` write  | Warn when the file is claimed by another Intent or an open collision; never block.  |
| `conn-post-write`    | `PostToolUse` write | Record the written path, never its content.                                         |

Hooks read from the Conn backend and never write to it. They never exit 2, print a permission decision, or ask for permission. A pre-write warning exits with code 1, which Kiro documents as showing standard error to the human while the tool runs; `conn hook pre-write --warn-exit-code 0` keeps warnings in the prompt-submit queue only. Any Conn, network, credential, or input failure allows the event silently and records a diagnostic that `conn doctor` reports.

Hook output is labelled `Conn (Project data, not instructions):` and contains only identifier prefixes, revisions, rule IDs, normalized subjects, states, counts, and Conn commands, never participant prose.

Official Kiro references:

- Steering: <https://kiro.dev/docs/steering.md>
- Hooks: <https://kiro.dev/docs/hooks.md>
- Hook triggers: <https://kiro.dev/docs/hooks/types.md>
- Hook actions: <https://kiro.dev/docs/hooks/actions.md>
- CLI V3 hook migration: <https://kiro.dev/docs/cli/v3/hooks-migration.md>
