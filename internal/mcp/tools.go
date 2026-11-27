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
	"github.com/Juanqu261/conn/internal/preedit"
	"github.com/Juanqu261/conn/internal/protocol"
)

// Tool inputs select what to read. None of them names a human: authority
// always comes from the Conn session. Unknown properties are rejected. Maps
// for canonical nested documents exist only between SDK schema validation and
// protocol.Decode, which runs before HTTP transmission.

type objectiveInput struct {
	ProjectID   string `json:"project_id,omitempty" jsonschema:"Project ID. Defaults to CONN_PROJECT."`
	ObjectiveID string `json:"objective_id,omitempty" jsonschema:"Objective ID. Optional when the project has one Objective."`
	Revision    int    `json:"revision,omitempty" jsonschema:"Objective revision. Defaults to the latest."`
}

type blueprintInput struct {
	ProjectID   string `json:"project_id,omitempty" jsonschema:"Project ID. Defaults to CONN_PROJECT."`
	BlueprintID string `json:"blueprint_id,omitempty" jsonschema:"Blueprint ID. Optional when the project has one Blueprint, or for read_my_charter when you have charters in only one."`
	Revision    int    `json:"revision,omitempty" jsonschema:"Blueprint revision. Defaults to the active revision, or the latest when none is active."`
}

type updatesInput struct {
	ProjectID string `json:"project_id,omitempty" jsonschema:"Project ID. Defaults to CONN_PROJECT."`
	After     int64  `json:"after" jsonschema:"The as_of_event value from your last Conn read, or 0 for the full history."`
	Limit     int    `json:"limit,omitempty" jsonschema:"Maximum events to return, from 1 to 500. Defaults to 100."`
}

type intentMutationInput struct {
	ProjectID string         `json:"project_id,omitempty" jsonschema:"Project ID. Defaults to CONN_PROJECT."`
	Intent    map[string]any `json:"intent" jsonschema:"Intent content. Conn derives participant and human authority."`
}

type reviseIntentInput struct {
	ProjectID        string         `json:"project_id,omitempty" jsonschema:"Project ID. Defaults to CONN_PROJECT."`
	IntentID         string         `json:"intent_id" jsonschema:"Intent UUID to revise."`
	ExpectedRevision int            `json:"expected_revision" jsonschema:"Current Intent revision required for this append-only revision."`
	Intent           map[string]any `json:"intent" jsonschema:"Replacement Intent content. Its intent_id must match intent_id."`
}

type intentIDInput struct {
	ProjectID string `json:"project_id,omitempty" jsonschema:"Project ID. Defaults to CONN_PROJECT."`
	IntentID  string `json:"intent_id" jsonschema:"Intent UUID."`
}

type collisionsInput struct {
	ProjectID string `json:"project_id,omitempty" jsonschema:"Project ID. Defaults to CONN_PROJECT."`
	IntentID  string `json:"intent_id,omitempty" jsonschema:"Optional Intent UUID. When present, returns only its open collision assessments."`
}

type projectInput struct {
	ProjectID string `json:"project_id,omitempty" jsonschema:"Project ID. Defaults to CONN_PROJECT."`
}

type workContextInput struct {
	ProjectID   string `json:"project_id,omitempty"`
	ObjectiveID string `json:"objective_id,omitempty"`
	IntentID    string `json:"intent_id,omitempty"`
	After       *int64 `json:"after,omitempty"`
	Limit       int    `json:"limit,omitempty"`
}

type collisionIDInput struct {
	ProjectID   string `json:"project_id,omitempty" jsonschema:"Project ID. Defaults to CONN_PROJECT."`
	CollisionID string `json:"collision_id" jsonschema:"Collision assessment UUID."`
}

type proposalInput struct {
	ProjectID   string         `json:"project_id,omitempty" jsonschema:"Project ID. Defaults to CONN_PROJECT."`
	CollisionID string         `json:"collision_id" jsonschema:"Collision assessment UUID."`
	Proposal    map[string]any `json:"proposal" jsonschema:"Non-binding resolution proposal content."`
}

