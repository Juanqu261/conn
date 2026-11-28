# Design

## Purpose and scope

Conn's Kiro setup is three manual steps today: build `conn`, run `kiro-cli mcp add`, and optionally run `conn kiro integration install`. A Kiro power replaces that with one install from the powers panel and an onboarding skill that walks the human through the rest.

The power adds no Conn capability. It packages the existing MCP server and guidance in Kiro's native distribution format, and reuses the existing installer for everything that must stay always on.

## Dependencies

- `kiro-native-work-loop`: the MCP server, tools, and prompts (`/conn-start`, `/conn-sync`, `/conn-resolve`, `/conn-finish`, `/conn-review`, `/conn-handoff`).
- `pre-edit-collision-warnings`: `internal/kirointegration` (steering and warn-only command hooks, preview-first install, conflict refusal).
- `verification-ci-and-adoption`: `internal/agentguide` as the single guidance source, and `conn mcp config`.

## Architecture decision gate

Decision 0009 must be accepted before implementation. It amends 0002 (interaction surfaces) and records:

1. **Format: Agent Plugins.** Use `plugin.json`, `mcp.json`, and `skills/`, which is the current recommended format. The older `POWER.md` format still installs, but it is not the documented path forward.
2. **Split between on-demand and always-on.** The power carries the MCP server and skills, and loads on demand. The always-included steering and the warn-only command hooks stay workspace files that the onboarding skill installs with `conn kiro integration install --command-on-path` after preview and confirmation. The Agent Plugins format documents no hooks, and a collision warning that is unloaded at the moment of the edit is worthless.
3. **How users get the `conn` executable.** The power runs `conn` from `PATH`, so it must already be installed.

   | Option                                                                             | Advantages                                                                          | Disadvantages                                                            | Operational cost    |
   | ---------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------- | ------------------------------------------------------------------------ | ------------------- |
   | Build from source, guided by the onboarding skill (recommended for this milestone) | No release infrastructure; matches decision 0008's CI distribution                  | Needs Go, Node.js, and pnpm; several minutes on first install            | None                |
   | GitHub Release archives per OS with `conn` and the built analyzer side by side     | Download and add to `PATH`; analyzer found beside the executable; fastest first run | Needs a release workflow, checksums, and preferably signed artifacts     | Release maintenance |
   | `go install github.com/Juanqu261/conn/cmd/conn@<ref>`                              | One command for Go users                                                            | No TypeScript analyzer, so planned and observed analysis are unavailable | None                |

   Recommendation: build from source now. Release archives are the next adoption step and are the main thing that would cut install time for people trying Conn. They are proposed as a separate follow-up, not part of this milestone.
4. **Conn still never writes MCP configuration.** Kiro installs and manages the power's server. Conn only renders files into its own repository or into a folder the human names.

## Kiro facts this design relies on

Confirmed from kiro.dev documentation and the `kirodotdev/powers` repository:

- A power folder holds `plugin.json` (required), `mcp.json` (optional), and `skills/<name>/SKILL.md` with optional `scripts/` and `references/`. One repository may hold several powers in separate folders.
- `plugin.json` requires `$schema` (`https://agent-plugins.org/schemas/1.0.0/plugin.schema.json`), `name`, `version`, `description`, `author.name`, and `keywords`. `homepage`, `repository`, `license`, `author.url`, and `author.email` are optional. Renaming an installed power may require reinstalling it.
- `mcp.json` uses `$schema` `https://agent-plugins.org/schemas/1.0.0/mcp.schema.json` and `mcpServers.<name>` with `type: "stdio"`, `command`, `args`, and `env`. Secrets and environment-specific values use `${VAR}` placeholders.
- Kiro namespaces server names (`<server>` becomes `power-<power>-<server>`), manages the servers itself rather than through `~/.kiro/settings/mcp.json`, and activates and deactivates them with the power.
- Activation matches installed powers' keywords against the task. A power's tools and skills load on a match and unload when another power's keywords match.
- Installation: registry ("Add to Kiro"), the IDE powers panel, "Add Custom Power" → "Import power from a repository URL" (GitHub or GitLab; no branch means `main`), or "Import power from a folder". Updates come through "Check for updates". `kiro-cli powers uninstall <name>` exists. Powers work in the IDE and in Kiro CLI v3.
- Registry submission is at `https://kiro.dev/powers/submit/`.

Not confirmed, and resolved by the spike in task 3:

