# Requirements

Milestone 5. Acceptance goal: a team that plans features with GitHub Spec Kit sees Conn collisions when a plan is written, before any code exists, and gets the same pre-edit warnings and review proof in every coding agent Spec Kit supports, without Conn replacing or editing Spec Kit.

Spec Kit gives each agent a disciplined, single-feature process (`/speckit.specify` → `/speckit.plan` → `/speckit.tasks` → `/speckit.implement`). It has no mechanism for features developed at the same time. Conn supplies that layer: a Spec Kit feature becomes an Objective and an Intent, its task list becomes planned claims, and its hooks call Conn at the points where a collision is cheapest to fix.

This milestone depends on `verification-ci-and-adoption` (`conn start`, collision preview, `AGENTS.md` guidance, `mcp` attribution) and `review-proof` (checks, acceptance keys, review packet).

## 1. Spec Kit feature as an Objective source

**User story:** As a developer who wrote a feature specification with Spec Kit, I want Conn to import it as an Objective so that its requirements become the acceptance criteria Conn tracks evidence against.

### Acceptance criteria

1. WHEN a human runs `conn start <path>` and the path is a directory containing `spec.md`, or is a `spec.md` file inside one
   THE SYSTEM SHALL treat it as a Spec Kit feature with source kind `speckit_feature`
2. THE SYSTEM SHALL record the repository-relative `spec.md` path as the source reference and its SHA-256 digest as the content hash
3. THE SYSTEM SHALL suggest the title from the first level-one heading with a leading `Feature Specification:` removed, and otherwise from the feature directory name without its number prefix
4. THE SYSTEM SHALL suggest one `acceptance_criterion` key point per `**FR-NNN**` functional requirement with key `fr-nnn`, one `outcome` per `**SC-NNN**` success criterion with key `sc-nnn`, and one `question` per `[NEEDS CLARIFICATION: ...]` marker
5. WHEN the repository has a Spec Kit constitution
   THE SYSTEM SHALL suggest one `constraint` key point per constitution principle heading, keyed `constitution-<slug>`, and SHALL record the constitution version line in the overview
6. THE SYSTEM SHALL bound suggested key points to the Objective limit of 50, preferring requirements, then success criteria, then questions, then constitution principles, and SHALL report what it omitted
7. WHEN the human has not confirmed the suggested Objective content
   THE SYSTEM SHALL send nothing to Conn
8. WHEN a feature imported before is started again with the same digest
   THE SYSTEM SHALL reuse the Objective, and WHEN the digest changed THE SYSTEM SHALL offer a new Objective revision after confirmation, as for Kiro specs and GitHub issues

## 2. Planned paths from the task list

**User story:** As a developer, I want the files my Spec Kit task list names to become my planned claims so that I don't have to list them again.

### Acceptance criteria

1. WHEN the feature directory contains `tasks.md`
   THE SYSTEM SHALL extract repository-relative file paths from task lines of the form `- [ ] T<number> ...` and `- [x] T<number> ...`
2. THE SYSTEM SHALL accept a token as a path only when it contains a `/` or a file extension, resolves inside the repository, and either exists or has an existing parent directory
3. WHEN `tasks.md` is absent and `plan.md` contains a source-structure code block
   THE SYSTEM SHALL extract paths from that block with the same rules
4. THE SYSTEM SHALL report extracted paths in first-appearance order, deduplicated, bounded to 200, and SHALL let the human remove paths before claims are suggested
5. THE SYSTEM SHALL report which paths are new files, which are existing files, and which task lines named no path
6. WHEN `--paths` is passed
   THE SYSTEM SHALL use those paths instead of extracting them

## 3. Plan-time collision check

**User story:** As a developer who just generated a task list, I want to know whether a teammate's in-progress work changes what my plan will change, before I start implementing.

### Acceptance criteria

1. WHEN a human or agent runs `conn speckit check [<feature-dir>]`
   THE SYSTEM SHALL suggest claims for the extracted planned paths and SHALL return the collision preview against active Intents without publishing anything
2. THE SYSTEM SHALL list each previewed collision with the counterpart Intent title, its Objective, the matched subjects, and any inherited decision state, using names rather than identifiers
3. WHEN the feature already has a published Intent
   THE SYSTEM SHALL exclude that Intent from its own preview and SHALL report claims the task list adds or drops relative to the Intent's current revision
