package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"uuid"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Juanqu261/conn/internal/credentials"
	"github.com/Juanqu261/conn/internal/httpclient"
	"github.com/Juanqu261/conn/internal/protocol"
)

// Tool inputs select what to read. None of them names a human: authority
// always comes from the Conn session. Unknown properties are rejected.

type objectiveInput struct {
	ProjectID   string `json:"project_id,omitempty" jsonschema:"Project ID. Defaults to CONN_PROJECT."`
	ObjectiveID string `json:"objective_id,omitempty" jsonschema:"Objective ID. Optional when the project has one Objective."`
	Revision    int    `json:"revision,omitempty" jsonschema:"Objective revision. Defaults to the latest."`
}

type blueprintInput struct {
	ProjectID   string `json:"project_id,omitempty" jsonschema:"Project ID. Defaults to CONN_PROJECT."`
	BlueprintID string `json:"blueprint_id,omitempty" jsonschema:"Blueprint ID. Optional when the project has one Blueprint."`
	Revision    int    `json:"revision,omitempty" jsonschema:"Blueprint revision. Defaults to the active revision, or the latest when none is active."`
}

type updatesInput struct {
	ProjectID string `json:"project_id,omitempty" jsonschema:"Project ID. Defaults to CONN_PROJECT."`
	After     int64  `json:"after" jsonschema:"The as_of_event value from your last Conn read, or 0 for the full history."`
	Limit     int    `json:"limit,omitempty" jsonschema:"Maximum events to return, from 1 to 500. Defaults to 100."`
}

func registerTools(server *sdk.Server, cfg Config) {
	t := tools{cfg: cfg}
	closed := false
	annotations := func(title string) *sdk.ToolAnnotations {
		return &sdk.ToolAnnotations{Title: title, ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: &closed}
	}

	sdk.AddTool(server, &sdk.Tool{
		Name:        "read_objective",
		Description: "Read one immutable Objective revision: the narrative overview and its keyed points. Returns {data: objective, as_of_event}.",
		Annotations: annotations("Read objective"),
	}, t.readObjective)
	sdk.AddTool(server, &sdk.Tool{
		Name: "read_blueprint",
		Description: "Read one BlueprintRevision with every cell charter and its status. Only an active revision is approved. " +
			"Returns {data: {blueprint_revision, status}, as_of_event}.",
		Annotations: annotations("Read blueprint"),
	}, t.readBlueprint)
	sdk.AddTool(server, &sdk.Tool{
		Name: "read_my_charter",
		Description: "Read the charters you are accountable for in a blueprint revision in force, with each decision. " +
			"You are the human signed in to Conn; this tool cannot read as anyone else. " +
			"Returns {data: {human_id, blueprint_id, revision, status, objective_id, objective_revision, charters}, as_of_event}.",
		Annotations: annotations("Read my charter"),
	}, t.readMyCharter)
	sdk.AddTool(server, &sdk.Tool{
		Name: "check_updates",
		Description: "List committed project events after a cursor, oldest first. Pass the as_of_event from your last Conn read. " +
			"Returns {data: [events], as_of_event}.",
		Annotations: annotations("Check updates"),
	}, t.checkUpdates)
}

type tools struct {
	cfg Config
}

func (t tools) readObjective(ctx context.Context, _ *sdk.CallToolRequest, in objectiveInput) (*sdk.CallToolResult, any, error) {
	client, projectID, err := t.session(in.ProjectID)
	if err != nil {
		return t.fail("read_objective", err)
	}
	objectiveID, revision := uuid.UUID{}, in.Revision
	if in.ObjectiveID != "" {
		if objectiveID, err = parseID("objective_id", in.ObjectiveID); err != nil {
			return t.fail("read_objective", err)
		}
	}
	if objectiveID == (uuid.UUID{}) || revision == 0 {
		status, err := client.Status(ctx, projectID)
		if err != nil {
			return t.fail("read_objective", err)
		}
		summary, err := pickObjective(status.Data, objectiveID)
		if err != nil {
			return t.fail("read_objective", err)
		}
		objectiveID = summary.ObjectiveID
		if revision == 0 {
			revision = summary.CurrentRevision
		}
	}
	result, err := client.ObjectiveRevision(ctx, projectID, objectiveID, revision)
	if err != nil {
		return t.fail("read_objective", err)
	}
	o := result.Data
	return reply(fmt.Sprintf("Objective %s revision %d: %s", o.ObjectiveID, o.Revision, o.Title), o, result.AsOfEvent)
}

