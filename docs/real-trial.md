# Three-session Kiro trial

This is the remaining real-world acceptance test for the Kiro-native MVP. It requires three people, three GitHub accounts, and three independent Kiro V3 sessions. Automated tests cannot substitute for this evidence.

The trial validates participant work without launching `conn app` for Intents, analysis, collisions, proposals, evidence, sync, finish, or handoff. Human-authoritative Project, membership, Objective, Blueprint, and Charter actions still use `conn app` or direct CLI commands.

Do not record OAuth codes, Conn tokens, private prompts, repository source, or credentials. The local PostgreSQL volume is persistent; use a uniquely named Project and do not delete the volume as routine cleanup.

## Participants

| Person | Conn role | Accepted cell          | Independent environment             |
| ------ | --------- | ---------------------- | ----------------------------------- |
| A      | owner     | gateway behavior       | GitHub account A and Kiro session A |
| B      | member    | order behavior         | GitHub account B and Kiro session B |
| C      | member    | operator documentation | GitHub account C and Kiro session C |

Use separate machines or operating-system accounts so each person has an independent credential store. Each Kiro session must use its own workspace clone. Confirm every identity with `session_status`; never infer identity from the terminal name.

## 1. Validate and start local infrastructure

From the backend checkout:

```powershell
pnpm install --frozen-lockfile
pnpm check
pnpm test
pnpm build
pnpm db:start
pnpm db:migrate
$env:CONN_GITHUB_CLIENT_ID = '<device-flow-enabled GitHub OAuth client ID>'
go run ./cmd/conn server
```

Keep the server running. For a multi-machine local trial, expose the development server only on the trusted test network and use one agreed `CONN_SERVER_URL`. Production use requires HTTPS.

Each participant clone must run `pnpm install --frozen-lockfile` and `pnpm build` so `packages/typescript-analyzer/dist/cli.js` exists locally.

## 2. Configure three Kiro sessions

In each participant workspace, build or copy `conn.exe`, then explicitly configure the same backend URL:

```powershell
$conn = (Resolve-Path .\conn.exe).Path
kiro-cli mcp add `
  --name conn `
  --scope workspace `
  --command $conn `
  --env "CONN_SERVER_URL=<shared-server-url>" `
  --args mcp

kiro-cli mcp list workspace
kiro-cli chat --v3 --require-mcp-startup
```

Inside each Kiro session:

1. Run `/mcp` and confirm `conn` is connected.
2. Call `session_status`.
3. If unauthenticated, call `login` and complete GitHub Device Flow directly.
4. Call `session_status` again and record the sanitized Human UUID and GitHub login.

Expected: all three Human UUIDs and GitHub logins differ. No Device Flow code or token appears in the trial record.

## 3. Bootstrap the human-approved plan

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

## 4. Start three participant work cells

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

## 5. Synchronize by cursor

After all Intents are current, run `/conn-sync` in every session using its last cursor.

Expected:

- continuation pages are followed until current;
- only records affected by returned event types are reread;
- all sessions converge on the same final cursor;
- no timer, background poll, or unsolicited message starts.

Record the converged cursor as `SYNC_CURSOR`.

## 6. Run local analysis from Kiro

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

## 7. Review and propose a resolution

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

## 8. Finish and attach evidence

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

## 9. Generate handoffs

Run `/conn-handoff` in each session, requesting JSON in one session and Markdown in another.

Expected handoffs contain:

- Project and continuation cursor;
- Human and Participant attribution;
- exact Objective, Blueprint, own Charter, and Intent revisions;
- declared and observed claims with provenance;
- related collisions and decision references;
- evidence coverage, relevant updates, unresolved items, and known unknowns.

They must not contain source, diffs, prompts, transcripts, credentials, raw logs, or `TRIAL_SOURCE_SECRET_MARKER`. Handoffs are generated views and create no persisted handoff record.

## 10. Authority and privacy checks

Run these checks deliberately:

1. Add `human_id` or `participant_id` to a tool input. Schema validation must reject it before an HTTP write.
2. Try to read another person's Charter through `read_my_charter` or focused context. Only the authenticated Human's Charter may appear.
3. Inspect the tool list. No ordinary MCP tool may accept a Charter decision, Objective or Blueprint approval, membership mutation, accepted risk, or binding ResolutionDecision.
4. Confirm a Kiro permission prompt or auto-approval never appears as a Conn domain decision.
5. Search Radar, conflict context, handoffs, evidence, and server diagnostics for the source marker, Device Flow codes, session-token prefix, prompts, and transcripts. None may be present.
6. Confirm all ordinary participant operations in sections 4-9 completed without launching `conn app`.

## 11. Record observations

Complete this table with sanitized notes. Do not mark the trial complete from expected results alone.

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

## Automated baseline

The repository suite covers contracts and invariants but not the three-human claim:

```powershell
pnpm exec tsc --noEmit --pretty false -p fixtures/semantic-intent-radar-two-intents/apps/gateway
pnpm exec tsc --noEmit --pretty false -p fixtures/semantic-intent-radar-two-intents/apps/orders
go test ./internal/mcp -run TestParticipantWorkLoopEndToEnd -count=1
go test ./cmd/conn -run TestMCPStdio -count=1
```

`TestParticipantWorkLoopEndToEnd` exercises the full MCP work loop with a fake backend and real analyzer subprocess. `TestMCPStdio` uses isolated PostgreSQL and authenticated MCP subprocesses when the development database is available. Neither test claims real GitHub Device Flow or three live Kiro sessions.
