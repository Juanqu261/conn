# Optional Kiro integration bundle

This directory contains the versioned source for Conn's optional Kiro-specific guidance:

- `steering/conn-work-loop.md` becomes `.kiro/steering/conn-work-loop.md`.
- `hooks/conn-lifecycle.json` becomes `.kiro/hooks/conn-lifecycle.json`.

The bundle is not required for Conn. MCP tools and prompts remain the complete integration core when these files are absent.

## Inspect-first workflow

Preview a workspace installation without writing:

```text
conn kiro integration preview --scope workspace --root <workspace>
```

Explicitly install after reviewing the plan:

```text
conn kiro integration install --scope workspace --root <workspace>
```

Install for the current user by choosing user scope. Omit `--root` to use the operating-system home directory:

```text
conn kiro integration preview --scope user
conn kiro integration install --scope user
```

The preview lists preserved entries and prints the exact proposed Conn file contents. The installer reads both exact target files before changing anything. Different existing content is a conflict and is never overwritten. Other files under `.kiro/steering` and `.kiro/hooks` are listed as preserved and remain untouched. The command does not edit `.kiro/settings/mcp.json`, skills, agents, or any Conn record.

Remove only byte-for-byte Conn artifacts:

```text
conn kiro integration remove --scope workspace --root <workspace>
```

A modified target is preserved for manual review instead of being deleted.

## Hook boundary

The `SessionStart` hook may call only `session_status` and `read_work_context`. The `Stop` hook may use read-only Conn tools to report continuation state and offer `/conn-finish`; it must not invoke the finish workflow automatically. Neither hook publishes, revises, analyzes, releases, attaches evidence, proposes a resolution, polls, blocks stopping, or performs a binding action.

Kiro agent prompt hooks do not provide an enforceable read-only flag. The prompt text is advisory. Kiro tool permissions and Conn backend authorization remain the safety and authority boundaries. Users who do not want lifecycle agent turns should leave the bundle uninstalled or remove it.

Official Kiro references:

- Steering: <https://kiro.dev/docs/steering.md>
- Hooks: <https://kiro.dev/docs/hooks.md>
- Hook triggers: <https://kiro.dev/docs/hooks/types.md>
- Hook actions: <https://kiro.dev/docs/hooks/actions.md>
- CLI V3 hook migration: <https://kiro.dev/docs/cli/v3/hooks-migration.md>
