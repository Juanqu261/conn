# Real MVP trial

Use this guide to run the intended MVP experiment: three humans operating three independent Kiro sessions around one shared objective. It validates real sign-in, authorization, immutable revisions, charter acceptance, event sync, and the read-only Kiro view.

The trial creates persistent records in the local PostgreSQL volume. Use a unique project name and new UUIDv7 values. Do not delete the Docker volume as routine cleanup.

## Participants and preparation

Assign these roles before starting:

| Person | Role | Required account |
| --- | --- | --- |
| Person A | Project owner and accountable human for cell A | A distinct GitHub account |
| Person B | Project member and accountable human for cell B | A distinct GitHub account |
| Person C | Project member and accountable human for cell C | A distinct GitHub account |

Each person needs access to the same Conn server and an independent terminal in a different operating-system user profile or on a different machine. Conn stores one session per server URL in each operating-system credential store, so three people cannot share one OS profile. To exercise Kiro, each also needs an independent Kiro session that already starts the Conn MCP adapter for the same server and project. This guide does not create or change MCP configuration.

Before the session, prepare a small objective that can be divided into three real work cells. For example, add a health endpoint, its documentation, and its integration test to a small service. The work must be safe to perform in the chosen repository.

## Start the shared local service

Person A performs the local setup from the repository root:

```powershell
pnpm install --frozen-lockfile
Copy-Item .env.example .env
```

Set `CONN_GITHUB_CLIENT_ID` in `.env` to the public client ID of a GitHub OAuth application with Device Flow enabled. Then start the database, apply migrations, and run the backend:

```powershell
pnpm db:start
pnpm db:migrate
go run ./cmd/conn server
```

Keep the server running. In a second terminal, verify it is ready:

```powershell
go run ./cmd/conn doctor
```

Expected result: `Server reachable` and `Server ready` pass. The credential check is expected to report that no session exists until each person signs in, so `conn doctor` exits nonzero at this point.

## Establish the three identities

Each person runs the following against the shared server and completes the displayed GitHub Device Flow:

```powershell
go run ./cmd/conn login
go run ./cmd/conn whoami
```

Record the Conn human ID for each person:

```text
OWNER_HUMAN_ID = Person A's human ID
HUMAN_B_ID     = Person B's human ID
HUMAN_C_ID     = Person C's human ID
```

Expected result: every person has a different Conn human ID. `conn doctor` reports a usable credential-store session without displaying a token.

## Create the project and members

Person A creates `project.json`. Replace the placeholder with a new UUIDv7 and use a unique name:

```json
{
  "project_id": "<PROJECT_ID_UUIDV7>",
  "name": "Three-person Conn trial YYYY-MM-DD"
}
```

Create the project, then add Persons B and C:

```powershell
go run ./cmd/conn project create --file project.json --json
go run ./cmd/conn project member add --project <PROJECT_ID_UUIDV7> --human <HUMAN_B_ID> --json
go run ./cmd/conn project member add --project <PROJECT_ID_UUIDV7> --human <HUMAN_C_ID> --json
```

Expected result: Person A is the owner; B and C are members. Each successful project-scoped response includes `as_of_event`. Keep the highest observed value as `LAST_CURSOR`.

## Create the objective

Person A creates `objective.json`. Replace every placeholder with the recorded IDs and a new UUIDv7:

```json
{
  "objective_id": "<OBJECTIVE_ID_UUIDV7>",
  "title": "Deliver a small three-cell change",
  "overview_markdown": "Deliver the agreed change through three independently accountable work cells.",
  "accountable_human_id": "<OWNER_HUMAN_ID>",
  "key_points": [
    {
      "key": "acceptance",
      "kind": "acceptance_criterion",
      "statement": "The change works and the three cell deliverables fit together."
    }
  ]
}
```

Create it and read it back:

```powershell
go run ./cmd/conn objective create --project <PROJECT_ID_UUIDV7> --file objective.json --json
go run ./cmd/conn objective show --project <PROJECT_ID_UUIDV7> --objective <OBJECTIVE_ID_UUIDV7> --revision 1 --json
```

Expected result: revision `1` has the supplied overview, accountable human, and key point. Keep the returned event cursor.

## Propose three accountable charters

Person A prepares `blueprint.json`. Replace all IDs with new UUIDv7 values or recorded human IDs. The charter overviews should describe the real work chosen for the trial.

```json
{
  "blueprint_id": "<BLUEPRINT_ID_UUIDV7>",
  "objective_id": "<OBJECTIVE_ID_UUIDV7>",
  "objective_revision": 1,
  "cell_charters": [
    {
      "cell_id": "<CELL_A_ID_UUIDV7>",
      "accountable_human_id": "<OWNER_HUMAN_ID>",
      "title": "Implementation",
      "overview_markdown": "Implement the agreed application behavior.",
      "objective_point_refs": ["acceptance"],
      "boundary_points": [
        {
          "key": "deliverable",
          "kind": "deliverable",
          "statement": "A reviewed implementation change."
        }
      ]
    },
    {
      "cell_id": "<CELL_B_ID_UUIDV7>",
      "accountable_human_id": "<HUMAN_B_ID>",
      "title": "Documentation",
      "overview_markdown": "Document the behavior and its local use.",
      "objective_point_refs": ["acceptance"],
      "boundary_points": [
        {
          "key": "deliverable",
          "kind": "deliverable",
          "statement": "Documentation for the agreed behavior."
        }
      ]
    },
    {
      "cell_id": "<CELL_C_ID_UUIDV7>",
      "accountable_human_id": "<HUMAN_C_ID>",
      "title": "Verification",
      "overview_markdown": "Verify the agreed behavior through the selected test path.",
      "objective_point_refs": ["acceptance"],
      "boundary_points": [
        {
          "key": "evidence",
          "kind": "evidence_obligation",
          "statement": "Record the commands and outcome used to verify the change."
        }
      ],
      "dependencies": [
        {
          "cell_id": "<CELL_A_ID_UUIDV7>",
          "summary": "Verification needs the implementation deliverable."
        }
      ]
    }
  ]
}
```

