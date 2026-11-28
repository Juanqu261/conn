# Tasks

- [x] 1. Review and approve the Kiro power scope
  - Confirm the acceptance goal: one install from Kiro's powers panel plus guided onboarding to a working session with pre-edit warnings on.
  - Confirm the split: on-demand MCP tools and skills in the power; always-included steering and warn-only hooks installed into the workspace by onboarding after preview and confirmation.
  - Confirm that Conn still never writes MCP configuration.
  - Do not begin implementation until the specification and decision gate are approved. Requirements: 1, 2, 4, 7.

- [x] 2. Record decision 0009
  - Add `docs/decisions/0009-kiro-power.md`: Agent Plugins format, the on-demand and always-on split, the selected `conn` distribution for power users, and Conn not writing MCP configuration. Record that it amends 0002.
  - Update `docs/decisions/README.md`, `.kiro/steering/tech.md`, and the delivery order in `.kiro/steering/product.md`. Requirements: 1, 2, 4.

- [ ] 3. Spike the power in Kiro IDE and Kiro CLI v3
  - Install a hand-written minimal power from a local folder and from a public repository URL, including a subfolder URL.
  - Confirm: whether MCP prompts appear as slash commands; how an unset `${VAR}` is substituted; whether `command: "conn"` resolves on Windows; where installed powers live; and how Kiro CLI v3 installs a power.
  - Run a fixed list of twenty prompts (coordination, review, and unrelated work) and record which activate the power, with and without the workspace steering installed, and whether the power stays active through a focused Intent.
  - Record the findings in the design's "Kiro facts" section, choose the repository location (subfolder or dedicated repository), and stop for review if a finding changes a requirement. Requirements: 2, 3, 8.

- [x] 4. Treat empty Conn environment values as unset
  - Add the shared helper, use it at every read of `CONN_SERVER_URL`, `CONN_ANALYZER_PATH`, and `CONN_NODE_PATH`, and add one parameterized test. Requirements: 2.

- [x] 5. Extend `internal/agentguide` with power audiences
  - Add the `power-setup` and `power-review` renderings without changing the existing Kiro steering or `AGENTS.md` output, which the existing tests and the legacy-version checks pin. Requirements: 5, 6.

- [x] 6. Implement `internal/kiropower` and `conn kiro power export`
  - Render `plugin.json`, `mcp.json`, the three skills, and `references/install-from-source.md` with sorted keys and LF endings.
  - Add the export command with conflict refusal and `--replace`.
  - Add the render, sensitive-content, and byte-stability tests. Requirements: 1, 2, 4, 5, 6.

- [x] 7. Commit the power and add the drift checks
  - Export into `powers/conn/` (or the location chosen in task 3), commit it with `.digest`, and add the drift and version-digest tests.
  - Make sure Biome's Markdown formatting cannot rewrite the skills' frontmatter: exclude `powers/` from formatting, as for `.kiro/steering`. Requirements: 6.

- [x] 8. Extend `conn doctor`
  - Report the `conn` version against the power minimum, MCP registrations found in workspace and user configuration (read-only), duplicate-registration advice, and the workspace bundle state. Requirements: 4, 7.

- [x] 9. Update documentation
  - README: "Install the Conn power" as the first Kiro setup path, with the manual setup kept below it. Add a powers section to `internal/kirointegration/templates/README.md`. Requirements: 7, 8.

- [ ] 10. Run the real trial
  - Kiro IDE and Kiro CLI v3, clean workspace, install from the repository URL, complete onboarding, see a pre-edit warning while the power is inactive, and run `/conn-start`.
  - Record the transcript, activation results, timings, and friction in `docs/real-trial.md`. Requirements: 9.

- [ ] 11. Submit to the Kiro powers registry
  - After the trial passes, submit at `https://kiro.dev/powers/submit/` with the `plugin.json` listing text, and record the submission in `docs/real-trial.md`. Requirements: 8.
