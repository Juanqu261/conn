# Requirements

Milestone 4. Acceptance goal: a Kiro user installs Conn as a Kiro power from the Conn repository URL, and the power's onboarding takes them from nothing to a working Conn session in their workspace, with pre-edit warnings on, without reading the README.

A Kiro power packages MCP tools and skills into one installable unit that Kiro loads when the conversation matches its keywords, and unloads when the topic moves on. The current format is the Agent Plugins layout: a `plugin.json` manifest, an optional `mcp.json`, and `skills/<name>/SKILL.md`. Kiro installs powers from its registry, a public repository URL, or a local folder, in the IDE and in Kiro CLI v3.

Conn's MCP server, prompts, steering, and warn-only hooks already exist. The power changes how they are delivered, not what they do. One tension shapes this milestone: Conn warns about collisions only if it is present when an edit happens, while a power loads on demand. The power therefore handles discovery, onboarding, and on-demand tools. The always-on parts (the always-included steering and the command hooks) are installed into the workspace during onboarding by Conn's existing installer, with the human's confirmation.

This milestone depends on `kiro-native-work-loop` (MCP tools and prompts), `pre-edit-collision-warnings` (the integration bundle and hooks), and `verification-ci-and-adoption` (`conn mcp config`, `internal/agentguide`).

## 1. Power package

**User story:** As a Kiro user, I want to install Conn with one action from Kiro's powers panel.

### Acceptance criteria

1. THE SYSTEM SHALL provide a power in the Conn repository at `powers/conn/` with `plugin.json`, `mcp.json`, and `skills/`
2. `plugin.json` SHALL declare the Agent Plugins schema, the name `conn`, a semantic version, a description, an author, a license, the repository, and keywords
3. THE SYSTEM SHALL keep the power name `conn` stable, because renaming an installed power requires reinstalling it
4. THE SYSTEM SHALL install from the repository URL and from a local folder through Kiro's "Add Custom Power" options, with no step that Conn performs
5. THE SYSTEM SHALL contain no credential, token, server address, machine-specific path, or Project identifier in any power file

## 2. MCP server declaration

**User story:** As a power user, I want Conn's tools available whenever the power is active, pointing at my team's Conn backend.

### Acceptance criteria

1. `mcp.json` SHALL declare one stdio server that runs `conn mcp` from `PATH`
2. THE SYSTEM SHALL pass the backend address only through the `${CONN_SERVER_URL}` placeholder
3. WHEN `CONN_SERVER_URL`, `CONN_ANALYZER_PATH`, or `CONN_NODE_PATH` is set to an empty string
   THE SYSTEM SHALL treat it as unset and use its default
4. THE SYSTEM SHALL work unchanged when Kiro renames the server with its power namespace, depending on no server name
5. THE SYSTEM SHALL keep Conn's rule that Conn never writes MCP client configuration: Kiro manages the power's server, and Conn edits no Kiro settings file

## 3. Activation

**User story:** As a developer, I want Kiro to load Conn when my work is about coordination, parallel changes, or review proof, and not otherwise.

### Acceptance criteria

1. THE SYSTEM SHALL declare keywords covering Conn's three moments: before editing (collision, claim, intent, parallel work, teammate change), during work (sync, conflict, counterpart), and before merging (review packet, evidence, proof, pull request description)
2. THE SYSTEM SHALL record, from the activation spike, which prompts activate the power and which do not, and SHALL tune the keywords from that record
3. THE SYSTEM SHALL describe in the power's skills when the always-on workspace bundle is needed and why

## 4. Onboarding skill

**User story:** As a new user, I want the agent to check what's missing and set Conn up with me step by step.

### Acceptance criteria

1. THE SYSTEM SHALL provide a `conn-setup` skill that the agent runs on first activation and on request
2. THE skill SHALL check, in order: `conn --version` on `PATH` and the minimum version the power requires; the Node.js analyzer through `conn doctor`; the backend address; the Conn session through `session_status`; and the selected Project
3. WHEN `conn` is missing or too old
   THE skill SHALL show the supported installation steps and SHALL run them only after the human confirms
4. WHEN no session exists
   THE skill SHALL start login through the `login` tool or `conn login`, and SHALL leave the GitHub device approval to the human
5. THE skill SHALL offer the workspace integration bundle by running `conn kiro integration preview --scope workspace --root . --command-on-path`, showing the plan, and installing only after confirmation
6. THE skill SHALL offer `conn git-hook install` after `conn git-hook preview`, only after confirmation
7. THE skill SHALL finish with `conn doctor` and a short summary of what is on, what is off, and the next command, `/conn-start`
8. THE skill SHALL never create a CI credential, record a decision, accept a proposal, add a Project member, or edit MCP configuration

## 5. Work-loop skills

**User story:** As a power user, I want the agent to follow Conn's work loop while the power is active, even before the workspace bundle is installed.

### Acceptance criteria

1. THE SYSTEM SHALL provide a `conn-work-loop` skill with the same guidance as the Kiro steering and the `AGENTS.md` section, rendered from `internal/agentguide`
2. THE SYSTEM SHALL provide a `conn-review-proof` skill covering checks, `Conn-Intent` trailers, evidence labels, and the review packet, rendered from the same source
3. THE skills SHALL name the Conn MCP prompts and tools exactly as the server exposes them

## 6. Single source and drift

**User story:** As a Conn maintainer, I want the power, the steering, and `AGENTS.md` never to disagree.

### Acceptance criteria

1. THE SYSTEM SHALL render every power file from embedded templates and `internal/agentguide`
2. WHEN `conn kiro power export <dir>` runs
   THE SYSTEM SHALL write the rendered power, refusing to overwrite different content unless `--replace` is passed
3. A test SHALL fail WHEN the committed `powers/conn/` differs from the rendered power, and SHALL print the export command that fixes it
4. THE SYSTEM SHALL keep the power's version in one constant, raised whenever any rendered file changes

## 7. Coexistence with the manual setup

### Acceptance criteria

1. WHEN a workspace already registers Conn through `kiro-cli mcp add`
   THE onboarding skill SHALL detect it with `conn doctor` and SHALL explain that both registrations start the same server, recommending one
2. THE SYSTEM SHALL keep the manual setup (`kiro-cli mcp add`, `conn kiro integration install`) documented and working
3. `conn doctor` SHALL report, read-only, whether workspace or user MCP configuration also registers Conn, and `session_status` SHALL report the MCP client name the running server received

## 8. Publication

### Acceptance criteria

1. THE SYSTEM SHALL install from the public Conn repository URL, and from a URL to the `powers/conn` folder if Kiro supports folder URLs
2. AFTER the real trial passes
   THE SYSTEM SHALL be submitted to the Kiro powers registry, with the listing text taken from `plugin.json`

## 9. Milestone validation

### Acceptance criteria

1. Tests SHALL cover the rendered `plugin.json` and `mcp.json` (required keys, no machine-specific values, placeholder only), skill frontmatter, and the drift check
2. A test SHALL prove that empty Conn environment variables behave as unset
3. A real trial SHALL install the power from the repository URL in Kiro IDE and in Kiro CLI v3 on a clean workspace. It SHALL record the onboarding transcript, the activation results, a pre-edit warning from the installed hooks, and the time from install to the first `/conn-start` in `docs/real-trial.md`
