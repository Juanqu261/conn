# 0009: Kiro power

- Status: accepted. The facts marked unconfirmed in the `kiro-power` design are settled by the spike in its task 3 before the real trial; a finding that changes a requirement reopens this record.
- Owner: project owner
- Amends: [0002](0002-runtime-and-interaction-surfaces.md) (Kiro distribution)

## Context

Setting up Conn in Kiro takes three manual steps: build `conn`, run `kiro-cli mcp add`, and optionally run `conn kiro integration install`. Kiro powers package MCP servers and skills into one unit that installs from the powers panel, a repository URL, or a local folder, in the IDE and in Kiro CLI v3, and loads when the conversation matches its keywords.

A power loads on demand, while Conn warns about collisions only if it is present when an edit happens. A collision warning that is unloaded at the moment of the edit is worthless.

The alternatives were the older `POWER.md` format, which still installs but is not the documented path forward, and moving the steering and hooks into the power, which the Agent Plugins format does not support for hooks.

## Decision

1. **Format: Agent Plugins.** The power lives at `powers/conn/` with `plugin.json`, `mcp.json`, and `skills/` (`conn-setup`, `conn-work-loop`, `conn-review-proof`). Its name `conn` stays stable, because renaming an installed power requires reinstalling it.
2. **Split between on-demand and always-on.** The power carries the MCP server and the skills, and loads on demand. The always-included steering and the warn-only command hooks stay workspace files that the `conn-setup` skill installs with `conn kiro integration install --command-on-path`, only after showing the preview and receiving the human's confirmation.
3. **How power users get `conn`.** The power runs `conn mcp` from `PATH`, so `conn` must already be installed. This milestone builds it from source, guided by the `conn-setup` skill. The skill never downloads and runs a binary.

   | Option                                                     | Advantages                                                         | Disadvantages                                                            | Operational cost    |
   | ---------------------------------------------------------- | ------------------------------------------------------------------ | ------------------------------------------------------------------------ | ------------------- |
   | Build from source, guided by `conn-setup` (selected)       | No release infrastructure; matches decision 0008's CI distribution | Needs Go, Node.js, and pnpm; several minutes on first install            | None                |
   | GitHub Release archives with `conn` and the built analyzer | Download and add to `PATH`; fastest first run                      | Needs a release workflow, checksums, and preferably signed artifacts     | Release maintenance |
   | `go install .../cmd/conn@<ref>`                            | One command for Go users                                           | No TypeScript analyzer, so planned and observed analysis are unavailable | None                |

   Release archives are the next adoption step and are proposed separately.
4. **Conn still never writes MCP configuration.** Kiro installs and manages the power's server and namespaces its name; Conn depends on no server name. `mcp.json` passes only the `${CONN_SERVER_URL}` placeholder, and Conn treats an empty `CONN_SERVER_URL`, `CONN_ANALYZER_PATH`, or `CONN_NODE_PATH` as unset. `conn kiro power export` renders files only into a folder the human names, and `conn doctor` reads Kiro MCP configuration without writing it.
5. **One source.** Every skill body renders from `internal/agentguide`, the same source as the Kiro steering and the `AGENTS.md` section. `internal/kiropower` holds the power's version, raised by hand whenever a rendered file changes, and a committed `.digest` and drift test enforce both.

## Consequences

- A Kiro user installs one power and is walked through the rest by `conn-setup`; the manual `kiro-cli mcp add` setup stays documented and working.
- A workspace can register Conn twice, through the power and through `kiro-cli mcp add`. `conn doctor` reports manual registrations read-only, and `conn-setup` recommends keeping one.
- First installation still costs a source build until release archives exist.
- Keyword activation is approximate. Too generic a keyword loads Conn into unrelated work; too narrow a list leaves the tools unloaded. The always-on bundle keeps warnings independent of activation.

## Validation

Render tests cover the manifest, the server declaration, skill front matter, line endings, byte stability, and a scan for credentials, addresses, paths, and identifiers. A drift test compares the committed power with the rendered one, and a digest test fails when content changes under an unchanged version. A parameterized test proves that empty Conn environment values behave as unset. A real trial installs the power from the repository URL in Kiro IDE and Kiro CLI v3 on a clean workspace and records the results in `docs/real-trial.md`.