Person A proposes the blueprint and checks the project status:

```powershell
go run ./cmd/conn blueprint propose --project <PROJECT_ID_UUIDV7> --file blueprint.json --json
go run ./cmd/conn status --project <PROJECT_ID_UUIDV7> --json
```

Expected result: blueprint revision `1` is `proposed` and clearly unapproved. It is not active before all three decisions.

## Read the shared plan through Kiro

In each participant's independent Kiro session, ask the configured Conn tools to read the current objective and blueprint. Each person should then call `read_my_charter`.

Check these results:

- `read_objective` and `read_blueprint` identify revision `1` as unapproved while decisions are incomplete.
- `read_my_charter` returns only the caller's own charter. It must not return another person's charter as their own.
- The available Conn tools are read-only. There is no tool that proposes, accepts, rejects, or writes a record.

If a session cannot read its charter, first run `conn doctor` in that person's terminal. Confirm that the MCP session uses the same Conn server and project, then repeat the tool call.

## Accept the charters

Each person accepts only their assigned cell. Do this in three different terminals:

```powershell
# Person A
go run ./cmd/conn charter accept --project <PROJECT_ID_UUIDV7> --blueprint <BLUEPRINT_ID_UUIDV7> --revision 1 --cell <CELL_A_ID_UUIDV7> --json

# Person B
go run ./cmd/conn charter accept --project <PROJECT_ID_UUIDV7> --blueprint <BLUEPRINT_ID_UUIDV7> --revision 1 --cell <CELL_B_ID_UUIDV7> --json

# Person C
go run ./cmd/conn charter accept --project <PROJECT_ID_UUIDV7> --blueprint <BLUEPRINT_ID_UUIDV7> --revision 1 --cell <CELL_C_ID_UUIDV7> --json
```

After the first and second decisions, Person A runs `conn status`. Expected result: revision `1` remains proposed. After the final decision, run it again:

```powershell
go run ./cmd/conn status --project <PROJECT_ID_UUIDV7> --json
go run ./cmd/conn blueprint show --project <PROJECT_ID_UUIDV7> --blueprint <BLUEPRINT_ID_UUIDV7> --revision 1 --json
```

Expected result: revision `1` is active and the blueprint view shows one acceptance by the accountable human for every cell.

## Verify events and authority boundaries

Any participant reads all events from the beginning:

```powershell
go run ./cmd/conn sync --project <PROJECT_ID_UUIDV7> --after 0 --limit 500 --json
```

Expected result: events are ordered oldest first, have increasing cursor values, and include project creation, membership changes, objective creation, blueprint proposal, and charter decisions. Copy the final cursor and call `sync` again with it. Expected result: no events are returned unless another person changed the project in the meantime.

Run these negative checks deliberately. They must fail without changing the active blueprint:

1. Person B tries to accept Cell A. The request must be denied.
2. A person tries to accept their own cell a second time. The command must return conflict exit code `5`.
3. Person B tries to add a member. The request must be denied because only the owner may add members.

## Verify a proposed revision preserves the active plan

Person A changes a charter overview in a copy of `blueprint.json`, keeping the same `blueprint_id` and cell IDs. Propose revision `2` with the revision that was just read:

```powershell
go run ./cmd/conn blueprint revise --project <PROJECT_ID_UUIDV7> --blueprint <BLUEPRINT_ID_UUIDV7> --expected-revision 1 --file blueprint-v2.json --json
go run ./cmd/conn status --project <PROJECT_ID_UUIDV7> --json
```

Expected result: revision `1` remains active and revision `2` is proposed and unapproved. Have Person B reject their revision-2 charter with a clear reason:

```powershell
go run ./cmd/conn charter reject --project <PROJECT_ID_UUIDV7> --blueprint <BLUEPRINT_ID_UUIDV7> --revision 2 --cell <CELL_B_ID_UUIDV7> --reason "The boundary needs clarification." --json
```

Expected result: revision `2` becomes rejected. Revision `1` remains active. This proves that a proposed or rejected change cannot silently replace the accepted plan.

## Record the outcome

For each participant, record:

- The Conn human ID and assigned cell ID.
- The project, objective, and blueprint IDs.
- The final active blueprint revision and final event cursor.
- Whether each CLI and MCP check produced the expected result.
- Any unexpected error, its command, exit code, and sanitized output.

Do not record session tokens, Device Flow codes, private prompts, or repository secrets. Keep the local database volume if the team needs to inspect trial records later.

See [architecture](architecture.md) for the behavior under test and [the local run guide](../README.md#run-conn-locally) for the concise startup reference.