func (t tools) readBlueprint(ctx context.Context, _ *sdk.CallToolRequest, in blueprintInput) (*sdk.CallToolResult, any, error) {
	client, projectID, err := t.session(in.ProjectID)
	if err != nil {
		return t.fail("read_blueprint", err)
	}
	blueprintID, revision, err := t.selectRevision(ctx, client, projectID, in)
	if err != nil {
		return t.fail("read_blueprint", err)
	}
	result, err := client.BlueprintRevision(ctx, projectID, blueprintID, revision)
	if err != nil {
		return t.fail("read_blueprint", err)
	}
	v := result.Data
	summary := fmt.Sprintf("Blueprint %s revision %d %s It has %d cell charters.",
		v.BlueprintRevision.BlueprintID, v.BlueprintRevision.Revision, approval(v.Status), len(v.BlueprintRevision.CellCharters))
	return reply(summary, v, result.AsOfEvent)
}

func (t tools) readMyCharter(ctx context.Context, _ *sdk.CallToolRequest, in blueprintInput) (*sdk.CallToolResult, any, error) {
	client, projectID, err := t.session(in.ProjectID)
	if err != nil {
		return t.fail("read_my_charter", err)
	}
	// The human is whoever the session belongs to.
	me, err := client.Me(ctx)
	if err != nil {
		return t.fail("read_my_charter", err)
	}
	status, err := client.Status(ctx, projectID)
	if err != nil {
		return t.fail("read_my_charter", err)
	}
	blueprint, inForce, err := pickInForce(status.Data, in)
	if err != nil {
		return t.fail("read_my_charter", err)
	}
	result, err := client.BlueprintRevision(ctx, projectID, blueprint.BlueprintID, inForce.Revision)
	if err != nil {
		return t.fail("read_my_charter", err)
	}

	decisions := make(map[uuid.UUID]protocol.CellDecision, len(inForce.Cells))
	for _, cell := range inForce.Cells {
		decisions[cell.CellID] = cell.Decision
	}
	snapshot := result.Data.BlueprintRevision
	view := protocol.CharterView{
		HumanID:           me.HumanID,
		BlueprintID:       snapshot.BlueprintID,
		Revision:          snapshot.Revision,
		Status:            result.Data.Status,
		ObjectiveID:       snapshot.ObjectiveID,
		ObjectiveRevision: snapshot.ObjectiveRevision,
	}
	for _, charter := range snapshot.CellCharters {
		if charter.AccountableHumanID == me.HumanID {
			view.Charters = append(view.Charters, protocol.OwnCharter{Charter: charter, Decision: decisions[charter.CellID]})
		}
	}
	if len(view.Charters) == 0 {
		return t.fail("read_my_charter", fmt.Errorf("you (@%s) have no charter in blueprint %s revision %d. "+
			"Use read_blueprint to see who is accountable for each cell", me.GitHubLogin, snapshot.BlueprintID, snapshot.Revision))
	}

	summary := fmt.Sprintf("You (@%s) are accountable for %d charter(s) in blueprint %s revision %d, which %s",
		me.GitHubLogin, len(view.Charters), snapshot.BlueprintID, snapshot.Revision, strings.TrimSuffix(approval(view.Status), "."))
	for _, own := range view.Charters {
		summary += fmt.Sprintf("; your decision on cell %s is %s", own.Charter.CellID, own.Decision)
	}
	summary += "."
	if newer := newerRevision(blueprint, snapshot.Revision); newer != nil {
		summary += fmt.Sprintf(" Revision %d is %s and unapproved; read it with revision: %d.", newer.Revision, newer.Status, newer.Revision)
	}
	return reply(summary, view, result.AsOfEvent)
}

func (t tools) checkUpdates(ctx context.Context, _ *sdk.CallToolRequest, in updatesInput) (*sdk.CallToolResult, any, error) {
	limit := in.Limit
	if limit == 0 {
		limit = 100
	}
	if in.After < 0 || limit < 1 || limit > 500 {
		return t.fail("check_updates", errors.New("after must be at least 0 and limit from 1 to 500"))
	}
	client, projectID, err := t.session(in.ProjectID)
	if err != nil {
		return t.fail("check_updates", err)
	}
	result, err := client.Events(ctx, projectID, in.After, limit)
	if err != nil {
		return t.fail("check_updates", err)
	}
	var data []byte
	data = append(data, '[')
	last := in.After
	for i, event := range result.Data {
		encoded, err := protocol.Encode(event)
		if err != nil {
			return t.fail("check_updates", err)
		}
		if i > 0 {
			data = append(data, ',')
		}
		data = append(data, encoded...)
		last = event.EventSequence
	}
	data = append(data, ']')

	summary := fmt.Sprintf("%d event(s) after %d. The project is at event %d.", len(result.Data), in.After, result.AsOfEvent)
	if last < result.AsOfEvent {
		summary += fmt.Sprintf(" More events remain; call check_updates with after: %d.", last)
	} else if len(result.Data) > 0 {
		summary += " Reread the objective, blueprint, or charter if these events changed them."
	}
	return envelope(summary, data, result.AsOfEvent)
}