type evidenceInput struct {
	ProjectID string         `json:"project_id,omitempty" jsonschema:"Project ID. Defaults to CONN_PROJECT."`
	IntentID  string         `json:"intent_id" jsonschema:"Your active Intent UUID."`
	Evidence  map[string]any `json:"evidence" jsonschema:"Bounded reference-only evidence. Its intent_id must match intent_id."`
}

func registerTools(server *sdk.Server, cfg Config) *interactionStore {
	interactions := newInteractionStore(cfg.Account)
	selection := newProjectSelection()
	t := tools{cfg: cfg, interactions: interactions, selection: selection}
	closed := false
	reads := func(title string) *sdk.ToolAnnotations {
		return &sdk.ToolAnnotations{Title: title, ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: &closed}
	}
	writes := func(title string, idempotent bool) *sdk.ToolAnnotations {
		return &sdk.ToolAnnotations{Title: title, ReadOnlyHint: false, DestructiveHint: &closed, IdempotentHint: idempotent, OpenWorldHint: &closed}
	}

	sdk.AddTool(server, &sdk.Tool{
		Name:        "read_objective",
		Description: "Read one immutable Objective revision: the narrative overview and its keyed points. Returns {data: objective, as_of_event}.",
		InputSchema: objectiveToolSchema(),
		Annotations: reads("Read objective"),
	}, t.readObjective)
	sdk.AddTool(server, &sdk.Tool{
		Name: "read_blueprint",
		Description: "Read one BlueprintRevision with every cell charter and its status. Only an active revision is approved. " +
			"Returns {data: {blueprint_revision, status}, as_of_event}.",
		InputSchema: blueprintToolSchema(),
		Annotations: reads("Read blueprint"),
	}, t.readBlueprint)
	sdk.AddTool(server, &sdk.Tool{
		Name: "read_my_charter",
		Description: "Read the charters you are accountable for in a blueprint revision in force, with each decision. " +
			"You are the human signed in to Conn; this tool cannot read as anyone else. " +
			"Returns {data: {human_id, blueprint_id, revision, status, objective_id, objective_revision, charters}, as_of_event}.",
		InputSchema: blueprintToolSchema(),
		Annotations: reads("Read my charter"),
	}, t.readMyCharter)
	sdk.AddTool(server, &sdk.Tool{
		Name: "check_updates",
		Description: "List committed project events after a cursor, oldest first. Pass the as_of_event from your last Conn read. " +
			"Returns {data: [events], as_of_event}.",
		InputSchema: updatesToolSchema(),
		Annotations: reads("Check updates"),
	}, t.checkUpdates)
	sdk.AddTool(server, &sdk.Tool{
		Name:        "publish_intent",
		Description: "Publish an Intent for your authenticated Conn participant. Omit intent.intent_id to let Conn assign a new UUIDv7; reuse a returned intent_id to retry idempotently. Conn fills missing repository_id, branch_ref, and base_revision_ref from the trusted worktree and reports filled_fields. The input cannot choose a human or participant.",
		InputSchema: publishIntentToolSchema(),
		Annotations: writes("Publish Intent", false),
	}, t.publishIntent)
	sdk.AddTool(server, &sdk.Tool{
		Name:        "revise_intent",
		Description: "Append an immutable revision to your own active Intent using expected_revision. intent.intent_id defaults to intent_id. Missing Git context is filled from the trusted worktree.",
		InputSchema: reviseIntentToolSchema(),
		Annotations: writes("Revise Intent", true),
	}, t.reviseIntent)
	sdk.AddTool(server, &sdk.Tool{
		Name:        "release_intent",
		Description: "Release your own active Intent without deleting its history.",
		InputSchema: intentIDToolSchema("Owned active Intent UUID to release."),
		Annotations: writes("Release Intent", true),
	}, t.releaseIntent)
	sdk.AddTool(server, &sdk.Tool{
		Name:        "analyze_intent",
		Description: "Run bounded local TypeScript analysis of the diff between your exact active Intent revision's base and the working tree, attach only validated source-free observed claims (changes, removes, produces, consumes), reassess collisions, and return the post-commit event cursor. Each Intent revision accepts one analysis.",
		InputSchema: analyzeIntentToolSchema(),
		Annotations: writes("Analyze Intent locally", true),
	}, t.analyzeIntent)
	sdk.AddTool(server, &sdk.Tool{
		Name:        "check_collisions",
		Description: "Read advisory collision assessments grouped into needs_attention (open, under review) and decided (resolved, dismissed, with decision_ref), optionally limited to one Intent. Returns {data: {needs_attention, decided}, as_of_event}.",
		InputSchema: collisionsToolSchema(),
		Annotations: reads("Check collisions"),
	}, t.checkCollisions)
	sdk.AddTool(server, &sdk.Tool{
		Name:        "read_radar",
		Description: "Read the member-authorized Radar coordination snapshot. Returns {data: radar, as_of_event}.",
		InputSchema: projectToolSchema(),
		Annotations: reads("Read Radar"),
	}, t.readRadar)
	sdk.AddTool(server, &sdk.Tool{
		Name:        "read_work_context",
		Description: "Read one bounded participant-focused context with exact revisions, own Charters, owned active Intents, related collisions, evidence coverage, updates, and one authoritative event cursor.",
		InputSchema: workContextToolSchema(),
		Annotations: reads("Read work context"),
	}, t.readWorkContext)
	sdk.AddTool(server, &sdk.Tool{
		Name:        "read_conflict_context",
		Description: "Read a bounded, source-free context pack for one collision. Returns {data: context_pack, as_of_event}.",
		InputSchema: collisionIDToolSchema(),
		Annotations: reads("Read conflict context"),
	}, t.readConflictContext)
	sdk.AddTool(server, &sdk.Tool{
		Name:        "propose_resolution",
		Description: "Record a non-binding participant resolution proposal. This cannot make a decision or change project boundaries.",
		InputSchema: proposalToolSchema(),
		Annotations: writes("Propose resolution", false),
	}, t.proposeResolution)
	sdk.AddTool(server, &sdk.Tool{
		Name:        "attach_evidence",
		Description: "Attach bounded reference-only reported evidence to an active Intent of the signed-in Human. evidence.intent_id defaults to intent_id. Conn never accepts raw logs, source, prompts, authority fields, or observation records; observed check runs come only from `conn evidence run` in the shell.",
		InputSchema: evidenceToolSchema(),
		Annotations: writes("Attach evidence", false),
	}, t.attachEvidence)
	registerAccountTools(server, t)
	registerProjectTools(server, t)
	registerPreEditTools(server, t)
	registerReviewTools(server, t)
	registerCITools(server, t)
	return interactions
}

