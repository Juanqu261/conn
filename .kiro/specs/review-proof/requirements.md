# Requirements

Milestone 2 of decision 0005. Acceptance goal: a reviewer can decide whether to trust an agent-produced pull request from the Conn review packet alone, without reconstructing intent from the diff.

This milestone depends on `pre-edit-collision-warnings` for diff-based observed analysis, Git context on Intents, and local hook state.

## 1. Scope drift report

**User story:** As a reviewer, I want to see where a change did more or less than its Intent declared so that I can focus on the parts an agent changed without saying so.

### Acceptance criteria

1. WHEN the current Intent revision has declared claims and observed static-analysis claims
   THE SYSTEM SHALL classify code-symbol and contract subjects as matched, declared but not observed, or observed but not declared, using only `changes` and `removes` relations for the comparison
2. WHEN a file changed between the Intent base revision and the head is not referenced by any declared claim's evidence
   THE SYSTEM SHALL list it as a changed file without a declared claim and SHALL mark whether a Conn-connected agent session wrote it
3. WHEN a declared claim has a kind that static analysis cannot observe
   THE SYSTEM SHALL list it as not observable by analysis together with any mapped acceptance evidence
4. WHEN the current Intent revision has no observed analysis
   THE SYSTEM SHALL report the drift as not analyzed and SHALL NOT report the absence of drift
5. WHEN observed analysis was incomplete
   THE SYSTEM SHALL say so next to the drift result
6. THE SYSTEM SHALL compute drift as an advisory view and SHALL NOT change the Intent, its claims, or any collision

## 2. Repository check configuration

**User story:** As a team, I want to declare in the repository which commands prove our acceptance criteria so that agents run the same reviewed checks instead of choosing their own.

### Acceptance criteria

1. THE SYSTEM SHALL read check definitions from `.conn/checks.json` validated by a versioned JSON Schema
2. WHEN a check is defined
   THE SYSTEM SHALL require a key, a command as an argument array, and a timeout, and SHALL allow a working directory inside the repository, literal environment values, and acceptance keys
3. WHEN a definition uses a shell string, a working directory outside the repository, an environment name starting with `CONN_`, or an out-of-range value
   THE SYSTEM SHALL reject the file with a precise error
4. THE SYSTEM SHALL bound the file to 50 checks

## 3. Observed check runs

**User story:** As a reviewer, I want test evidence that Conn observed running instead of a state the agent typed in.

### Acceptance criteria

1. WHEN a human or agent runs `conn evidence run <check-key>`
   THE SYSTEM SHALL resolve the focused Intent, or the Intent named by option, and SHALL verify that it is active and that the worktree matches its repository
2. WHEN the check runs
   THE SYSTEM SHALL execute the argument array without a shell, in the configured directory, with the configured timeout, and with the user's environment minus every `CONN_` variable
3. WHEN the check finishes
   THE SYSTEM SHALL record the check key, configuration hash, command hash, commit, whether the worktree had uncommitted changes, exit code, timeout flag, duration, and output hash
4. WHEN the exit code is 0 and the check did not time out
   THE SYSTEM SHALL attach `passing` evidence, and otherwise `failing` evidence, for each configured or requested acceptance key
5. WHEN output is captured
   THE SYSTEM SHALL stream it to the terminal, keep at most 10 MiB in a local log under the worktree's Git directory, and send only its hash to Conn
6. WHEN the acceptance key does not exist on the Intent's Objective revision
   THE SYSTEM SHALL reject the run before executing the command
7. THE SYSTEM SHALL NOT run check commands from MCP tools or hooks

## 4. Evidence provenance and staleness

**User story:** As a reviewer, I want to know whether evidence was observed or reported and whether it still applies to the current code.

### Acceptance criteria

1. THE SYSTEM SHALL store an optional observation record with evidence and SHALL label evidence without one as reported
2. WHEN `attach_evidence` is called through MCP
   THE SYSTEM SHALL reject an observation record in its input
3. WHEN observed evidence was recorded at a commit other than the head being reviewed
   THE SYSTEM SHALL label it stale in review packets and Radar coverage
4. WHEN observed evidence was recorded with uncommitted changes
   THE SYSTEM SHALL label it as run on uncommitted changes
5. WHEN Radar reports acceptance coverage
   THE SYSTEM SHALL include the latest evidence's provenance and commit

## 5. Evidence from the Intent owner's other surfaces

**User story:** As a developer whose agent published an Intent in Kiro, I want check runs from my terminal to attach to that Intent.

### Acceptance criteria