4. THE SYSTEM SHALL exit 0 for every finding and for every Conn, network, or configuration error, and SHALL print the reason for an error
5. WHEN the preview finds no collision
   THE SYSTEM SHALL offer to publish the Intent now so that later plans collide with this one

## 4. Spec Kit extension

**User story:** As a Spec Kit user, I want Conn to run at the right Spec Kit steps automatically so that coordination is part of the process I already follow.

### Acceptance criteria

1. THE SYSTEM SHALL provide a Spec Kit extension with manifest `extension.yml`, schema version `1.0`, extension id `conn`, and commands named `speckit.conn.<name>`
2. THE SYSTEM SHALL register an optional `after_tasks` hook that runs the plan-time collision check
3. THE SYSTEM SHALL register a `before_implement` hook that publishes or revises the feature's Intent with confirmation and checks collisions
4. THE SYSTEM SHALL register an optional `after_implement` hook that runs the Conn finish flow: record the head revision, run observed analysis, offer each configured check for human-approved `conn evidence run`, and produce the review packet
5. THE SYSTEM SHALL declare `conn` as a required tool and a supported Spec Kit version range in `requires`
6. EACH extension command SHALL instruct the agent to use the Conn MCP tools when the client has them and the `conn` CLI otherwise, and SHALL never instruct the agent to make a human decision, accept a proposal, or create a credential
7. WHEN `conn speckit extension export <dir>` runs
   THE SYSTEM SHALL write the extension package into the directory, refusing to overwrite different content, so that the human installs it with Spec Kit's own command
8. THE SYSTEM SHALL never create or edit `.specify/extensions.yml` or any other Spec Kit file

## 5. Agent lifecycle events

**User story:** As a developer using a coding agent other than Kiro through Spec Kit, I want Conn's warn-only pre-write warning in that agent too.

### Acceptance criteria

1. WHERE the Spec Kit runtime-event spike confirms that an agent's `pre_tool_use` and `post_tool_use` payloads can be identified and parsed
   THE SYSTEM SHALL register Conn's warn-only `conn hook` commands for `session_start`, `user_prompt_submit`, `pre_tool_use`, and `post_tool_use` through the extension's `events`
2. THE SYSTEM SHALL detect the payload format of each supported agent and SHALL allow any event it cannot parse silently, recording a diagnostic that `conn doctor` reports
3. THE SYSTEM SHALL keep these hooks read-only toward Conn and non-blocking, as the Kiro hooks are
4. WHERE the spike does not confirm parseable payloads
   THE SYSTEM SHALL ship the extension without `events` and record the gap in `docs/real-trial.md`

## 6. Kiro and Spec Kit together

**User story:** As a Kiro user on a team where others use Spec Kit, I want our work to collide in Conn no matter which method each of us used.

### Acceptance criteria

1. THE SYSTEM SHALL produce identical claim subjects for the same planned paths whether the Intent started from a Kiro spec, a Spec Kit feature, or a GitHub issue
2. WHEN Spec Kit is installed with the `kiro-cli` integration
   THE SYSTEM SHALL work alongside the Spec Kit prompts in `.kiro/prompts/` and the Conn prompts without name conflicts
3. THE SYSTEM SHALL record the Objective source kind so that the review packet and Radar show which method produced each Objective

## 7. Safety, privacy, and authority

### Acceptance criteria

1. THE SYSTEM SHALL read Spec Kit files locally and SHALL send Conn only the Objective content the human confirmed and the claims derived from paths, never task text, plan text, research, contracts, or data models
2. THE SYSTEM SHALL bound each Spec Kit file it reads to 1 MiB and SHALL refuse symbolic links and paths outside the repository
3. THE SYSTEM SHALL keep human authority unchanged: an agent may publish and revise its own Intent through the extension, and decisions, Objective approval, and credentials remain human-only
4. THE SYSTEM SHALL execute no command named in a Spec Kit file

## 8. Milestone validation

### Acceptance criteria

1. THE SYSTEM SHALL include a committed Spec Kit feature fixture with `spec.md`, `plan.md`, `tasks.md`, and a constitution, covering every extraction rule
2. A Go scenario SHALL prove that two features whose task lists name the same exported function produce a previewed collision before either publishes, and a published collision after one publishes
3. A real trial SHALL run one participant with Kiro and one with Spec Kit and another coding agent on the same repository and record the plan-time warning, the `before_implement` warning, and the review packet in `docs/real-trial.md`