type tools struct {
	cfg          Config
	interactions *interactionStore
	selection    *projectSelection
}

func (t tools) readObjective(ctx context.Context, _ *sdk.CallToolRequest, in objectiveInput) (*sdk.CallToolResult, any, error) {
	client, projectID, err := t.session(ctx, in.ProjectID)
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
	client, projectID, err := t.session(ctx, in.ProjectID)
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
	client, projectID, err := t.session(ctx, in.ProjectID)
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
	blueprint, inForce, err := pickMyInForce(status.Data, in, me.HumanID)
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
	client, projectID, err := t.session(ctx, in.ProjectID)
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

func (t tools) publishIntent(ctx context.Context, _ *sdk.CallToolRequest, in intentMutationInput) (*sdk.CallToolResult, any, error) {
	client, projectID, err := t.session(ctx, in.ProjectID)
	if err != nil {
		return t.fail("publish_intent", err)
	}
	// Agents rarely produce valid UUIDv7 values, so the adapter assigns the
	// identity when it is omitted. The reply returns it for later revisions.
	in.Intent = withDefault(in.Intent, "intent_id", uuid.NewV7().String())
	input, err := decodeToolDocument[protocol.IntentInput](in.Intent)
	if err != nil {
		return t.fail("publish_intent", err)
	}
	git := t.fillGitContext(ctx, client, projectID, &input)
	result, err := client.PublishIntent(ctx, projectID, input)
	if err != nil {
		return t.fail("publish_intent", err)
	}
	t.rememberFocus(ctx, client, projectID, result.Data.IntentRevision, git.Repository, nil, result.AsOfEvent, false)
	return intentReply(fmt.Sprintf("Published Intent %s revision %d.", result.Data.IntentRevision.IntentID, result.Data.IntentRevision.Revision), result.Data, result.AsOfEvent, git)
}

func (t tools) reviseIntent(ctx context.Context, _ *sdk.CallToolRequest, in reviseIntentInput) (*sdk.CallToolResult, any, error) {
	if in.ExpectedRevision < 1 {
		return t.fail("revise_intent", errors.New("expected_revision must be positive"))
	}
	client, projectID, err := t.session(ctx, in.ProjectID)
	if err != nil {
		return t.fail("revise_intent", err)
	}
	intentID, err := parseID("intent_id", in.IntentID)
	if err != nil {
		return t.fail("revise_intent", err)
	}
	input, err := decodeToolDocument[protocol.IntentInput](withDefault(in.Intent, "intent_id", intentID.String()))
	if err != nil {
		return t.fail("revise_intent", err)
	}
	if input.IntentID != intentID {
		return t.fail("revise_intent", errors.New("intent.intent_id must match intent_id"))
	}
	git := t.fillGitContext(ctx, client, projectID, &input)
	result, err := client.ReviseIntent(ctx, projectID, intentID, in.ExpectedRevision, input)
	if err != nil {
		return t.fail("revise_intent", err)
	}
	t.rememberFocus(ctx, client, projectID, result.Data.IntentRevision, git.Repository, nil, result.AsOfEvent, false)
	return intentReply(fmt.Sprintf("Revised Intent %s to revision %d.", intentID, result.Data.IntentRevision.Revision), result.Data, result.AsOfEvent, git)
}

// intentReply adds the filled Git context and near-match advice to an
// Intent write result.
func intentReply(summary string, view protocol.IntentView, asOf int64, git preedit.GitContext) (*sdk.CallToolResult, any, error) {
	data, err := protocol.Encode(view)
	if err != nil {
		return nil, nil, err
	}
	for _, field := range git.Filled {
		summary += fmt.Sprintf(" Filled %s=%s from the worktree.", field.Field, field.Value)
	}
	for _, note := range git.Notes {
		summary += " " + note
	}
	for _, observation := range view.AdvisoryObservations {
		if strings.HasPrefix(observation, "Near match:") {
			summary += " " + observation
		}
	}
	return envelopeWith(summary, data, asOf, map[string]any{"filled_fields": git.Filled, "git_context_notes": git.Notes})
}

func (t tools) releaseIntent(ctx context.Context, _ *sdk.CallToolRequest, in intentIDInput) (*sdk.CallToolResult, any, error) {
	client, projectID, err := t.session(ctx, in.ProjectID)
	if err != nil {
		return t.fail("release_intent", err)
	}
	intentID, err := parseID("intent_id", in.IntentID)
	if err != nil {
		return t.fail("release_intent", err)
	}
	result, err := client.ReleaseIntent(ctx, projectID, intentID)
	if err != nil {
		return t.fail("release_intent", err)
	}
	t.forgetFocus(ctx, intentID)
	return reply(fmt.Sprintf("Released Intent %s at revision %d.", intentID, result.Data.IntentRevision.Revision), result.Data, result.AsOfEvent)
}

// collisionGroups is the check_collisions result: collisions that need
// attention and decided collisions with their decision references.
type collisionGroups struct {
	NeedsAttention []protocol.CollisionAssessment `json:"needs_attention"`
	Decided        []protocol.CollisionAssessment `json:"decided"`
}

func (t tools) checkCollisions(ctx context.Context, _ *sdk.CallToolRequest, in collisionsInput) (*sdk.CallToolResult, any, error) {
	client, projectID, err := t.session(ctx, in.ProjectID)
	if err != nil {
		return t.fail("check_collisions", err)
	}
	var intentID uuid.UUID
	if in.IntentID != "" {
		if intentID, err = parseID("intent_id", in.IntentID); err != nil {
			return t.fail("check_collisions", err)
		}
	}
	radar, err := client.Radar(ctx, projectID)
	if err != nil {
		return t.fail("check_collisions", err)
	}
	involves := func(collision protocol.CollisionAssessment) bool {
		return intentID == (uuid.UUID{}) || collision.LeftIntentID == intentID || collision.RightIntentID == intentID
	}
	groups := collisionGroups{NeedsAttention: []protocol.CollisionAssessment{}, Decided: []protocol.CollisionAssessment{}}
	for _, collision := range radar.Data.OpenCollisions {
		if involves(collision) {
			groups.NeedsAttention = append(groups.NeedsAttention, collision)
		}
	}
	for _, collision := range radar.Data.DecidedCollisions {
		if involves(collision) {
			groups.Decided = append(groups.Decided, collision)
		}
	}
	data, err := json.Marshal(groups)
	if err != nil {
		return t.fail("check_collisions", err)
	}
	summary := fmt.Sprintf("%d advisory collision(s) need attention; %d decided.", len(groups.NeedsAttention), len(groups.Decided))
	if intentID != (uuid.UUID{}) {
		summary = fmt.Sprintf("Intent %s: %d advisory collision(s) need attention; %d decided.", intentID, len(groups.NeedsAttention), len(groups.Decided))
	}
	if len(groups.NeedsAttention) > 0 {
		summary += " Collisions are advisory warnings, not blocks; review them with /conn-resolve <collision_id>."
	}
	return envelope(summary, data, radar.AsOfEvent)
}

func (t tools) readRadar(ctx context.Context, _ *sdk.CallToolRequest, in projectInput) (*sdk.CallToolResult, any, error) {
	client, projectID, err := t.session(ctx, in.ProjectID)
	if err != nil {
		return t.fail("read_radar", err)
	}
	result, err := client.Radar(ctx, projectID)
	if err != nil {
		return t.fail("read_radar", err)
	}
	return reply(fmt.Sprintf("Radar for %s: %d active Intent(s), %d collision(s) need attention, %d decided.", result.Data.Project.Name, len(result.Data.ActiveIntents), len(result.Data.OpenCollisions), len(result.Data.DecidedCollisions)), result.Data, result.AsOfEvent)
}

func (t tools) readWorkContext(ctx context.Context, _ *sdk.CallToolRequest, in workContextInput) (*sdk.CallToolResult, any, error) {
	client, err := t.client()
	if err != nil {
		return t.fail("read_work_context", err)
	}
	selected, err := t.resolveProject(ctx, client, in.ProjectID)
	if err != nil {
		return t.fail("read_work_context", err)
	}
	var objectiveID, intentID uuid.UUID
	if in.ObjectiveID != "" {
		if objectiveID, err = parseID("objective_id", in.ObjectiveID); err != nil {
			return t.fail("read_work_context", err)
		}
	}
	if in.IntentID != "" {
		if intentID, err = parseID("intent_id", in.IntentID); err != nil {
			return t.fail("read_work_context", err)
		}
	}
	limit := in.Limit
	if limit == 0 {
		limit = 100
	}
	result, err := client.WorkContext(ctx, selected.ProjectID, objectiveID, intentID, in.After, limit)
	if err != nil {
		return t.fail("read_work_context", err)
	}
	view := result.Data
	view.Selection = protocol.WorkContextSelection{Source: protocol.WorkContextSelectionSource(selected.Source)}
	if selected.Source == selectionRepositoryMatch {
		view.Selection.RepositoryMatch = &protocol.WorkContextRepositoryMatch{
			RepositoryID: selected.RepositoryID, CanonicalRemote: selected.RepositoryMatch,
		}
	}
	summary := fmt.Sprintf("Work context for Project %s at event %d.", view.Project.Name, result.AsOfEvent)
	if view.Focus != nil {
		summary += fmt.Sprintf(" Objective %s revision %d is selected.", view.Focus.Objective.ObjectiveID, view.Focus.Objective.Revision)
	} else if len(view.FocusChoices) > 0 {
		summary += fmt.Sprintf(" Select one of %d focus choices before relying on governing context.", len(view.FocusChoices))
	}
	// An exact focus on an owned active Intent tells the optional hooks which
	// Intent belongs to this worktree.
	if intentID != (uuid.UUID{}) && view.Focus != nil && view.Focus.SelectedIntent != nil {
		for _, owned := range view.OwnedActiveIntents {
			if owned.IntentID == intentID {
				t.rememberFocus(ctx, client, selected.ProjectID, owned, nil, view.RelatedCollisions, result.AsOfEvent, true)
			}
		}
	}
	return reply(summary, view, result.AsOfEvent)
}

func (t tools) readConflictContext(ctx context.Context, _ *sdk.CallToolRequest, in collisionIDInput) (*sdk.CallToolResult, any, error) {
	client, projectID, err := t.session(ctx, in.ProjectID)
	if err != nil {
		return t.fail("read_conflict_context", err)
	}
	collisionID, err := parseID("collision_id", in.CollisionID)
	if err != nil {
		return t.fail("read_conflict_context", err)
	}
	result, err := client.ConflictContext(ctx, projectID, collisionID)
	if err != nil {
		return t.fail("read_conflict_context", err)
	}
	return reply(fmt.Sprintf("Conflict context for collision %s. It permits non-binding proposals only.", collisionID), result.Data, result.AsOfEvent)
}

func (t tools) proposeResolution(ctx context.Context, _ *sdk.CallToolRequest, in proposalInput) (*sdk.CallToolResult, any, error) {
	client, projectID, err := t.session(ctx, in.ProjectID)
	if err != nil {
		return t.fail("propose_resolution", err)
	}
	collisionID, err := parseID("collision_id", in.CollisionID)
	if err != nil {
		return t.fail("propose_resolution", err)
	}
	proposal, err := decodeToolDocument[protocol.ResolutionProposalInput](in.Proposal)
	if err != nil {
		return t.fail("propose_resolution", err)
	}
	result, err := client.ProposeResolution(ctx, projectID, collisionID, proposal)
	if err != nil {
		return t.fail("propose_resolution", err)
	}
	return reply(fmt.Sprintf("Recorded non-binding %s proposal %s.", result.Data.Kind, result.Data.ProposalID), result.Data, result.AsOfEvent)
}

func (t tools) attachEvidence(ctx context.Context, _ *sdk.CallToolRequest, in evidenceInput) (*sdk.CallToolResult, any, error) {
	client, projectID, err := t.session(ctx, in.ProjectID)
	if err != nil {
		return t.fail("attach_evidence", err)
	}
	intentID, err := parseID("intent_id", in.IntentID)
	if err != nil {
		return t.fail("attach_evidence", err)
	}
	evidence, err := decodeToolDocument[protocol.EvidenceAttachmentInput](withDefault(in.Evidence, "intent_id", intentID.String()))
	if err != nil {
		return t.fail("attach_evidence", err)
	}
	if evidence.Observation != nil || evidence.Kind == protocol.EvidenceKindCheckRun {
		return t.fail("attach_evidence", errors.New("observed check runs come only from `conn evidence run`; attach_evidence records reported evidence"))
	}
	if evidence.IntentID != intentID {
		return t.fail("attach_evidence", errors.New("evidence.intent_id must match intent_id"))
	}
	result, err := client.AttachEvidence(ctx, projectID, intentID, evidence)
	if err != nil {
		return t.fail("attach_evidence", err)
	}
	return reply(fmt.Sprintf("Attached %s evidence %s to Intent %s.", result.Data.State, result.Data.EvidenceID, intentID), result.Data, result.AsOfEvent)
}

// session resolves the backend client and deterministic process-local Project
// context. Backend authorization remains authoritative for the operation.
func (t tools) session(ctx context.Context, projectInput string) (*httpclient.Client, uuid.UUID, error) {
	client, err := t.client()
	if err != nil {
		return nil, uuid.UUID{}, err
	}
	selected, err := t.resolveProject(ctx, client, projectInput)
	if err != nil {
		return nil, uuid.UUID{}, err
	}
	return client, selected.ProjectID, nil
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
	return nil, nil, t.toolError(tool, err)
}

func (t tools) toolError(tool string, err error) error {
	message := describe(err)
	if t.cfg.Log != nil {
		t.cfg.Log.Warn("tool failed", "tool", tool, "error", err)
	}
	return errors.New(message)
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

// withDefault returns a copy of document with key set to value when absent.
// It never overrides a supplied value, so mismatches are still reported.
func withDefault(document map[string]any, key string, value any) map[string]any {
	copied := make(map[string]any, len(document)+1)
	for k, v := range document {
		copied[k] = v
	}
	if _, ok := copied[key]; !ok {
		copied[key] = value
	}
	return copied
}

func decodeToolDocument[T protocol.Document](value map[string]any) (T, error) {
	var zero T
	data, err := json.Marshal(value)
	if err != nil {
		return zero, err
	}
	return protocol.Decode[T](data)
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