// session resolves the human's backend client and the selected project.
func (t tools) session(projectInput string) (*httpclient.Client, uuid.UUID, error) {
	raw := projectInput
	if raw == "" {
		raw = t.cfg.DefaultProject
	}
	if raw == "" {
		return nil, uuid.UUID{}, errors.New("no project selected. Pass project_id, or ask the human to set CONN_PROJECT in the Kiro MCP configuration")
	}
	projectID, err := parseID("project_id", raw)
	if err != nil {
		return nil, uuid.UUID{}, err
	}
	client, err := t.cfg.Client()
	return client, projectID, err
}

// selectRevision picks a blueprint and revision for read_blueprint. An
// explicit blueprint and revision are read directly, so any revision,
// including a superseded one, can be inspected.
func (t tools) selectRevision(ctx context.Context, client *httpclient.Client, projectID uuid.UUID, in blueprintInput) (uuid.UUID, int, error) {
	if in.BlueprintID != "" && in.Revision > 0 {
		id, err := parseID("blueprint_id", in.BlueprintID)
		return id, in.Revision, err
	}
	status, err := client.Status(ctx, projectID)
	if err != nil {
		return uuid.UUID{}, 0, err
	}
	if in.Revision > 0 {
		blueprint, err := pickBlueprint(status.Data, "")
		return blueprint.BlueprintID, in.Revision, err
	}
	blueprint, revision, err := pickInForce(status.Data, in)
	return blueprint.BlueprintID, revision.Revision, err
}

// fail reports err as a tool error the agent can act on. Internal details
// stay in the log on standard error.
func (t tools) fail(tool string, err error) (*sdk.CallToolResult, any, error) {
	message := describe(err)
	if t.cfg.Log != nil {
		t.cfg.Log.Warn("tool failed", "tool", tool, "error", err)
	}
	return nil, nil, errors.New(message)
}

// describe turns an error into a message that tells the agent what the
// human must do. Server messages are safe by contract.
func describe(err error) string {
	var apiErr *httpclient.APIError
	switch {
	case errors.Is(err, credentials.ErrNotLoggedIn):
		return "Not signed in to Conn. Ask the human to run `conn login`, then retry."
	case errors.Is(err, credentials.ErrUnavailable), errors.Is(err, credentials.ErrInvalid):
		return "The Conn credential could not be used (" + err.Error() + "). Ask the human to run `conn doctor`."
	case errors.Is(err, httpclient.ErrUnreachable):
		return "The Conn server could not be reached. Ask the human to run `conn doctor`."
	case errors.As(err, &apiErr) && apiErr.Code == "unauthenticated":
		return "The Conn session has expired or was revoked. Ask the human to run `conn login`, then retry."
	case errors.As(err, &apiErr) && apiErr.Code == "not_found":
		return "Not found, or the signed-in human is not a member of this project."
	}
	return err.Error()
}

func parseID(field, raw string) (uuid.UUID, error) {
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.UUID{}, fmt.Errorf("%s is not a UUID", field)
	}
	return id, nil
}

// reply returns doc in the API envelope as both structured content and
// text, after a one-line summary. Encode validates doc against its schema.
func reply[T protocol.Document](summary string, doc T, asOfEvent int64) (*sdk.CallToolResult, any, error) {
	data, err := protocol.Encode(doc)
	if err != nil {
		return nil, nil, err
	}
	return envelope(summary, data, asOfEvent)
}

func envelope(summary string, data []byte, asOfEvent int64) (*sdk.CallToolResult, any, error) {
	body := json.RawMessage(fmt.Sprintf(`{"data":%s,"as_of_event":%d}`, data, asOfEvent))
	return &sdk.CallToolResult{
		Content: []sdk.Content{&sdk.TextContent{Text: summary}, &sdk.TextContent{Text: string(body)}},
	}, body, nil
}