- Whether a power's MCP **prompts** appear as slash commands while the power is active.
- Whether the always-included workspace steering, or the `UserPromptSubmit` hook output that mentions Conn, keeps the power active during a focused Intent.
- How Kiro substitutes an unset `${VAR}` (empty string, literal text, or a prompt for a value).
- Whether `command: "conn"` resolves `conn.exe` on Windows through `PATH` and `PATHEXT`.
- Whether a repository URL can point to a subfolder (`.../tree/main/powers/conn`), or the power must sit at the repository root.
- The installation path on disk and how to install from Kiro CLI v3.

## Package layout

```
powers/conn/
  plugin.json
  mcp.json
  skills/
    conn-setup/
      SKILL.md
      references/
        install-from-source.md
    conn-work-loop/
      SKILL.md
    conn-review-proof/
      SKILL.md
```

If the spike shows that repository-URL install needs the power at the repository root, the fallback is a small dedicated repository (for example `conn-kiro-power`) that a release step updates from `conn kiro power export`. That decision is recorded in task 3.

### `plugin.json`

```json
{
  "$schema": "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json",
  "name": "conn",
  "version": "1.0.0",
  "description": "Warns your agent before it edits when a teammate's in-progress work changes the same behavior, contract, or data, and gives reviewers proof of what each change intended, touched, and verified.",
  "author": { "name": "Conn" },
  "license": "MIT",
  "repository": "https://github.com/Juanqu261/conn",
  "homepage": "https://github.com/Juanqu261/conn#readme",
  "keywords": [
    "conn",
    "collision",
    "collisions",
    "parallel work",
    "concurrent changes",
    "teammate",
    "intent",
    "claims",
    "coordination",
    "merge conflict",
    "review packet",
    "review proof",
    "evidence",
    "pull request description"
  ]
}
```

The first keyword set is a starting point. Task 3 runs a fixed prompt list and records which prompts activate the power. Keywords too generic to be safe (for example `review` or `test`) are excluded, because unrelated activation costs every user context.

### `mcp.json`

```json
{
  "$schema": "https://agent-plugins.org/schemas/1.0.0/mcp.schema.json",
  "mcpServers": {
    "conn": {
      "type": "stdio",
      "command": "conn",
      "args": ["mcp"],
      "env": {
        "CONN_SERVER_URL": "${CONN_SERVER_URL}"
      }
    }
  }
}
```

Only the backend address is passed through. The session token stays in the operating-system credential store, as today. `CONN_ANALYZER_PATH` and `CONN_NODE_PATH` are left out so that the defaults (the analyzer beside the executable, and `node` on `PATH`) apply. If the spike shows that an unset placeholder becomes an empty string, requirement 2.3 already makes that equivalent to unset. If it becomes literal `${CONN_SERVER_URL}`, `httpclient.ParseServerURL` rejects it with a message naming the variable, and the setup skill explains how to set it.

### Empty environment values

`internal/analyzer/locate.go`, `internal/cli/account.go`, `internal/hooks/hooks.go`, and `internal/cicheck` read these variables. Each read goes through one helper, `envValue(lookup, name) (string, bool)`, which returns not-set for an empty or whitespace-only value. One parameterized test covers the three variables.

## Skills

Each `SKILL.md` has frontmatter with `name` and `description`, and a Markdown body. All three are rendered by `internal/kiropower` from `internal/agentguide`, extended with two new audiences beside `kiro` and `generic`: `power-setup` and `power-review`. The work-loop text is shared, so the Kiro steering, `AGENTS.md`, the Spec Kit commands (if `speckit-coordination` lands), and the power cannot drift.

### `conn-setup`

Description: "Set up Conn in this workspace: check the conn executable, the backend, your session, and the Project, then turn on pre-edit warnings."

Steps, each of which reports its result before the next:

