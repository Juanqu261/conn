# Conn

Conn tells a coding agent, before it edits, when other in-progress work changes the same behavior, contract, or data, and gives the reviewer proof of what each change intended, touched, and verified.

Conn coordinates work. It does not launch agents, supervise people, score productivity, or let an agent make a binding human decision.

## Implemented MVP

- GitHub Device Flow login with Conn sessions stored in the operating-system credential store.
- Projects with owner/member authorization and versioned Objectives. Blueprints with human-accepted Cell Charters are optional planning context.
- Participant-owned Intents, declared and locally observed semantic claims, deterministic advisory collisions, non-binding proposals, human decisions that dismiss, resolve, or put collisions under review, and reference-first evidence.
- A shared concept catalog of Objective-derived and claimed subjects, near-match advice, and claim suggestions for the files an agent plans to change, so collisions open before anyone edits.
- One Go executable with CLI, `conn app`, `conn server`, warn-only Kiro command hooks, and a Kiro stdio MCP adapter.
- A local TypeScript analyzer subprocess with planned and diff-based observed modes that receives no Conn credential and returns bounded, source-free JSON.
- Kiro session diagnostics, login/logout, Project selection, focused work context, workflow prompts, optional resources, local analysis, and generated handoffs.
- Review proof: repository-defined checks in `.conn/checks.json` that `conn evidence run` executes and attaches as observed evidence, evidence provenance and staleness, `Conn-Intent` commit trailers with an optional Git hook, scope drift, and a bounded review packet for pull-request descriptions.
- Verification and CI: `conn verify-pair` tests the two branches of a collision together and records an advisory combination check; a report-only GitHub Actions check that Conn installs shows Intents, previewed collisions, and review packets on every pull request through a narrow CI credential.
- Adoption: an `AGENTS.md` section and printed MCP configuration for other coding agents, `conn start` from a Kiro spec, GitHub issue, or Objective, GitHub issue import, and Project-level insights.
- Conn Lab, a read-only web application served by `conn server`: GitHub sign-in in the browser, a Project overview, and a live feed of Project events ([Conn Lab](#conn-lab)).

The MVP requires the authoritative backend. It has no offline outbox, background polling, or idle MCP push.

## Local development

Requirements: Go 1.27.4, Node.js 26.13.1, pnpm 12.21.0, and Docker (PostgreSQL 18.7 runs through the supplied Compose file).

1. Copy `.env.example` to `.env` and fill in `CONN_GITHUB_CLIENT_ID` and `CONN_GITHUB_CLIENT_SECRET` from your GitHub OAuth app. In that app, enable Device Flow and set the **Authorization callback URL** to `http://127.0.0.1:5173/v1/auth/github/web/callback`. The comments in `.env.example` walk through it.
2. Run:

   ```powershell
   pnpm install --frozen-lockfile
   pnpm dev
   ```

   `pnpm dev` starts PostgreSQL, applies migrations, and runs `conn server` together with Conn Lab. Open **http://127.0.0.1:5173** for the Lab; Ctrl+C stops both servers. Database commands never delete the named volume, and `pnpm db:stop` stops PostgreSQL.

3. In another terminal, use the CLI and terminal application against the same server:

   ```powershell
   go run ./cmd/conn login
   go run ./cmd/conn app
   ```

## Conn Lab

Conn Lab is the web application: one page per Project showing its Objectives and their proven acceptance criteria, the Intents in progress, the collisions that need a decision, failing pair checks, open questions, and a live feed of everything that happens. Themed names ("Stitches", "Specimens on the slab") always carry a plain caption, and the **Plain labels** preference shows only the plain wording. Records appear by name; identifiers appear only on the Account page.

This version is read-only: decisions, Charter acceptance, and every other write stay in `conn app` and the CLI. Sign-in uses GitHub with the same OAuth app as `conn login`, requests no GitHub scope, and keeps the browser session in an `HttpOnly` cookie for at most 7 days, separate from your `conn login` session. The feed is live; the CLI, `conn mcp`, and hooks still pull by cursor.

Keyboard: `Ctrl K` or `⌘ K` searches the Lab, `g` then `o`, `f`, or `a` opens the overview, feed, or Account, and `?` lists shortcuts.

If GitHub says *"The redirect_uri is not associated with this application"*, the OAuth app's callback URL does not match the Lab's address. The sign-in page, the `conn server` log, and `conn doctor` each show the exact URL to register.

To serve the Lab from a built `conn` instead of `pnpm dev`, build the web app first so `conn` embeds it, then set `CONN_PUBLIC_URL` to the address people open (plain `http` only on a loopback address) and register `<CONN_PUBLIC_URL>/v1/auth/github/web/callback` as the callback URL:

```powershell
pnpm build
go build -o conn.exe ./cmd/conn
```

## Install the Conn power

The Conn Kiro power installs Conn's MCP server and three skills in one step, in Kiro IDE and in Kiro CLI v3. In Kiro, open the powers panel, choose **Add Custom Power**, then **Import power from a repository URL** with `https://github.com/Juanqu261/conn` (or **Import power from a folder** with a local `powers/conn`). Then ask the agent to set up Conn.

The `conn-setup` skill checks, in order, `conn --version`, `conn doctor`, the backend address, your session, and the Project, and reports each result before the next. It shows every installation and file change before making it and runs it only after you confirm:

- When `conn` is missing, it shows the build-from-source steps (Go, Node.js, and pnpm) and runs them only on confirmation.
- Sign-in uses GitHub device authorization; you approve the code yourself.
- It installs the workspace steering and warn-only hooks with `conn kiro integration install --scope workspace --root . --command-on-path`. Kiro loads the power only while a conversation is about Conn, so these always-on files are what warn you before an edit while the power is inactive.
- It offers the `Conn-Intent` commit-trailer hook with `conn git-hook preview`, then `install`.
- It finishes with `conn doctor` and suggests `/conn-start`.

The power runs `conn mcp` from `PATH` and passes only `CONN_SERVER_URL`. Unset or empty, it means a local `conn server` on `http://127.0.0.1:8080`; set it in the environment Kiro starts from for a team backend. Kiro manages the power's server; Conn never edits MCP configuration. `conn-work-loop` and `conn-review-proof` carry the same guidance as the Kiro steering, rendered from one source.

If the workspace also registers Conn through `kiro-cli mcp add`, both start the same server; `conn doctor` lists manual registrations so you can keep one. Maintainers regenerate the committed power with `go run ./cmd/conn kiro power export powers/conn --replace` after raising `kiropower.Version`.

## Manual Kiro setup

Without the power, build an absolute executable path, then explicitly add the local stdio server from the workspace that should use Conn:

```powershell
go build -o conn.exe ./cmd/conn
$conn = (Resolve-Path .\conn.exe).Path

kiro-cli mcp add `
  --name conn `
  --scope workspace `
  --command $conn `
  --env "CONN_SERVER_URL=http://127.0.0.1:8080" `
  --args mcp

kiro-cli mcp list workspace
```

This Kiro command edits `.kiro/settings/mcp.json`; Conn itself never edits MCP configuration. Use `/mcp` inside Kiro to check startup and `/tools` to inspect tools. Remove the entry with:

```powershell
kiro-cli mcp remove --name conn --scope workspace
```

The optional steering and warn-only command-hook bundle is separate from MCP setup:

```powershell
conn kiro integration preview --scope workspace --root .
conn kiro integration install --scope workspace --root .
# Later: conn kiro integration remove --scope workspace --root .
```

Preview performs no writes. Installation refuses to overwrite different content, preserves unrelated Kiro files, and removes an unchanged version 1 agent-prompt hook file. The hooks run this `conn` executable by absolute path; pass `--command-on-path` to run `conn` from `PATH` instead. Install from a built binary, not `go run`.

The four hooks read from Conn and never write to it, and they never block a tool call:

- SessionStart summarizes the focused Intent or recommends `/conn-start`.
- UserPromptSubmit reports new collisions, decisions, and counterpart revisions since the last prompt.
- PreToolUse on writes warns when the file is claimed by another participant's Intent or an open collision, then lets the write continue.
- PostToolUse on writes records the written path, never its content.

When Conn, the network, or the credential is unavailable, a hook allows the event silently and records a diagnostic that `conn doctor` reports.

The analyzer resolves from `CONN_ANALYZER_PATH`, then `packages/typescript-analyzer/dist/cli.js` beside the `conn` executable, then the current directory. Node.js resolves from `CONN_NODE_PATH`, then `PATH`. `session_status` and `conn doctor` report what was found.

## Daily Kiro workflow

Use the normal Kiro agent. A custom agent is not required.

- `/conn-start`: authenticate if needed, select a Project, load exact context, list concepts, suggest claims for the files you plan to change, publish or resume an Intent, and check collisions before any write.
- `/conn-sync <cursor>`: pull committed events and reread affected context. Conn does not poll while idle.
- `/conn-resolve <collision-id>`: inspect source-free context and optionally record a non-binding proposal.
- `/conn-finish <intent-id>`: record the commit as the Intent's head revision, analyze that revision against the Intent base, recheck collisions, offer each configured check for human-approved `conn evidence run`, attach approved reported evidence, read the review packet, and release owned work only when asked.
- `/conn-review <intent-id>`: produce the review packet as a pull-request description, ready for `gh pr create --body-file`.
- `/conn-handoff <intent-id>`: generate bounded JSON or Markdown without persisting a handoff record.

Collisions are advisory. A human decision closes them: `false_positive` dismisses; `dependency`, `sequencing`, `contract_agreement`, and `accepted_risk` resolve. A later Intent revision keeps the decision unless it adds a matched subject the decision did not cover. Record decisions with `conn resolution decide` or `conn app`.

## Review proof

Declare the commands that prove your acceptance criteria in a committed `.conn/checks.json`:

```json
{
  "version": 1,
  "checks": {
    "gateway-tests": {
      "command": ["pnpm", "--filter", "gateway", "test"],
      "working_directory": ".",
      "timeout_seconds": 600,
      "acceptance_keys": ["gateway-tests"],
      "env": { "CI": "1" }
    }
  }
}
```

Each `command` is an argument array that no shell interprets. `working_directory` must stay inside the repository, `timeout_seconds` is 1 to 3,600, `env` holds literal non-secret values and may not name a `CONN_` variable, and the file holds at most 50 checks.

Run a check for the focused Intent, or name one with `--intent`:

```powershell
conn evidence run gateway-tests
conn evidence run gateway-tests --intent <intent-id> --acceptance gateway-tests
```

The command runs without a shell and without any `CONN_` variable, streams its output, keeps the first 10 MiB in `<git dir>/conn/runs/<evidence-id>.log`, and sends only the output hash. It attaches `check_run` evidence per acceptance key, passing only for exit code 0 within the timeout, and exits with the check's exit code. The evidence records the check key, configuration and command hashes, commit, uncommitted-changes flag, exit code, timeout flag, and duration. It is labelled as observed on your machine, which is not independent CI proof. Evidence attached through MCP or `conn evidence attach` without an observation is labelled reported. Any of your Participants may attach evidence to your Intents, so a terminal run can attach to an Intent your Kiro session owns.

Name the Intent in each commit with the trailer `Conn-Intent: <intent-id>@<revision>`. Install the optional hook, which adds it from the local focus record and never blocks a commit:

```powershell
conn git-hook preview
conn git-hook install
# Later: conn git-hook remove
```

The hook goes into the effective hooks directory, including `core.hooksPath`. The hook is `commit-msg`, so it runs after you edit the message and leaves an empty message empty for Git to abort. Installation refuses to replace a different `commit-msg` hook and prints the line to add to it instead, and it removes an unchanged `prepare-commit-msg` hook left by an earlier Conn version. Without the hook, agents pass `focus.commit_trailer` from work context to `git commit --trailer`.

Generate the review packet:

```powershell
conn review --output review.md
gh pr create --body-file review.md
conn review --format json
```

The packet states what the Intent set out to do, each acceptance criterion with its evidence state, provenance, commit, and staleness against the local head, scope drift between declared and observed claims, changed files without a declared claim, collisions with their decisions, and integrity notes, including a changed `.conn/checks.json` on the branch. The Markdown stays under 60,000 characters and contains no source, diffs, prompts, transcripts, credentials, or logs. The packet is generated on demand and never stored. `--analyze` runs observed analysis first when the CLI participant owns the Intent; in Kiro, `/conn-review` asks before running `analyze_intent`.

Project resolution uses explicit tool input, process-local selection, `CONN_PROJECT`, the sole visible Project, or an exact credential-free Git remote match, in that order. Ambiguity stops the workflow.

Interactive Kiro can run Device Flow through `login`. Headless sessions have no mid-session input; use an explicit non-persistent `CONN_TOKEN` and narrowly trusted tools. Do not use `--trust-all-tools` for normal Conn operation.

Read-only tools, including `read_review_packet`, may be allowed after inspection. Keep login, logout, Intent writes, analysis, evidence, and proposal tools on `ask` unless a specific automation has a reviewed reason to allow them. Keep the shell tool on `ask` so each `conn evidence run` needs approval. Kiro permission allows a tool call; it is never a Charter, Objective, Blueprint, membership, risk, or binding resolution decision.

## Pair verification

When two Intents collide, test their branches together before either merges:

```powershell
conn verify-pair <collision-id>
conn verify-pair <collision-id> --fetch --yes
```

Conn resolves both `branch_ref` values from local branches or `origin/<branch>` (fetching only with `--fetch`), shows the two commits and the commands, and asks for confirmation because it runs code from both branches on this machine. It merges them in temporary detached worktrees, compares TypeScript diagnostics of the merged tree with each branch alone, then runs the setup and checks of the `pair_verification` section of `.conn/checks.json` on the default branch, never from either feature branch:

```json
{
  "pair_verification": {
    "setup": { "command": ["pnpm", "install", "--frozen-lockfile"], "timeout_seconds": 900 },
    "checks": ["gateway-tests", "order-tests"]
  }
}
```

The outcome is `merge_conflict`, `failing` for a new diagnostic or a failing check, `incomplete` when setup failed, a run timed out, or the analyzer was unavailable, and `passing` otherwise. Conn records an immutable combination check with commits, the diagnostic delta, exit codes, durations, and output hashes, and shows its latest outcome in conflict context, Radar, and review packets. It never changes the collision state. Commands run without a shell and without any `CONN_` variable, and the worktrees are removed after success, failure, or cancellation unless `--keep-worktree` is passed. `/conn-resolve` suggests the command; agents run it only through the shell with your approval.

## Pull-request check

Conn installs a report-only GitHub Actions workflow at `.github/workflows/conn-check.yml`:

```powershell
conn ci preview                 # workflow, target, and prerequisites; writes nothing
conn ci install --comment       # also keep one pull-request comment up to date
# Later: conn ci remove
```

In Kiro or another MCP client, `preview_ci_check` and `install_ci_check` do the same and only write the workflow file. The workflow builds Conn from the pinned source revision (this build's commit, or `--conn-ref`), checks out the pull request at its head, and runs `conn ci check`, which reports the Intents named by `Conn-Intent` trailers with one review packet each, drift against the changes observed in CI, unregistered work, and collisions previewed for the observed changes. It never fails the pull request for a finding or an error, runs no pull-request code, and writes nothing to Conn. Fork pull requests receive no secret and report that the check was skipped.

Prerequisites, which `conn ci preview` prints with exact values:

1. A Conn backend reachable from GitHub-hosted runners over HTTPS. A local development server is not reachable unless you use a self-hosted runner on the same network.
2. A readable Conn source repository; the template assumes the public `Juanqu261/conn`.
3. A Project owner creates the CI credential and stores it as the `CONN_CI_TOKEN` secret: `conn ci credential create --repository <repository-id> --github-secret <owner/name>`.
4. `gh variable set CONN_SERVER_URL --body <https-url> --repo <owner/name>`.
5. Commit and push the workflow.

A CI credential is bound to one Project and RepositoryConnection, reads only the routes the check needs plus the collision preview, expires after 1 to 365 days (90 by default), and is stored as a SHA-256 hash. `--github-secret` writes the token to the standard input of `gh secret set`; `--print` writes it to standard output; without either the command refuses. List and revoke credentials with `conn ci credential list` and `conn ci credential revoke <credential-id>`. Agents never create CI credentials, and the backend refuses creation from MCP.

## Other coding agents

Any MCP coding agent can use the same loop:

```powershell
conn mcp config --client generic   # mcpServers JSON with this conn executable
conn mcp config --client kiro      # the kiro-cli mcp add command
conn agents preview
conn agents install                # one marker-delimited Conn section in AGENTS.md
# Later: conn agents remove
```

Conn prints configuration and never writes MCP client settings. The `AGENTS.md` section comes from the same source as the Kiro steering; content outside its markers is never changed, and a modified section is preserved as a conflict. A client that identifies itself as something other than Kiro is recorded with participant adapter `mcp`; this changes attribution, not authority. Kiro command hooks remain Kiro-only.

## Starting work and importing issues

```powershell
conn start .kiro/specs/checkout-retries --paths src/payments/retry.ts
conn start acme/shop#218 --paths src/payments/retry.ts --publish
conn objective import-github-issue acme/shop#218
```

`conn start` resolves the Project, then reads an Objective by ID or imports a Spec Kit feature (see [Spec Kit](#spec-kit)), a Kiro spec, or a GitHub issue after showing its content and suggested acceptance criteria. An unchanged earlier import is reused and a changed one becomes a new revision. It then suggests claims for the planned paths, previews their collisions without publishing, and offers to publish a CLI Intent with the local focus, to continue in Kiro with `/conn-start <objective-id>`, or to quit. Non-interactive runs need `--publish` or `--import-only`.

Issue import calls the GitHub REST API from your machine, using `GH_TOKEN` or `GITHUB_TOKEN` only when set. The token is never stored or sent to Conn. Pull requests and bodies over 32,768 characters are refused, and an issue from a repository the Project does not connect produces a warning.

## Spec Kit

Teams that plan with [GitHub Spec Kit](https://github.com/github/spec-kit) see Conn collisions when the task list is written, before any code exists, in every agent Spec Kit supports.

```powershell
conn speckit check specs/001-checkout-retries        # preview collisions for the task list's paths; publishes nothing
conn start specs/001-checkout-retries                # import the feature as an Objective, then publish or revise its Intent
conn speckit extension export .conn-speckit          # render the Conn Spec Kit extension into a folder
specify extension add --dev .conn-speckit            # install it with Spec Kit's own command
```

`conn start <feature-dir>` imports a feature with source kind `speckit_feature`: the title from `spec.md`, one acceptance criterion per `**FR-NNN**` keyed `fr-nnn`, one outcome per `**SC-NNN**`, one question per `[NEEDS CLARIFICATION: ...]`, and one constraint per constitution principle, at most 50 in that order. Name checks in `.conn/checks.json` with `"acceptance_keys": ["fr-001"]` and the review packet shows FR-001's evidence. Planned paths come from the `tasks.md` task lines, or from the source-structure block of `plan.md`, and you can remove any before claims are suggested. When you already have an Intent for the feature, `conn start` shows the claims the task list adds and drops and revises it.

`conn speckit check` finds the feature from its argument, `SPECIFY_FEATURE_DIRECTORY`, `.specify/feature.json`, or `specs/<branch>/`. It names each collision by the other Intent's summary and Objective, excludes your own Intent and lists its claim delta, and always exits 0; `--format json` follows the `speckit-check` schema.

The extension runs `speckit.conn.check` after `/speckit.tasks` (optional), `speckit.conn.start` before `/speckit.implement`, and `speckit.conn.finish` after it (optional). Each command uses the Conn MCP tools when the agent has them and the `conn` CLI otherwise, and never makes a human decision. With the `kiro-cli` integration the commands appear as `/speckit.conn.*` beside Conn's `/conn-*` prompts. Conn reads Spec Kit files locally, sends only the Objective content you confirm and claims derived from paths, never runs Spec Kit or a command named in its files, and never writes `.specify/`. `conn doctor` reports whether the extension is installed. Warnings at each write remain Kiro-only for now.

## Insights

```powershell
conn insights --since-days 30
```

Insights report collisions opened per rule, decisions per kind and rule, the false-positive share, the median time to a first decision, decisions made before either Intent was released, analyzed revisions with undeclared observed changes, acceptance criteria by evidence provenance, and combination checks by outcome. They are Project-level only and never include a person; any ratio with fewer than five samples reports insufficient data.

## Validation and design

```powershell
pnpm check
pnpm test
pnpm build
```

See [architecture](docs/architecture.md), [code structure](docs/code-structure.md), [Kiro trials](docs/real-trial.md), and [accepted decisions](docs/decisions/README.md).
