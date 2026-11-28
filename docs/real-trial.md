# Manual trials

These trials are the real-world acceptance tests that automated tests cannot replace. Each needs real people, GitHub accounts, and Kiro sessions. Do not mark a trial complete from expected results alone; record what you observed.

| Trial                                                                       | People | Needs                                                            |
| --------------------------------------------------------------------------- | ------ | ---------------------------------------------------------------- |
| [Three-session Kiro trial](#three-session-kiro-trial)                       | 3      | Local backend, Blueprints and Charters                           |
| [Two-session pre-edit trial](#two-session-pre-edit-trial)                   | 2      | Local backend, a GitHub repository made from the fixture         |
| [Review-proof reviewer trial](#review-proof-reviewer-trial)                 | 2      | The pre-edit trial repository                                    |
| [Verification, CI, and adoption trial](#verification-ci-and-adoption-trial) | 2      | An HTTPS URL for the backend, GitHub Actions                     |
| [Kiro power trial](#kiro-power-trial)                                       | 1-2    | Kiro IDE and Kiro CLI v3, a clean workspace and machine account  |
| [Spec Kit coordination trial](#spec-kit-coordination-trial)                 | 2      | The pre-edit trial repository, Spec Kit, Kiro, and another agent |

Run the pre-edit, review-proof, and verification trials in order: each reuses the repository and Project of the one before. The Spec Kit trial reuses them too and runs after the pre-edit trial.

Never record OAuth codes, Conn tokens, CI tokens, private prompts, repository source, or credentials. The local PostgreSQL volume is persistent; use a uniquely named Project and do not delete the volume as routine cleanup.

## Shared setup

### Machine and accounts

Run every participant on one machine, each under their own operating-system account. Each account then has its own credential store and its own Kiro session, and every account reaches the backend over loopback.

The Conn client accepts plain `http` only for loopback addresses. A participant on another machine needs the backend behind HTTPS.

Build `conn.exe` at the root of a Conn checkout that every account can read. The analyzer is then found next to the executable at `packages/typescript-analyzer/dist/cli.js`. If you copy `conn.exe` elsewhere, set `CONN_ANALYZER_PATH` to that file in each account and in each MCP configuration. The Kiro hooks run `conn.exe` by its absolute path, so that path must be readable by every account.

### Start the backend

In the Conn checkout, with `.env` copied from `.env.example` and `CONN_GITHUB_CLIENT_ID` set to a GitHub OAuth app with Device Flow enabled (development builds read `CONN_DATABASE_URL` and the client ID from `.env`):

```powershell
pnpm install --frozen-lockfile
pnpm check
pnpm build
pnpm db:start
pnpm db:migrate
go build -o conn.exe ./cmd/conn
.\conn.exe server
```

Keep the server running on `127.0.0.1:8080`.

### Each participant

In each operating-system account:

```powershell
$conn = 'C:\path\to\conn\conn.exe'   # the shared build
& $conn login                       # GitHub Device Flow; use this account's GitHub login
& $conn whoami                      # note the Human ID
& $conn doctor                      # server, session, analyzer, Node.js
```

Expected: every account reports a different GitHub login and Human ID, and `conn doctor` finds the analyzer and Node.js.

### Identifiers

Conn documents take lowercase UUID version 7 identifiers. PowerShell 7.5 and later creates them:

```powershell
[guid]::CreateVersion7().ToString()
```

## Three-session Kiro trial

This is the acceptance test for the Kiro-native MVP with Blueprints and Charters. It requires three people, three GitHub accounts, and three independent Kiro V3 sessions.

The trial validates participant work without launching `conn app` for Intents, analysis, collisions, proposals, evidence, sync, finish, or handoff. Human-authoritative Project, membership, Objective, Blueprint, and Charter actions still use `conn app` or direct CLI commands.

### Participants

| Person | Conn role | Accepted cell          | Independent environment                        |
| ------ | --------- | ---------------------- | ---------------------------------------------- |
| A      | owner     | gateway behavior       | GitHub account A, OS account A, Kiro session A |
| B      | member    | order behavior         | GitHub account B, OS account B, Kiro session B |
| C      | member    | operator documentation | GitHub account C, OS account C, Kiro session C |

Each Kiro session must use its own workspace clone. Confirm every identity with `session_status`; never infer identity from the terminal name.

### 1. Validate and start local infrastructure

Complete the [shared setup](#shared-setup). Each participant clone must run `pnpm install --frozen-lockfile` and `pnpm build` so `packages/typescript-analyzer/dist/cli.js` exists locally.

### 2. Configure three Kiro sessions

In each participant workspace, print the exact MCP command for the shared build and run it:

```powershell
& $conn mcp config --client kiro    # prints: kiro-cli mcp add --name conn --scope workspace ...
kiro-cli mcp list workspace
kiro-cli chat --v3 --require-mcp-startup
```

Inside each Kiro session:

1. Run `/mcp` and confirm `conn` is connected.
2. Call `session_status`.
3. If unauthenticated, call `login` and complete GitHub Device Flow directly.
4. Call `session_status` again and record the sanitized Human UUID and GitHub login.

Expected: all three Human UUIDs and GitHub logins differ. No Device Flow code or token appears in the trial record.

### 3. Bootstrap the human-approved plan

Person A uses `conn app` or direct CLI commands to:

1. create `Kiro native trial <date>`;
2. add the already authenticated Human UUIDs for B and C;
3. import a small reviewed Objective with these keyed acceptance criteria:
   - `gateway-tests`: gateway retry tests pass;
   - `order-tests`: order retry tests pass;
   - `operator-docs`: retry behavior is documented;
4. connect the repository remote with local analysis enabled;
5. propose one Blueprint revision with three cells assigned to A, B, and C.

Each person directly accepts only their own Charter through `conn app` or `conn charter accept`. After the third acceptance, verify that the Blueprint is active.

Record sanitized identifiers:

```text
PROJECT_ID=
OBJECTIVE_ID=
OBJECTIVE_REVISION=
BLUEPRINT_ID=
BLUEPRINT_REVISION=
CELL_A_ID=
CELL_B_ID=
CELL_C_ID=
REPOSITORY_ID=
START_CURSOR=
```

This is the only stage that requires the human-authoritative surfaces.

### 4. Start three participant work cells

In each Kiro session, invoke `/conn-start` with that person's cell objective. If Project selection is ambiguous, use `list_projects` and `select_project`; do not guess.

Expected in every session:

- the authenticated Human and Kiro Participant are explicit;
- only that Human's accepted Charter appears in `own_charters`;
- Objective, Blueprint, Charter, and Intent revisions are exact;
- the response includes one authoritative event cursor;
- an owned active Intent is resumed or published before implementation;
- initial collisions and evidence gaps are explicit.

Record each Intent and cursor:

```text
INTENT_A=  REVISION_A=  CURSOR_A=
INTENT_B=  REVISION_B=  CURSOR_B=
INTENT_C=  REVISION_C=  CURSOR_C=
```

If `/conn-start` publishes without the required repository or claim, use `revise_intent` with the exact current revision while preserving every other field.

A and B declare the same invariant:

```json
{
  "subject_kind": "invariant",
  "normalized_subject": "invariant:checkout:one-idempotency-key-one-successful-charge",
  "display_name": "One idempotency key, one successful charge",
  "relation": "changes",
  "provenance_kind": "declared"
}
```

C declares an independent contract claim:

```json
{
  "subject_kind": "contract",
  "normalized_subject": "contract:operator-docs:retry-guidance",
  "display_name": "Operator retry guidance",
  "relation": "produces",
  "provenance_kind": "declared"
}
```

Expected: exactly one supported `shared_declared_invariant` collision exists between A and B. C's unrelated claim creates no collision. The assessment is advisory and does not block work.

### 5. Synchronize by cursor

After all Intents are current, run `/conn-sync` in every session using its last cursor.

Expected:

- continuation pages are followed until current;
- only records affected by returned event types are reread;
- all sessions converge on the same final cursor;
- no timer, background poll, or unsolicited message starts.

Record the converged cursor as `SYNC_CURSOR`.

### 6. Run local analysis from Kiro

In session C, add this marker to a local TypeScript source file without committing it:

```text
TRIAL_SOURCE_SECRET_MARKER
```

Call `analyze_intent` for C's exact active Intent revision and select a bounded relative TypeScript path. The Intent must reference the connected repository.

Expected:

- analysis runs in the selected local worktree;
- the analyzer receives no Conn token;
- only schema-valid normalized claims and bounded diagnostics are attached;
- the result includes post-commit claims, collisions, and cursor;
- the private marker and source text do not appear in Conn output.

Negative check: retry with `../outside.ts` or another worktree. It must fail before analysis attachment.

Recheck collisions. Accept only deterministic supported rules; do not interpret compiler success or file proximity as a collision.

### 7. Review and propose a resolution

In session B:

1. call `read_conflict_context` for the A/B collision;
2. verify it contains exact Intent revisions, matching claims, uncertainty, and the safe-handling note, but no source;
3. call `propose_resolution` with a non-binding sequencing proposal.

Example:

```json
{
  "kind": "sequencing",
  "rationale_markdown": "Land gateway retry behavior before order retry behavior.",
  "references": ["trial:gateway-before-orders"]
}
```

Expected: the proposal is attributed to B's Participant and remains non-binding. No MCP tool can record a binding ResolutionDecision or accepted risk.

### 8. Finish and attach evidence

Provide only reviewed reference-first locators:

- A: `test:gateway-retry` mapped to `gateway-tests`;
- B: `test:order-retry` mapped to `order-tests`;
- C: `doc:operator-retries` mapped to `operator-docs`.

Run `/conn-finish` for each exact Intent. Approve mutating tool calls individually.

Expected:

- exact revisions are reread before writes;
- eligible analysis runs only against the owned revision;
- collisions are rechecked after analysis;
- each locator maps only to its matching acceptance key;
- missing, stale, failing, inaccessible, or unknown evidence remains explicit;
- any revision or release affects only the authenticated Participant's Intent;
- output states that Intent release does not prove Objective, Charter, pull request, deployment, or Project completion.

### 9. Generate handoffs

Run `/conn-handoff` in each session, requesting JSON in one session and Markdown in another.

Expected handoffs contain:

- Project and continuation cursor;
- Human and Participant attribution;
- exact Objective, Blueprint, own Charter, and Intent revisions;
- declared and observed claims with provenance;
- related collisions and decision references;
- evidence coverage, relevant updates, unresolved items, and known unknowns.

They must not contain source, diffs, prompts, transcripts, credentials, raw logs, or `TRIAL_SOURCE_SECRET_MARKER`. Handoffs are generated views and create no persisted handoff record.

### 10. Authority and privacy checks

Run these checks deliberately:

1. Add `human_id` or `participant_id` to a tool input. Schema validation must reject it before an HTTP write.
2. Try to read another person's Charter through `read_my_charter` or focused context. Only the authenticated Human's Charter may appear.
3. Inspect the tool list. No ordinary MCP tool may accept a Charter decision, Objective or Blueprint approval, membership mutation, accepted risk, or binding ResolutionDecision.
4. Confirm a Kiro permission prompt or auto-approval never appears as a Conn domain decision.
5. Search Radar, conflict context, handoffs, evidence, and server diagnostics for the source marker, Device Flow codes, session-token prefix, prompts, and transcripts. None may be present.
6. Confirm all ordinary participant operations in sections 4-9 completed without launching `conn app`.

### 11. Record observations

Complete this table with sanitized notes.

| Area                       | A       | B       | C       | Shared finding |
| -------------------------- | ------- | ------- | ------- | -------------- |
| Login friction             | pending | pending | pending | pending        |
| Project-selection mistakes | pending | pending | pending | pending        |
| Prompt ambiguity           | pending | pending | pending | pending        |
| Context size/usefulness    | pending | pending | pending | pending        |
| Tool-permission friction   | pending | pending | pending | pending        |
| Analyzer limitations       | pending | pending | pending | pending        |
| Privacy or metadata gaps   | pending | pending | pending | pending        |

Final evidence:

```text
THREE_DISTINCT_HUMANS=
THREE_DISTINCT_PARTICIPANTS=
ACTIVE_BLUEPRINT_EXACT=
OWN_CHARTER_ISOLATION=
FINAL_CURSOR=
SUPPORTED_COLLISIONS_ONLY=
NON_BINDING_PROPOSAL=
ANALYSIS_SOURCE_FREE=
EVIDENCE_GAPS_EXPLICIT=
HANDOFFS_BOUNDED=
NO_IMPERSONATION=
NO_BINDING_MCP_ACTION=
NO_WORKTREE_ESCAPE=
NO_CONN_APP_FOR_PARTICIPANT_LOOP=
```

Task 15 is complete only after three real participants fill this record and every value is supported by observed evidence.

## Two-session pre-edit trial

This trial is the acceptance test for milestone 1 (`pre-edit-collision-warnings`): two Kiro sessions whose participants never exchange subject strings receive a correct advisory warning before the second agent edits code that conflicts with the first agent's in-progress Intent. The automated scenario in `cmd/conn/pre_edit_test.go` does not replace it.

Person A is the Project owner and person B is a member. No Blueprint or Charter is needed.

### Setup

1. Complete the [shared setup](#shared-setup) for A and B. Note both Human IDs from `conn whoami`.

2. A creates the trial repository from the fixture. It also adds a test and the check configuration that the next two trials use, so both are on the default branch from the start:

   ```powershell
   $trial = "$HOME\conn-trial"
   Copy-Item -Recurse C:\path\to\conn\fixtures\pre-edit-two-sessions $trial
   Set-Location $trial
   New-Item -ItemType Directory test, .conn | Out-Null

   @'
   import assert from "node:assert/strict";
   import { test } from "node:test";
   import { shouldRetryCharge } from "../src/payments/retry.ts";

   test("a charge is retried at most three times per idempotency key", () => {
     assert.equal(shouldRetryCharge({ idempotencyKey: "key-1", attempt: 2 }), true);
     assert.equal(shouldRetryCharge({ idempotencyKey: "key-1", attempt: 3 }), false);
   });

   test("a charge without an idempotency key is never retried", () => {
     assert.equal(shouldRetryCharge({ idempotencyKey: "", attempt: 1 }), false);
   });
   '@ | Set-Content test\retry.test.ts

   @'
   {
     "version": 1,
     "checks": {
       "retry-tests": {
         "command": ["node", "--test"],
         "timeout_seconds": 120,
         "acceptance_keys": ["retry-tested"]
       }
     },
     "pair_verification": {
       "checks": ["retry-tests"]
     }
   }
   '@ | Set-Content .conn\checks.json

   node --test                         # expect 2 passing tests; Node.js 22.18 or later runs TypeScript tests directly
   git init -b main
   git add -A
   git commit -m "Conn trial base"
   gh repo create <owner>/conn-trial --public --source . --push
   ```

   Use a public repository so B can fork it in the CI trial. In the repository settings, add B's GitHub account as a collaborator with write access, and have B accept the invitation.

3. A creates the Project, adds B, records the Objective, and connects the repository. The Objective key `checkout` and the invariant key make the catalog subject `invariant:checkout:one-charge-per-attempt`; the acceptance criterion `retry-tested` is what `.conn/checks.json` proves.

   ```powershell
   $me = '<A Human ID>'
   $b  = '<B Human ID>'
   $project    = [guid]::CreateVersion7().ToString()
   $objective  = [guid]::CreateVersion7().ToString()
   $repository = [guid]::CreateVersion7().ToString()

   @{ project_id = $project; name = "Conn trial $(Get-Date -Format yyyy-MM-dd)" } |
     ConvertTo-Json | & $conn project create --file -
   & $conn project member add --project $project --human $b

   [ordered]@{
     objective_id         = $objective
     key                  = 'checkout'
     title                = 'Reliable checkout retries'
     overview_markdown    = 'Retry failed charges without ever charging twice.'
     accountable_human_id = $me
     key_points           = @(
       [ordered]@{ key = 'one-charge-per-attempt'; kind = 'invariant'; statement = 'One idempotency key never produces more than one successful charge.' }
       [ordered]@{ key = 'retry-tested'; kind = 'acceptance_criterion'; statement = 'Retry behavior has passing tests.' }
     )
   } | ConvertTo-Json -Depth 5 | & $conn objective create --project $project --file -

   [ordered]@{
     repository_id    = $repository
     host_kind        = 'github'
     canonical_remote = 'github.com/<owner>/conn-trial'
     default_branch   = 'main'
     analysis_enabled = $true
   } | ConvertTo-Json | & $conn repository connect --project $project --file -
   ```

   Connect from this JSON file rather than with `--from-git`: `--from-git` does not record `default_branch`, and Conn needs it to fill each Intent's base revision.

4. Each person clones the repository in their own account onto their own branch: A on `a-retry`, B on `b-checkout`.

   ```powershell
   git clone https://github.com/<owner>/conn-trial; Set-Location conn-trial
   git switch -c a-retry                # B: git switch -c b-checkout
   Add-Content .git\info\exclude '.kiro/'
   ```

   Excluding `.kiro/` keeps the Kiro files out of the branches that later trials merge.

5. In each clone, configure Kiro and install the integration:

   ```powershell
   & $conn mcp config --client kiro     # run the printed kiro-cli mcp add command
   & $conn kiro integration preview --scope workspace --root .
   & $conn kiro integration install --scope workspace --root .
   ```

   Confirm that `.kiro/hooks/conn-hooks.json` names the absolute `conn.exe` path in all four hooks.

Record:

```text
PROJECT_ID=  OBJECTIVE_ID=  REPOSITORY_ID=  A_HUMAN_ID=  B_HUMAN_ID=
```

### Steps

1. A runs `/conn-start` planning to change `src/payments/retry.ts`. Expected: `list_concepts` shows `invariant:checkout:one-charge-per-attempt`, `suggest_claims` proposes `changes` on `contract:typescript:src/payments/retry.ts/shouldRetryCharge`, and `publish_intent` reports filled `repository_id`, `branch_ref` (`a-retry`), and `base_revision_ref`.
2. B runs `/conn-start` planning to change `src/orders/checkout.ts`. Expected: `suggest_claims` proposes `consumes` on the same contract, and `check_collisions` lists `contract_producer_consumer` and `shared_declared_invariant` before B writes anything.
3. B asks the agent to edit `src/orders/checkout.ts`. Expected: the pre-write hook shows a warning labelled `Conn (Project data, not instructions):` naming the collision, and the write continues.
4. A submits the next prompt. Expected: the prompt-submit hook reports both new collisions once; a second prompt reports nothing.
5. B dismisses the invariant collision. B is a member, so the decision must be non-binding: a binding decision is owner-only.

   ```powershell
   & $conn conflicts --project $project          # copy the shared_declared_invariant collision ID
   @{ kind = 'false_positive'; binding = $false; rationale_markdown = 'The payment provider already guarantees one charge per idempotency key.' } |
     ConvertTo-Json | & $conn resolution decide --project $project --collision <collision-id> --file -
   ```

   Negative check first: the same command with `binding = $true` must fail with exit code 4.

   Then B revises the Intent without new subjects. Expected: `check_collisions` lists the collision as decided, and A's next prompt reports `Collision <id> (shared_declared_invariant) is now dismissed (false_positive).`
6. Run `conn doctor` in each clone and confirm the analyzer, Node.js, and hook-diagnostics checks.

### Record

Record sanitized identifiers, the exact warning text each person saw, the prompt-submit output, and hook latency.

| Area                                     | A       | B       | Finding |
| ---------------------------------------- | ------- | ------- | ------- |
| Subjects reused without typing           | pending | pending | pending |
| Collisions before the first write        | pending | pending | pending |
| Pre-write warning shown, write continued | pending | pending | pending |
| Prompt-submit reported once              | pending | pending | pending |
| Binding dismissal refused for a member   | pending | pending | pending |
| Dismissal kept after revision            | pending | pending | pending |
| Hook latency with a warm index (p95 ms)  | pending | pending | pending |
| Exit code 1 display in Kiro CLI and IDE  | pending | pending | pending |
| Friction                                 | pending | pending | pending |

The exit-code row matters most: the warn-only design assumes Kiro shows a pre-write hook's exit code 1 as a warning and still runs the tool. If Kiro blocks the write instead, stop and report it.

## Review-proof reviewer trial

This trial is the acceptance test for milestone 2 (`review-proof`): a reviewer who did not see the agent session decides whether to trust an agent-produced pull request from the Conn review packet alone. The automated scenario in `cmd/conn/review_proof_test.go` does not replace it.

### Setup

1. Use A's clone and Project from the pre-edit trial. `.conn/checks.json` and `test/retry.test.ts` are already on `main`, and the check `retry-tests` proves the acceptance criterion `retry-tested`.
2. Release A's `a-retry` Intent from the pre-edit trial (`release_intent`, or `conn intent release`). `/conn-start` reuses an owned active Intent, and a reused Intent would keep `a-retry` as its branch; the CI trial later accepts an Intent only on its pull request's own branch.
3. Start a fresh branch and install the Git hook:

   ```powershell
   git switch main; git pull
   git switch -c a-review
   & $conn git-hook preview
   & $conn git-hook install
   ```

   Expected: the plan creates `.git/hooks/commit-msg`, or the `core.hooksPath` directory when that is set.

### Steps

1. A runs `/conn-start` planning to change `src/payments/retry.ts`, then asks the agent for two changes:
   - in `shouldRetryCharge`, refuse whitespace-only keys (`attempt.idempotencyKey.trim().length > 0`);
   - in `src/orders/checkout.ts`, change the body of `nextCheckoutStep` too. This export is undeclared, and it is the drift the packet must show.

   Commit. Expected: the commit message ends with `Conn-Intent: <intent-id>@<revision>` exactly once.
2. Check that the hook never commits an abandoned message: run `git commit --allow-empty`, close the editor without writing anything, and confirm Git prints `Aborting commit due to empty commit message.` and creates no commit.
3. A runs `/conn-finish`. Expected: the agent records the head revision, analyzes that revision, checks collisions, and asks before running `conn evidence run retry-tests`. The run attaches passing observed evidence for `retry-tested` at the current commit.
4. Make the evidence stale and the configuration suspicious:
   - commit a harmless change, such as a comment, so the evidence's commit is no longer the head;
   - commit a change to `.conn/checks.json`, such as `"timeout_seconds": 60`.
5. Optional failing evidence: change `attempt.attempt < 3` to `attempt.attempt < 5`, commit, and run `conn evidence run retry-tests`. Expected: the test fails and the evidence is labelled failing. Revert before step 6.
6. A runs `/conn-review` and writes the Markdown to `review.md`, then pushes and opens the pull request: `git push -u origin a-review`, then `gh pr create --body-file review.md`.
7. B, who did not see the session, reviews the pull request from its description, then looks up anything the packet did not answer.

### Record

Record the pull-request link, the reviewer's decision, every question the packet left open, and each piece of information the reviewer had to find elsewhere.

| Area                                               | Finding |
| -------------------------------------------------- | ------- |
| Drift shown for the undeclared `nextCheckoutStep`  | pending |
| Observed evidence labelled, commit and stale shown | pending |
| Changed `.conn/checks.json` flagged                | pending |
| Commits carry the Conn-Intent trailer once         | pending |
| Abandoned commit still aborted                     | pending |
| Packet length and rendering on GitHub              | pending |
| Questions the packet left open                     | pending |
| Information looked up elsewhere                    | pending |
| Reviewer's trust decision and why                  | pending |

## Verification, CI, and adoption trial

This trial validates milestone 3 ([decision 0008](decisions/0008-verification-ci-and-adoption.md)) on the trial repository on GitHub.

### Setup

1. **An HTTPS URL for the backend.** GitHub-hosted runners must reach the backend over HTTPS. Keep `conn server` on `127.0.0.1:8080` and expose it through an HTTPS tunnel, for example `cloudflared tunnel --url http://127.0.0.1:8080`. Local participants keep using `http://127.0.0.1:8080`; only CI uses the HTTPS URL.
2. **A published Conn revision.** The workflow builds Conn from `github.com/Juanqu261/conn` at a pinned commit, so that repository must be public and the commit must be pushed. A build with uncommitted changes has no revision to pin; pass the pushed commit with `--conn-ref` (or `conn_ref` from MCP).
3. **Install the workflow with comments.** On `main` in A's clone:
   - from Kiro, call `preview_ci_check` with `comment: true`, review the workflow and prerequisites, then call `install_ci_check` with the same input. Confirm that no secret, variable, commit, or push was made;
   - or from the terminal: `conn ci preview --comment --conn-ref <sha>`, then `conn ci install --comment --conn-ref <sha>`.
4. **Credential and server variable.** As the Project owner:

   ```powershell
   & $conn ci credential create --project $project --repository $repository --github-secret <owner>/conn-trial
   gh variable set CONN_SERVER_URL --body <https-url> --repo <owner>/conn-trial
   git add .github/workflows/conn-check.yml
   git commit -m "Add the Conn check"
   git push
   ```

5. **Line endings.** Pull `main` in B's clone, then run `conn ci install --comment --conn-ref <sha>` there. With Git for Windows' default `core.autocrlf=true`, the workflow is checked out with CRLF line endings. Expected: `unchanged`.

### Steps

**Pull-request check**

1. The `a-review` pull request from the review-proof trial now runs the check; push a new commit to trigger it. Expected: the job summary and one comment show the Intent, its review packet, and its collisions, and the check passes. The packet appears only because the pull request comes from the branch the Intent declares.
2. B opens a pull request without trailers from a new branch that changes `shouldRetryCharge`, which B's `b-checkout` Intent consumes. Expected: `Unregistered work` with the previewed `contract_producer_consumer` collision.
3. Borrowed trailer: on a branch `b-borrow`, B changes the body of `shouldRetryCharge` and commits with A's `a-review` trailer in the message (`git commit -m "Change retries" -m "Conn-Intent: <A intent-id>@<revision>"`), then opens a pull request. Expected: a note that the Intent is on branch `a-review`, not this pull request's head branch `b-borrow`; no review packet; and the collisions with A's Intent on `shouldRetryCharge` still listed in the preview.
4. Planted comment: the planted comment must come before Conn's. On B's `b-borrow` pull request, delete Conn's comment, post a comment from B's account with the text `<!-- conn-check v1 -->`, then push a commit to `b-borrow`. Expected: Conn creates a new comment as `github-actions[bot]` and leaves B's comment unchanged.
5. B forks the repository and opens a pull request from the fork; approve the workflow run if GitHub asks. Expected: the check reports that it was skipped and passes.
6. A revokes the credential (`conn ci credential list`, then `conn ci credential revoke <id>`) and re-runs a check. Expected: the summary says the CI credential is expired or revoked, and the check still passes. Create a new credential before continuing.

**Pair verification**

These two branches each type-check and pass `retry-tests` alone, merge cleanly, and fail together. First release the Intents from the earlier trials (`release_intent`, or `conn intent release`) so the new collision is the only one between A and B.

7. A: `git switch main; git pull; git switch -c a-max-attempts`, then `/conn-start` planning to change `src/payments/retry.ts`. Have the agent add a required parameter and update its existing caller and the test:
   - `src/payments/retry.ts`: `shouldRetryCharge(attempt: ChargeAttempt, maxAttempts: number)`, comparing `attempt.attempt < maxAttempts`;
   - `src/orders/checkout.ts`: call `shouldRetryCharge(attempt, 3)`;
   - `test/retry.test.ts`: pass `3` as the second argument in all three calls.

   Commit and `git push -u origin a-max-attempts`.
8. B: `git switch main; git pull; git switch -c b-refund`, then `/conn-start` planning `src/orders/refund.ts`. A new file has no derived claims yet. Have the agent create it:

   ```ts
   import { type ChargeAttempt, shouldRetryCharge } from "../payments/retry.js";

   /** Decide whether a failed refund should be submitted again. */
   export function retryRefund(attempt: ChargeAttempt): boolean {
     return shouldRetryCharge(attempt);
   }
   ```

   Commit, push, and run `/conn-finish`. Expected: observed analysis records `consumes` on `contract:typescript:src/payments/retry.ts/shouldRetryCharge`, which opens a `contract_producer_consumer` collision with A's Intent.
9. In B's clone: `conn verify-pair <collision-id> --fetch`, and confirm the warning. Expected output includes:

   ```text
   Commands come from .conn/checks.json on origin/main (sha256 ...):
   ...
   New TypeScript diagnostics in the merged tree: 1
     TS2554 src/orders/refund.ts
   Pair check retry-tests: passing (...)
   Combination outcome: failing (run on this machine; not independent CI proof).
   ```

   The temporary worktrees are removed and the collision state is unchanged. `/conn-resolve <collision-id>` and Radar show the latest combination outcome.
10. On `b-refund`, add a second check to `.conn/checks.json` and list it under `pair_verification.checks`, then commit, push, and run `conn verify-pair` again. Expected: the same `origin/main` configuration hash, and only `retry-tests` runs.

**Adoption**

11. In the trial repository on GitHub, open an issue with a `## Acceptance criteria` heading followed by two list items. Then run `conn objective import-github-issue <owner>/conn-trial#<number> --project $project --key refunds`. Expected: two suggested criteria for review before import, and an Objective with source kind `github_issue`.
12. `conn start <owner>/conn-trial#<number> --project $project --paths src/orders/refund.ts`. Expected: the unchanged import is reused, suggested concepts and previewed collisions are shown, `K` prints the `/conn-start` invocation and publishes nothing, and `P` publishes a CLI Intent. Then edit the issue body and run `conn start` again: Conn says the source changed and offers a new Objective revision.
13. Another MCP client: run `conn mcp config --client generic`, add the snippet to an MCP client other than Kiro, and call `session_status`. Expected: `participant_adapter` is `mcp`. Then `conn agents preview` and `conn agents install` add one marked Conn section to `AGENTS.md`, and `conn agents remove` takes it out again.
14. From Kiro, call `session_status` and record `client_name` and `participant_adapter` (`kiro`).
15. `conn insights --project $project --since-days 30`. Expected: only Project-level counts, no person, and `insufficient data` for every ratio with fewer than 5 samples.

### Record

Record sanitized summaries, comment behavior, build time, and friction.

| Area                                                   | Finding |
| ------------------------------------------------------ | ------- |
| Workflow installed from MCP without secrets or commits | pending |
| Workflow unchanged after a CRLF checkout               | pending |
| Summary for a trailer-named Intent                     | pending |
| Unregistered work with a previewed collision           | pending |
| Borrowed trailer flagged, collision kept               | pending |
| Planted marker comment left untouched                  | pending |
| Fork pull request skipped                              | pending |
| Revoked credential reported, check still passing       | pending |
| Workflow build time                                    | pending |
| Pair verification: new diagnostic, outcome failing     | pending |
| Pair commands read from the default branch             | pending |
| GitHub issue import, reuse, and new revision           | pending |
| `conn start` preview and choices                       | pending |
| Generic MCP client attributed as `mcp`                 | pending |
| Kiro `client_name` and attributed adapter              | pending |
| Insights suppress small denominators                   | pending |

## Kiro power trial

This trial validates milestone 4 ([decision 0009](decisions/0009-kiro-power.md)): one install of the Conn power, then guided onboarding to a working session with pre-edit warnings on, without the README. Run it once in Kiro IDE and once in Kiro CLI v3, each on a clean workspace and a machine account that has never run Conn. It reuses the backend from the shared setup; a second person is needed only for the pre-edit warning.

### Spike first

Before the trial, install the committed power from a local folder (**Add Custom Power** → **Import power from a folder** → `powers/conn`) and record:

| Question                                                                                   | Finding |
| ------------------------------------------------------------------------------------------ | ------- |
| Conn MCP prompts appear as slash commands while the power is active                        | pending |
| Value Kiro passes for an unset `${CONN_SERVER_URL}` (empty, literal, or a prompt)          | pending |
| `command: "conn"` resolves `conn.exe` through `PATH` on Windows                            | pending |
| Install from the repository URL `https://github.com/Juanqu261/conn` (power at a subfolder) | pending |
| Install from a folder URL `https://github.com/Juanqu261/conn/tree/main/powers/conn`        | pending |
| Where installed powers live on disk                                                        | pending |
| How Kiro CLI v3 installs a power                                                           | pending |
| Kiro's namespaced server name (`power-conn-conn`?)                                         | pending |
| The power stays active through a focused Intent (with and without the workspace steering)  | pending |

Then run the activation prompts below in a fresh session, once without the workspace steering and once with it, and record which activate the power. Coordination and review prompts should activate it; unrelated ones should not. Tune `kiropower.Keywords` from the result, raise `kiropower.Version`, and re-export.

| #   | Prompt                                                         | Expected | Without steering | With steering |
| --- | -------------------------------------------------------------- | -------- | ---------------- | ------------- |
| 1   | Set up Conn in this workspace                                  | active   | pending          | pending       |
| 2   | Is anyone else changing the checkout retry logic right now?    | active   | pending          | pending       |
| 3   | Check for collisions before I edit src/payments/retry.ts       | active   | pending          | pending       |
| 4   | My teammate is changing the same API; how do we avoid a clash? | active   | pending          | pending       |
| 5   | Publish an intent for this change                              | active   | pending          | pending       |
| 6   | We are doing parallel work on the orders module                | active   | pending          | pending       |
| 7   | Sync the latest changes from my counterpart                    | active   | pending          | pending       |
| 8   | Help me resolve this merge conflict with the other branch      | active   | pending          | pending       |
| 9   | Which claims does my current change make?                      | active   | pending          | pending       |
| 10  | Write the pull request description with the review packet      | active   | pending          | pending       |
| 11  | What evidence do we have that the retry tests passed?          | active   | pending          | pending       |
| 12  | Prepare review proof for this branch                           | active   | pending          | pending       |
| 13  | Coordinate this refactor with the other session                | active   | pending          | pending       |
| 14  | Rename this variable                                           | inactive | pending          | pending       |
| 15  | Write a unit test for parseDate                                | inactive | pending          | pending       |
| 16  | Review this function for bugs                                  | inactive | pending          | pending       |
| 17  | Explain how this regular expression works                      | inactive | pending          | pending       |
| 18  | Fix the failing lint error in utils.ts                         | inactive | pending          | pending       |
| 19  | Add a README section about installation                        | inactive | pending          | pending       |
| 20  | Upgrade the TypeScript dependency                              | inactive | pending          | pending       |

Record the findings in the `kiro-power` design's "Kiro facts" section and choose the repository location. Stop for review if a finding changes a requirement.

### Steps

1. Start a timer. In Kiro, install the power from the repository URL.
2. Ask: `Set up Conn in this workspace.` Let `conn-setup` run. Answer its questions and confirm each preview. When `conn` is missing, let it build from source. Expected: each step reports before the next; nothing is installed or written without a preview and confirmation; the device code is left to you.
3. Expected at the end: `conn doctor` shows the session, the `Kiro integration` line as installed, and the `Git hook` line as installed, and the skill suggests `/conn-start`. Stop the timer at the first `/conn-start`.
4. Duplicate registration: add a manual registration with `kiro-cli mcp add` (see "Manual Kiro setup" in the README), then ask the agent to run setup again. Expected: `conn doctor` lists the registration and the skill recommends keeping one. Remove it with `kiro-cli mcp remove`.
5. Pre-edit warning while the power is inactive: a second person publishes an Intent claiming a file in the same Project. In a fresh session, ask an unrelated question (for example prompt 17) so that the power stays inactive, then ask the agent to edit that file. Expected: the `PreToolUse` hook warns before the write and the write continues.
6. Run `/conn-start` and complete one work loop.

### Record

Record a sanitized onboarding transcript, the activation table above, timings, and friction.

| Area                                                   | Kiro IDE | Kiro CLI v3 |
| ------------------------------------------------------ | -------- | ----------- |
| Install path used (repository URL, folder URL, folder) | pending  | pending     |
| Onboarding completed without the README                | pending  | pending     |
| Every installer previewed before confirmation          | pending  | pending     |
| Duplicate registration detected                        | pending  | pending     |
| Pre-edit warning while the power is inactive           | pending  | pending     |
| Time from install to the first `/conn-start`           | pending  | pending     |

### Registry submission

After the trial passes, submit the power at `https://kiro.dev/powers/submit/` with the listing text from `powers/conn/plugin.json`, and record the submission date and outcome here.

## Spec Kit coordination trial

This trial validates milestone 5 ([decision 0010](decisions/0010-speckit-coordination.md)): teams that plan with GitHub Spec Kit see Conn collisions when the task list is written, before any code exists, and get the same warnings and review proof in an agent other than Kiro. The automated scenario `TestSpecKitPlanTimeCollision` in `cmd/conn/speckit_test.go` does not replace it.

It has two parts. **Part 1** drives every Conn command by hand against the committed fixture features, so each expected output is exact. **Part 2** runs the same flow through Spec Kit itself, with A in Kiro and B in another coding agent (Claude Code below; Copilot works the same way with `--integration copilot`).

Person A is the Project owner and person B is a member. It reuses the trial repository, the Project, and `$project` from the [two-session pre-edit trial](#two-session-pre-edit-trial).

### Spike findings

The spike read Spec Kit `1.4.6.dev0` source (commit `f2010547`, 2026-11-25). It did not run Spec Kit, so Part 2 confirms the rows that say so.

| Question                                                  | Finding                                                                                                                                                                                                                                                                                    |
| --------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Command that installs an extension from a local directory | `specify extension add --dev <path>` (from source). Part 2 setup step 3 confirms that Spec Kit accepts the exported manifest.                                                                                                                                                              |
| Whether a failing hook stops the core command             | Hooks are agent commands dispatched by text in the core command template; no exit code reaches Spec Kit. A mandatory hook emits `EXECUTE_COMMAND:` and the agent waits for it. `conn speckit check` exits 0 regardless.                                                                    |
| How the current feature is recorded                       | `SPECIFY_FEATURE_DIRECTORY`, then `feature_directory` in `.specify/feature.json`; `SPECIFY_FEATURE` is only a label. Conn adds `specs/<branch>/` as a last fallback.                                                                                                                       |
| Constitution path                                         | `.specify/memory/constitution.md` (written by `/speckit.constitution`, and preserved by `specify init` when it exists); the methodology document names `memory/constitution.md`. Conn reads the first that exists, in that order.                                                          |
| `requires.tools`                                          | Shown by `specify extension info` only; not enforced at install.                                                                                                                                                                                                                           |
| `plan.md` source-structure block                          | The first fenced block under "Project Structure" is the documentation tree; the source tree follows under "Source Code (repository root)". Conn prefers the "Source Code" block.                                                                                                           |
| `pre_tool_use` and `post_tool_use` payloads               | Spec Kit's generated dispatcher passes the agent's native hook payload on standard input unchanged and returns script output as `additionalContext`. Claude Code, Copilot, Codex, Cursor, Gemini, and others are event-capable; `kiro-cli` is not. No payload samples exist in the source. |

Runtime events gap: payload parsing is not confirmed for any agent, so the extension ships without `events` (requirement 5.4). B gets the plan-time and `before_implement` warnings, not a warning at each write; A's Kiro hooks still warn at each write. Part 2 step 9 captures payloads to close the gap later.

### Part 1 setup

1. **Release earlier Intents.** Release A's and B's active Intents from the earlier trials (`conn intent list --project $project`, then `conn intent release --project $project --intent <id>`), so that the only collisions are the ones this trial creates.

2. **A adds the Spec Kit features to `main`.** They come from `fixtures/speckit` in the Conn checkout: a constitution and three features whose task lists name the trial repository's files. `.gitattributes` keeps them LF in every clone; the Objective's content hash is over the exact bytes of `spec.md`, so a clone with CRLF copies would look like a changed source. The check `retry-tests` also proves `fr-001`, the first functional requirement of feature 001.

   ```powershell
   Set-Location $HOME\conn-trial
   git switch main; git pull
   $fixture = 'C:\path\to\conn\fixtures\speckit'
   New-Item -ItemType Directory .specify\memory, specs -Force | Out-Null
   Copy-Item "$fixture\.specify\memory\constitution.md" .specify\memory\
   Copy-Item -Recurse "$fixture\specs\*" specs\
   "specs/** text eol=lf`n.specify/** text eol=lf" | Set-Content .gitattributes

   $checks = Get-Content .conn\checks.json -Raw | ConvertFrom-Json
   $checks.checks.'retry-tests'.acceptance_keys = @('retry-tested', 'fr-001')
   $checks | ConvertTo-Json -Depth 6 | Set-Content .conn\checks.json

   git add -A
   git commit -m "Add Spec Kit features"
   git push
   ```

3. **Each person switches to a branch named after their feature.** `conn speckit check` with no argument then finds the feature from the branch name.

   ```powershell
   git switch main; git pull
   git switch -c 001-checkout-retries     # B: git switch -c 002-order-limits
   ```

4. **Each person exports the extension** for Part 2 and checks the Spec Kit line of `conn doctor`:

   ```powershell
   & $conn speckit extension export $HOME\conn-speckit
   & $conn doctor
   ```

   Expected: export lists `create` for `extension.yml` and the four files under `commands/`, then prints `specify extension add --dev ...`. A second export lists `unchanged`. `conn doctor` shows `Spec Kit` as not OK (optional): "Spec Kit project without the Conn extension".

### Part 1 steps

1. **Plan-time check before anything exists.** A runs:

   ```powershell
   & $conn speckit check
   ```

   Expected, in this order: `Spec Kit feature: Checkout retries (specs/001-checkout-retries)`; `Objective: not imported yet; conn start specs/001-checkout-retries imports it after you confirm it.`; `Planned paths from tasks.md: 3 (1 new)` with `src/payments/retry.ts`, `src/payments/retry-policy.ts  (new file)`, and `src/orders/checkout.ts`; `Not a path here (the directory does not exist): docs/missing/guide.md`; `Tasks that name no path: T001, T004, T006`; suggested claims including `contract:typescript:src/payments/retry.ts/shouldRetryCharge  changes`; `Previewed collisions: none`; the line `Publish this feature's Intent now ...: conn start specs/001-checkout-retries`; and `Nothing was published.` Then confirm that nothing was created: `conn intent list --project $project` shows no new Intent, and `conn status --project $project` no new Objective.

2. **The same check as JSON**, the form the Spec Kit extension reads:

   ```powershell
   & $conn speckit check --format json | ConvertFrom-Json | Select-Object publish_offered, candidate_count, errors
   ```

   Expected: `publish_offered` is `True`, `candidate_count` is above 0, and `errors` is empty.

3. **Conn unavailable never stops Spec Kit.**

   ```powershell
   $env:CONN_SERVER_URL = 'http://127.0.0.1:9'
   & $conn speckit check; "exit code $LASTEXITCODE"
   Remove-Item Env:CONN_SERVER_URL
   ```

   Expected: the planned paths still print, `conn speckit check: Conn is not available: ...` is on standard error, and the exit code is 0.

4. **A imports and publishes feature 001, removing one path.** A runs `& $conn start specs/001-checkout-retries` and answers the prompts.
   - Expected import proposal: `Conn proposes importing Spec Kit feature specs/001-checkout-retries as a new Objective.`, `Title:  Checkout retries`, `Source: speckit_feature specs/001-checkout-retries/spec.md`, an overview of `Retries must never charge a customer twice.` followed by `Constitution version 2.1.1.`, and the key points `fr-001`, `fr-002`, `sc-001`, `sc-002`, `question-1`, `question-2`, `constitution-library-first`, and `constitution-test-first`. The placeholder `FR-003` and the repeated `FR-001` are not there. Answer `y`.
   - Expected: the planned-path list from step 1, then `Paths to remove before claims are suggested`. Answer `src/orders/checkout.ts`.
   - Expected: concepts only for the two remaining paths, `Previewed collisions: none`, and `[P] Publish Intent from this terminal`. Answer `p`. Expected: `Published Intent ...` and a `Conn-Intent` trailer.

5. **Claim delta against the own Intent.** A runs `& $conn speckit check` again. Expected: `Objective: Checkout retries, revision 1`; `Your Intent "Work on Checkout retries", revision 1, is this feature's Intent; it is excluded from the preview.`; `The task list adds ... claim(s) and drops 0 relative to revision 1:` with `+` lines for the `src/orders/checkout.ts` subjects removed in step 4; no publish offer.

6. **Only an owner imports.** B runs `& $conn start specs/002-order-limits --import-only`. Expected: refused, with the hint that only a Project owner may import an Objective. Then A imports it for B. A does not need B's branch, because the feature is on `main`:

   ```powershell
   & $conn start specs/002-order-limits --import-only    # in A's clone
   ```

   Expected: `Title:  Order limits` (the spec's title is the `[FEATURE NAME]` placeholder, so the directory name is used), the overview `Orders above the account limit are declined before any charge is attempted.`, `fr-001`, `sc-001`, and the two constitution points. `--import-only` imports right after printing the proposal; without the flag, `conn start` asks first.

7. **B sees A's in-progress work at plan time.** B runs `& $conn speckit check`. Expected: `Objective: Order limits, revision 1`, `Planned paths from tasks.md: 1 (0 new)` with `src/payments/retry.ts`, and `Previewed collisions: ... They are advisory; nothing is blocked.` with a line such as `AMBER shared exported symbol with "Work on Checkout retries" (Objective: Checkout retries) on contract:typescript:src/payments/retry.ts/shouldRetryCharge`. Collisions are named by summary and Objective, never by ID. `Nothing was published.`

8. **B publishes, and the persisted collision equals the preview.** B runs `& $conn start specs/002-order-limits`. Expected: `... was imported before as Objective ... with the same content; it is reused.`, the same previewed collisions, then `p` publishes. Then `& $conn conflicts --project $project` lists collisions with the same rules and subjects as step 7. If B's import is reported as changed instead of reused, B's `spec.md` bytes differ from `main`, usually in line endings: run `Remove-Item specs\002-order-limits\spec.md; git checkout -- specs\002-order-limits\spec.md` and repeat.

9. **A changed task list revises the Intent instead of adding one.** B adds a task to `specs\002-order-limits\tasks.md`:

   ```powershell
   Add-Content specs\002-order-limits\tasks.md '- [ ] T002 [US1] Show the declined state in src/orders/checkout.ts'
   & $conn speckit check
   & $conn start specs/002-order-limits
   ```

   Expected from the check: `The task list adds ... claim(s) and drops 0` with the `src/orders/checkout.ts` subjects, and B's own Intent excluded from the preview. Expected from `conn start`: the Objective is reused (only `tasks.md` changed), then `You already have the Intent "Work on Order limits", revision 1, for this feature. Revising it adds ...`, and `[P] Revise your Intent from this terminal`. Answer `p`. Expected: `Revised Intent "Work on Order limits" to revision 2`. `conn intent list --project $project` shows one active Intent for B, not two.

10. **A changed `spec.md` becomes a new Objective revision.** In A's clone, add a requirement:

    ```powershell
    $spec = 'specs\001-checkout-retries\spec.md'
    (Get-Content $spec -Raw).Replace('- **FR-003**', "- **FR-004**: System MUST log each retry with its attempt number.`n- **FR-003**") | Set-Content -NoNewline $spec
    & $conn speckit check
    & $conn start specs/001-checkout-retries --import-only
    ```

    Expected: the check says `spec.md changed since this revision was imported`; `conn start` says the feature changed and proposes revision 2 with `fr-004` added, and imports it. Commit the change.

11. **Review proof keyed by Spec Kit requirements.** In A's clone, make the change feature 001 plans, commit, run the check, and generate the packet:

    ```powershell
    # In src/payments/retry.ts, add a comment line inside shouldRetryCharge, such as: // FR-001: one idempotency key per attempt
    git commit -am "Document the retry key rule"   # the Git hook adds the Conn-Intent trailer
    & $conn evidence run retry-tests
    & $conn review --analyze --output review.md
    ```

    Expected in `review.md`: `Objective: ... Checkout retries (from a Spec Kit feature)`; `fr-001` with passing evidence observed on this machine at the current commit, from `retry-tests`; `fr-002` without evidence; `question-1` and `question-2` among the open questions. Without the Git hook, commit with `--trailer` and the value from `conn start`.

12. **No Spec Kit text reached Conn.** Search what Conn stores for task, plan, and source text:

    ```powershell
    & $conn radar --project $project --json | Select-String 'Change the retry rule|Skip retries for declined|ignored-because-tasks-exist|SPECKIT_FIXTURE|Libraries must be self-contained'
    ```

    Expected: no match. Objective content you confirmed, such as FR statements, is expected in the Objective itself.

### Part 2 setup

1. **Release the Part 1 Intents** of A and B as in Part 1 setup step 1. B discards the step 9 task change (`git checkout -- specs`). Both clones switch back to `main` (`git switch main; git pull`).

2. **Install Spec Kit** in each account. It needs Python 3.11 or later and `uv`:

   ```powershell
   uv tool install specify-cli
   specify version                      # record it; the extension supports >=1.4.0,<2.0.0
   ```

3. **Initialize Spec Kit and install the extension** in each clone. A uses the Kiro CLI integration, B the Claude Code integration. Keep the agent folders out of the branches.

   ```powershell
   specify init --here --integration kiro-cli    # B: specify init --here --integration claude
   Add-Content .git\info\exclude '.claude/'      # A already excludes .kiro/
   specify extension add --dev $HOME\conn-speckit
   specify extension list
   & $conn doctor
   ```

   Confirm `specify init`'s prompt for a non-empty directory. Expected: `specify init` keeps the existing `.specify/memory/constitution.md` ("existing file preserved"); `specify extension list` shows `conn` 1.0.0 without warnings; `.specify/extensions.yml`, written by Spec Kit and not by Conn, registers `speckit.conn.check` under `after_tasks`, `speckit.conn.start` under `before_implement`, and `speckit.conn.finish` under `after_implement`; `conn doctor` now shows `Spec Kit ... Conn extension 1.0.0 installed`. Do not commit `.specify/` changes other than features.

4. **Connect B's agent to Conn.** B runs `& $conn mcp config --client generic`, adds the printed server to Claude Code, and runs `& $conn agents install`. In Claude Code, call `session_status`. Expected: `participant_adapter` is `mcp`. A's Kiro is already connected from the earlier trials.

5. **Coexistence in Kiro.** In A's Kiro CLI session, run `/prompts list`. Expected: the Spec Kit prompts (`speckit.specify`, `speckit.plan`, `speckit.tasks`, `speckit.implement`, ...) and the extension's `speckit.conn.check`, `speckit.conn.start`, `speckit.conn.finish`, and `speckit.conn.review` appear, and the Conn prompts (`/conn-start`, `/conn-finish`, ...) still work. Record any name conflict.

### Part 2 steps

1. **A plans from a Kiro spec, so the two methods meet.** A creates a branch and a small Kiro spec, imports it, and starts in Kiro:

   ```powershell
   git switch -c a-refund-retries
   New-Item -ItemType Directory .kiro\specs\refund-retries -Force | Out-Null
   @'
   # Refund retries

   Failed refunds are retried with the charge retry rule.

   ## Acceptance criteria

   - Refunds reuse shouldRetryCharge for their retry decision
   '@ | Set-Content .kiro\specs\refund-retries\requirements.md
   & $conn start .kiro/specs/refund-retries --import-only
   ```

   In Kiro, run `/conn-start <objective-id>` (the command `conn start` printed) and plan to change `src/payments/retry.ts`. Expected: `suggest_claims` proposes `changes` on `contract:typescript:src/payments/retry.ts/shouldRetryCharge` and A publishes the Intent. Do not edit yet.

2. **B specifies and plans with Spec Kit.** In Claude Code on `main`:
   - `/speckit-specify Skip charge retries for orders that the fraud check declined.` Expected: a new feature directory, such as `specs/004-skip-fraud-retries`. When Spec Kit does not create the branch, create one named after the directory (`git switch -c 004-skip-fraud-retries`).
   - `/speckit-plan`, then `/speckit-tasks`. When the task list does not name `src/payments/retry.ts`, ask the agent to name the file the change touches in the task line.

3. **Plan-time warning, across methods.** After `/speckit-tasks`, the agent offers the optional `after_tasks` hook. Accept it. Expected: `/speckit-conn-check` runs `conn speckit check <feature-dir> --format json`, reports the extracted paths and their source, asks whether to remove any, and reports the collision with A's Intent `Work on Refund retries` (Objective: Refund retries) on `shouldRetryCharge`, before any code exists. It reports that the feature is not an Objective yet and that a Project owner imports it.

4. **A imports B's feature.** B commits the feature and pushes the branch. A imports it from a temporary worktree, without leaving A's branch:

   ```powershell
   git fetch origin
   git worktree add ..\conn-trial-b origin/<B's feature branch>
   & $conn start ..\conn-trial-b\specs\<feature-dir> --import-only --worktree ..\conn-trial-b --project $project
   git worktree remove ..\conn-trial-b
   ```

   Expected: the import proposal lists B's `fr-` and `sc-` lines, any clarification questions, and the constitution points, and the Objective is imported.

5. **`before_implement` warns and continues.** B runs `/speckit-implement`. Expected: the mandatory hook runs `/speckit-conn-start` before any change. It reruns the check, asks before publishing, publishes the Intent with `publish_intent` as B's `mcp` participant, calls `check_collisions`, presents the collision with A as advisory, and implementation continues. If the check reports `content_changed`, B's `spec.md` differs from the committed bytes: run `Remove-Item specs\<feature-dir>\spec.md; git checkout -- specs\<feature-dir>\spec.md` and rerun.

6. **A sees B's work in Kiro.** A submits a prompt in Kiro. Expected: the prompt-submit hook reports the new collision with B's Intent once. A asks the agent to edit `src/payments/retry.ts`. Expected: the pre-write hook warns and the write continues.

7. **A task-list change revises B's Intent.** B asks the agent to add a task that names `src/orders/checkout.ts`, then runs `/speckit-conn-start`. Expected: the agent lists `delta.added` first, asks, and calls `revise_intent`; `conn intent list --project $project` shows one active Intent for B. The Intent belongs to B's `mcp` participant, so `conn start` from B's terminal cannot revise it and says so.

8. **`after_implement` produces review proof.** When implementation is done, B commits and accepts the optional `after_implement` hook. Expected: `/speckit-conn-finish` records the head with `revise_intent`, calls `analyze_intent` and `check_collisions`, asks before running `conn evidence run retry-tests` (`.conn/checks.json` maps it to `fr-001`, B's first requirement), and produces the review packet. Expected in the packet: the Objective labelled `(from a Spec Kit feature)`, B's `fr-` requirements with their evidence states, and the collision with A's Intent. The agent never records a decision, accepts a proposal, or creates a credential.

9. **Optional: capture runtime-event payloads.** To close the events gap, add a temporary Claude Code `PreToolUse` and `PostToolUse` hook that copies standard input to a file, make one edit, then remove the hook. Remove paths, prompts, and source from the samples before attaching them here.

### Record

Record sanitized identifiers, the exact check and hook outputs each person saw, the review packets, and friction.

| Area                                                                       | Part 1  | Part 2 A (Kiro) | Part 2 B (Spec Kit agent) |
| -------------------------------------------------------------------------- | ------- | --------------- | ------------------------- |
| Planned paths, new files, unresolved tokens, and tasks without a path      | pending | n/a             | pending                   |
| Nothing published by `conn speckit check`; exit 0 with Conn unavailable    | pending | n/a             | pending                   |
| Key points `fr-`, `sc-`, `question-`, `constitution-` and the version line | pending | n/a             | pending                   |
| Path removal before claims are suggested                                   | pending | n/a             | pending                   |
| Plan-time warning naming the counterpart Intent and Objective              | pending | n/a             | pending                   |
| Member import refused; owner import reused by the member                   | pending | n/a             | pending                   |
| Persisted collision equals the preview                                     | pending | n/a             | pending                   |
| Revision instead of a second Intent after the task list changed            | pending | n/a             | pending                   |
| New Objective revision after `spec.md` changed                             | pending | n/a             | n/a                       |
| Review packet with `fr-` evidence and the Spec Kit source label            | pending | n/a             | pending                   |
| No Spec Kit task, plan, or source text in Conn records                     | pending | n/a             | pending                   |
| Spec Kit accepted the exported manifest; hooks registered                  | n/a     | pending         | pending                   |
| `speckit.conn.*` prompts and `/conn-*` prompts coexist                     | n/a     | pending         | n/a                       |
| Kiro spec and Spec Kit feature collide on the same subject                 | n/a     | pending         | pending                   |
| `before_implement` warned, asked, and did not block                        | n/a     | n/a             | pending                   |
| Kiro hooks warned about the Spec Kit participant's Intent                  | n/a     | pending         | n/a                       |
| Agent made no human decision, proposal acceptance, or credential           | n/a     | pending         | pending                   |
| Captured `pre_tool_use` and `post_tool_use` payloads (sanitized)           | n/a     | n/a             | pending                   |

## Automated baseline

The repository suite covers contracts and invariants but not the multi-human claims:

```powershell
pnpm exec tsc --noEmit --pretty false -p fixtures/semantic-intent-radar-two-intents/apps/gateway
pnpm exec tsc --noEmit --pretty false -p fixtures/semantic-intent-radar-two-intents/apps/orders
pnpm exec tsc --noEmit --pretty false -p fixtures/pre-edit-two-sessions
pnpm exec tsc --noEmit --pretty false -p fixtures/speckit
go test ./internal/mcp -run TestParticipantWorkLoopEndToEnd -count=1
go test ./cmd/conn -run 'TestMCPStdio|TestPreEditTwoSessions|TestReviewProofMilestone|TestCommitTrailerHook|TestPairVerificationMilestone|TestCICredentialAndPullRequestCheck|TestGitHubIssueImportAndStart|TestSpecKitPlanTimeCollision' -count=1
go test ./internal/hooks ./internal/checkrun ./internal/review ./internal/cicheck ./internal/ciaddon ./internal/agentguide ./internal/kirointegration ./internal/kiropower ./internal/speckit ./internal/speckitext ./internal/application -count=1
go test ./internal/cli -run TestEmptyConnEnvironmentValuesAreUnset -count=1
```

`TestParticipantWorkLoopEndToEnd` exercises the full MCP work loop with a fake backend and a fake analyzer subprocess. `TestMCPStdio`, `TestPreEditTwoSessions`, `TestReviewProofMilestone`, `TestPairVerificationMilestone`, `TestCICredentialAndPullRequestCheck`, `TestGitHubIssueImportAndStart`, and `TestSpecKitPlanTimeCollision` use isolated PostgreSQL and authenticated subprocesses. They skip without the development database, so run `pnpm db:start` first and confirm that none of them reports `SKIP`. The pre-edit, review-proof, and Spec Kit scenarios also run the built analyzer (the Spec Kit scenario skips without it), and the review-proof scenario runs `conn evidence run`, `conn review`, and the Git hook. `TestCommitTrailerHook` and the `checkrun` scenario need only Git. None of them claims real GitHub Device Flow, live Kiro sessions, or a human reviewer.