1. **Executable.** Run `conn --version`. If it is missing or older than `MinimumConnVersion`, show `references/install-from-source.md` (clone at a release tag, `pnpm install --frozen-lockfile`, `pnpm build`, `go build -o conn ./cmd/conn`, put the folder on `PATH`). Run these only after the human confirms, and stop if any fails.
2. **Diagnostics.** Run `conn doctor`. Report the analyzer and Node.js status, credential-store availability, and any recorded hook diagnostics.
3. **Backend.** If `CONN_SERVER_URL` is unset, explain that `conn mcp` defaults to `http://127.0.0.1:8080` (a local `conn server`), and how to set it for a team backend. Never invent an address.
4. **Session.** Call `session_status`. With no session, call `login` (or `conn login` in a terminal) and tell the human to approve the GitHub device code. Never approve on their behalf.
5. **Project.** Call `list_projects`. With one Project, offer to select it. With several, ask. With none, explain that a Project owner creates it with `conn project create`. The agent must not create it without the human asking.
6. **Always-on warnings.** Explain that pre-edit warnings come from workspace hooks that work even when the power is inactive. Run `conn kiro integration preview --scope workspace --root . --command-on-path`, show the plan, and run `install` only on confirmation. A conflict is reported, never forced.
7. **Commit trailers.** Offer `conn git-hook preview`, then `install` on confirmation.
8. **Summary.** Run `conn doctor`, list what is on and off, and suggest `/conn-start` (or the `read_work_context` tool if prompts are not exposed; see the spike).

Forbidden actions, stated in the skill: creating CI credentials, recording decisions, accepting proposals, adding Project members, creating Projects unasked, and editing MCP configuration.

### `conn-work-loop`

The rendered work-loop guidance: `/conn-start` before editing, `/conn-sync`, advisory collisions, `/conn-resolve` without binding decisions, and `/conn-finish`. It opens with one line: if this workspace has no `.kiro/steering/conn-work-loop.md`, run `conn-setup` step 6 so that warnings stay on when the power is inactive.

### `conn-review-proof`

The review-proof section: `.conn/checks.json`, human-approved `conn evidence run`, evidence labels (observed on this machine, reported), `Conn-Intent` trailers, and `/conn-review` to `gh pr create --body-file`.

## Rendering and drift

New package `internal/kiropower`:

- `const Version = "1.0.0"` and `const MinimumConnVersion`, both raised by hand. A test computes a digest over the rendered files and fails if the digest changed while `Version` did not, using a committed `powers/conn/.digest` file.
- `Render() ([]Artifact, error)` returns `plugin.json`, `mcp.json`, and the skill files, with LF line endings (the repository's `.gitattributes` keeps them LF; this is the CRLF lesson from the integration bundle).
- `conn kiro power export <dir> [--replace]` writes the artifacts. Without `--replace`, any existing file with different content is a conflict and nothing is written. It never deletes files it did not render.
- The drift test renders and compares with `powers/conn/`. On mismatch it prints `go run ./cmd/conn kiro power export powers/conn --replace`.

JSON is emitted with sorted keys and two-space indentation so that the output is byte-stable.

## `conn doctor` additions

- `conn` version and the power's minimum version.
- MCP registrations: `conn doctor` runs in its own process and cannot see which client started a server. It reads `.kiro/settings/mcp.json` and the user-level Kiro MCP configuration (read-only) for an entry that runs `conn mcp`, and reports each. The setup skill combines that with the power being active: a registration found while the power is installed means duplicate servers, with a recommendation to remove the manual one through `kiro-cli mcp remove`.
- `session_status`, which runs inside the server, adds the `clientInfo.name` it received, so the agent can tell the human which client it is talking through.
- Workspace bundle installed or not, and git hook installed or not (already reported).

## Security and privacy

- Power files contain no secrets, addresses, paths, or identifiers. A test scans the rendered files for `conn_`, `http://`, `https://` outside the schema and repository URLs, absolute paths, and UUIDs.
- A local MCP server runs with the user's access. The power runs only the `conn` executable the user installed. The setup skill never downloads and runs a binary.
- Onboarding runs installers only in preview-then-confirm form, so every file written to the workspace was shown first.

## Error behavior

- `conn` not on `PATH`: Kiro reports that the server failed to start. The setup skill explains installation. The power's skills still load.
- Backend unreachable: tools return Conn's existing connectivity errors. The setup skill suggests `conn server` for local use.
- Integration conflict: reported with the conflicting paths. The human resolves it manually.

## Validation plan

- Render tests: required `plugin.json` keys, `mcp.json` shape with only the placeholder, skill frontmatter present, LF endings, the sensitive-content scan, and byte stability.
- The drift and version-digest tests.
- The empty-environment test.
- Real trial in Kiro IDE and Kiro CLI v3 on a clean workspace and machine account. Record the install path used, the onboarding transcript, the activation results, a pre-edit warning from the installed hooks while the power is inactive, and time to first `/conn-start`.

## Deferred

- Release archives for `conn` with the analyzer (proposed next).
- A hosted demo backend for people trying the power.
- Moving the always-on hooks into the power, if Kiro adds hooks to the Agent Plugins format.