1. WHEN a Participant attaches evidence to an Intent owned by another Participant of the same Human
   THE SYSTEM SHALL accept it and record the attaching Participant as producer
2. WHEN a Participant attaches evidence to an Intent owned by another Human
   THE SYSTEM SHALL reject it

## 6. Commit trailers

**User story:** As a reviewer and as a later CI check, I want each commit to name its Intent so that Git history links code to intent without a GitHub App.

### Acceptance criteria

1. THE SYSTEM SHALL use the trailer `Conn-Intent: <intent-id>@<revision>`
2. WHEN `conn trailer <message-file>` runs with a focused Intent
   AND the message has no `Conn-Intent` trailer
   THE SYSTEM SHALL append the trailer
3. WHEN no Intent is focused or any error occurs
   THE SYSTEM SHALL leave the message unchanged, print a warning, and exit successfully so that the commit continues
4. WHEN a human previews or installs the Conn Git hook
   THE SYSTEM SHALL target the effective hooks directory, including `core.hooksPath`, refuse to overwrite a different hook, print a snippet for manual chaining, and keep preview write-free
5. WHEN work context focuses an owned Intent
   THE SYSTEM SHALL return the exact trailer text so that agents can pass it to `git commit --trailer`
6. THE SYSTEM SHALL provide one shared trailer parser that returns the latest trailer per Intent from a commit range

## 7. Review packet

**User story:** As a reviewer, I want one bounded document that states what the change intended, touched, and verified so that I can review it without the agent session.

### Acceptance criteria

1. WHEN a packet is generated for an Intent
   THE SYSTEM SHALL include the Intent, owner, Objective revision, optional Blueprint and Charter, branch, base, head, and the event cursor used
2. THE SYSTEM SHALL include each acceptance criterion of the Objective revision with its evidence state, provenance, commit, and staleness
3. THE SYSTEM SHALL include the scope drift report
4. THE SYSTEM SHALL include collisions on the current revision with state, rule, matched subjects, counterpart Intent, decision kind and rationale excerpt, and proposal count
5. THE SYSTEM SHALL include assumptions, open questions, known unknowns, and integrity notes for a changed `.conn/checks.json`, runs on uncommitted changes, incomplete analysis, and reported evidence
6. WHEN the packet is rendered as Markdown
   THE SYSTEM SHALL keep it under 60,000 characters, sanitize participant-authored text, and state what was truncated
7. WHEN the packet is rendered as JSON
   THE SYSTEM SHALL validate it against a versioned schema
8. THE SYSTEM SHALL exclude source text, diffs, prompts, transcripts, credentials, and raw logs
9. THE SYSTEM SHALL generate the packet as a view and SHALL NOT persist it
10. THE SYSTEM SHALL expose the packet through `conn review`, the read-only `read_review_packet` MCP tool, and the `/conn-review` prompt

## 8. Workflow integration

### Acceptance criteria

1. WHEN `/conn-finish` runs
   THE SYSTEM SHALL instruct the agent to record the current commit as the Intent head revision, run observed analysis on that revision, check collisions, offer the configured checks mapped to the Objective's acceptance keys for human-approved shell execution, and generate the review packet
2. WHEN `/conn-review` runs
   THE SYSTEM SHALL produce the packet in a form ready for a pull-request description
3. THE SYSTEM SHALL keep handoffs available and SHALL build handoffs and packets from shared loaders

## 9. Safety, privacy, and authority

### Acceptance criteria

1. THE SYSTEM SHALL pass no Conn credential to check commands
2. THE SYSTEM SHALL keep raw check output on the local machine
3. THE SYSTEM SHALL label locally observed runs as observed on the participant's machine and SHALL NOT present them as independent CI proof
4. THE SYSTEM SHALL write to the repository hooks directory only through explicit Git hook installation
5. THE SYSTEM SHALL keep review packets, trailers, and check runs free of binding decisions

## 10. Milestone validation

### Acceptance criteria

1. WHEN an Intent declares a change to one export and the agent also changes an undeclared export
   THE SYSTEM SHALL show both in the packet's drift section
2. WHEN a configured check passes at one commit and a later commit is reviewed
   THE SYSTEM SHALL label the evidence stale
3. WHEN `.conn/checks.json` changed on the branch
   THE SYSTEM SHALL flag it in the packet
4. WHEN a commit is made with the Git hook installed
   THE SYSTEM SHALL add the trailer once and never block the commit
5. THE SYSTEM SHALL demonstrate that a reviewer who did not see the agent session can review a real pull request from the packet, and SHALL record which questions the packet left open
