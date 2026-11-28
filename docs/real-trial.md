# Manual trials

These trials are the real-world acceptance tests that automated tests cannot replace. Each needs real people, GitHub accounts, and Kiro sessions. Do not mark a trial complete from expected results alone; record what you observed.

| Trial                                                                        | People | Needs                                                    |
| ---------------------------------------------------------------------------- | ------ | -------------------------------------------------------- |
| [Three-session Kiro trial](#three-session-kiro-trial)                        | 3      | Local backend, Blueprints and Charters                   |
| [Two-session pre-edit trial](#two-session-pre-edit-trial)                    | 2      | Local backend, a GitHub repository made from the fixture |
| [Review-proof reviewer trial](#review-proof-reviewer-trial)                  | 2      | The pre-edit trial repository                            |
| [Verification, CI, and adoption trial](#verification-ci-and-adoption-trial) | 2      | An HTTPS URL for the backend, GitHub Actions             |

Run the last three in order: each reuses the repository and Project of the one before.

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

| Person | Conn role | Accepted cell          | Independent environment                         |
| ------ | --------- | ---------------------- | ----------------------------------------------- |
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

| Area                                                  | Finding |
| ----------------------------------------------------- | ------- |
| Drift shown for the undeclared `nextCheckoutStep`     | pending |
| Observed evidence labelled, commit and stale shown    | pending |
| Changed `.conn/checks.json` flagged                   | pending |
| Commits carry the Conn-Intent trailer once            | pending |
| Abandoned commit still aborted                        | pending |
| Packet length and rendering on GitHub                 | pending |
| Questions the packet left open                        | pending |
| Information looked up elsewhere                       | pending |
| Reviewer's trust decision and why                     | pending |

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

| Area                                                    | Finding |
| ------------------------------------------------------- | ------- |
| Workflow installed from MCP without secrets or commits  | pending |
| Workflow unchanged after a CRLF checkout                | pending |
| Summary for a trailer-named Intent                      | pending |
| Unregistered work with a previewed collision            | pending |
| Borrowed trailer flagged, collision kept                | pending |
| Planted marker comment left untouched                   | pending |
| Fork pull request skipped                               | pending |
| Revoked credential reported, check still passing        | pending |
| Workflow build time                                     | pending |
| Pair verification: new diagnostic, outcome failing      | pending |
| Pair commands read from the default branch              | pending |
| GitHub issue import, reuse, and new revision            | pending |
| `conn start` preview and choices                        | pending |
| Generic MCP client attributed as `mcp`                  | pending |
| Kiro `client_name` and attributed adapter               | pending |
| Insights suppress small denominators                    | pending |

## Automated baseline

The repository suite covers contracts and invariants but not the multi-human claims:

```powershell
pnpm exec tsc --noEmit --pretty false -p fixtures/semantic-intent-radar-two-intents/apps/gateway
pnpm exec tsc --noEmit --pretty false -p fixtures/semantic-intent-radar-two-intents/apps/orders
pnpm exec tsc --noEmit --pretty false -p fixtures/pre-edit-two-sessions
go test ./internal/mcp -run TestParticipantWorkLoopEndToEnd -count=1
go test ./cmd/conn -run 'TestMCPStdio|TestPreEditTwoSessions|TestReviewProofMilestone|TestCommitTrailerHook|TestPairVerificationMilestone|TestCICredentialAndPullRequestCheck|TestGitHubIssueImportAndStart' -count=1
go test ./internal/hooks ./internal/checkrun ./internal/review ./internal/cicheck ./internal/ciaddon ./internal/agentguide ./internal/kirointegration ./internal/application -count=1
```

`TestParticipantWorkLoopEndToEnd` exercises the full MCP work loop with a fake backend and a fake analyzer subprocess. `TestMCPStdio`, `TestPreEditTwoSessions`, `TestReviewProofMilestone`, `TestPairVerificationMilestone`, and `TestCICredentialAndPullRequestCheck` use isolated PostgreSQL and authenticated subprocesses. They skip without the development database, so run `pnpm db:start` first and confirm that none of them reports `SKIP`. The pre-edit and review-proof scenarios also run the built analyzer, and the review-proof scenario runs `conn evidence run`, `conn review`, and the Git hook. `TestCommitTrailerHook` and the `checkrun` scenario need only Git. None of them claims real GitHub Device Flow, live Kiro sessions, or a human reviewer.
