package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"uuid"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Juanqu261/conn/internal/hookstate"
	"github.com/Juanqu261/conn/internal/httpclient"
	"github.com/Juanqu261/conn/internal/preedit"
	"github.com/Juanqu261/conn/internal/protocol"
)

type listConceptsInput struct {
	ProjectID   string `json:"project_id,omitempty"`
	Kind        string `json:"kind,omitempty"`
	Query       string `json:"q,omitempty"`
	ObjectiveID string `json:"objective_id,omitempty"`
	Limit       int    `json:"limit,omitempty"`
	After       string `json:"after,omitempty"`
}

type suggestClaimsInput struct {
	ProjectID   string              `json:"project_id,omitempty"`
	ObjectiveID string              `json:"objective_id,omitempty"`
	Paths       []string            `json:"paths"`
	Symbols     map[string][]string `json:"symbols,omitempty"`
	Focus       string              `json:"focus,omitempty"`
	Worktree    string              `json:"worktree,omitempty"`
	Preview     bool                `json:"preview_collisions,omitempty"`
	IntentID    string              `json:"intent_id,omitempty"`
}

func registerPreEditTools(server *sdk.Server, t tools) {
	closed := false
	reads := func(title string) *sdk.ToolAnnotations {
		return &sdk.ToolAnnotations{Title: title, ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: &closed}
	}
	sdk.AddTool(server, &sdk.Tool{
		Name: "list_concepts",
		Description: "List the Project concept catalog: subjects derived from Objective invariants and acceptance criteria, and the normalized subjects of active claims, with the Intents that claim them. " +
			"Use q to rank by deterministic token similarity. Reuse an existing subject instead of inventing a new string for the same concept. Returns {data: concept_list, as_of_event}.",
		InputSchema: listConceptsToolSchema(),
		Annotations: reads("List concepts"),
	}, t.listConcepts)
	sdk.AddTool(server, &sdk.Tool{
		Name: "suggest_claims",
		Description: "Before editing, turn planned repository-relative paths, optional export names, an Objective, and a task focus into declared-claim candidates: changes for planned exports, consumes for repository contracts the files import, Objective invariants, and catalog matches. " +
			"With preview_collisions, also returns the collisions the candidates would open against active Intents, including inherited decisions, still without publishing. " +
			"Nothing is persisted; include the chosen candidates as intent.declared_claims in publish_intent or revise_intent.",
		InputSchema: suggestClaimsToolSchema(),
		Annotations: reads("Suggest claims before editing"),
	}, t.suggestClaims)
}

func (t tools) listConcepts(ctx context.Context, _ *sdk.CallToolRequest, in listConceptsInput) (*sdk.CallToolResult, any, error) {
	client, projectID, err := t.session(ctx, in.ProjectID)
	if err != nil {
		return t.fail("list_concepts", err)
	}
	query := httpclient.ConceptQuery{Kind: in.Kind, Query: in.Query, Limit: in.Limit, After: in.After}
	if in.ObjectiveID != "" {
		if query.ObjectiveID, err = parseID("objective_id", in.ObjectiveID); err != nil {
			return t.fail("list_concepts", err)
		}
	}
	result, err := client.Concepts(ctx, projectID, query)
	if err != nil {
		return t.fail("list_concepts", err)
	}
	summary := fmt.Sprintf("%d concept(s) in the Project catalog.", len(result.Data.Concepts))
	if result.Data.HasMore {
		summary += fmt.Sprintf(" More remain; call list_concepts with after: %q.", result.Data.ContinuationAfter)
	}
	return reply(summary, result.Data, result.AsOfEvent)
}

func (t tools) suggestClaims(ctx context.Context, _ *sdk.CallToolRequest, in suggestClaimsInput) (*sdk.CallToolResult, any, error) {
	client, projectID, err := t.session(ctx, in.ProjectID)
	if err != nil {
		return t.fail("suggest_claims", err)
	}
	var objectiveID uuid.UUID
	if in.ObjectiveID != "" {
		if objectiveID, err = parseID("objective_id", in.ObjectiveID); err != nil {
			return t.fail("suggest_claims", err)
		}
	}
	worktree, _, err := resolveAnalysisWorkspace(t.cfg.WorkspaceRoot, in.Worktree, nil)
	if err != nil {
		return t.fail("suggest_claims", err)
	}
	var asIntent *uuid.UUID
	if in.IntentID != "" {
		id, err := parseID("intent_id", in.IntentID)
		if err != nil {
			return t.fail("suggest_claims", err)
		}
		asIntent = &id
	}
	suggestions, err := preedit.SuggestClaims(ctx, preedit.SuggestDeps{
		Client: client, Analyzer: t.cfg.Analyzer, AnalyzerAvailable: t.analyzerAvailable(), Discover: t.discoverAt(),
	}, preedit.SuggestRequest{
		ProjectID: projectID, ObjectiveID: objectiveID, Worktree: worktree, Paths: in.Paths, Symbols: in.Symbols, Focus: in.Focus,
		PreviewCollisions: in.Preview, AsIntentID: asIntent,
	})
	if err != nil {
		return t.fail("suggest_claims", err)
	}
	data, err := json.Marshal(suggestions)
	if err != nil {
		return t.fail("suggest_claims", err)
	}
	summary := fmt.Sprintf("%d claim candidate(s) for %d planned path(s). Nothing was persisted; publish the chosen candidates as declared claims.", len(suggestions.Candidates), len(in.Paths))
	if suggestions.OmittedCount > 0 {
		summary += fmt.Sprintf(" %d candidate(s) were omitted at the bound.", suggestions.OmittedCount)
	}
	if !suggestions.Analysis.Available {
		summary += " Planned analysis is unavailable: " + suggestions.Analysis.Reason
	}
	if len(suggestions.NewPaths) > 0 {
		summary += fmt.Sprintf(" New planned file(s): %s.", strings.Join(suggestions.NewPaths, ", "))
	}
	if preview := suggestions.Preview; preview != nil {
		summary += fmt.Sprintf(" The candidates would open %d advisory collision(s); nothing was published.", len(preview.Collisions))
	}
	return envelope(summary, data, suggestions.AsOfEvent)
}

func (t tools) analyzerAvailable() bool {
	if t.cfg.AnalyzerLocation != nil {
		return t.cfg.AnalyzerLocation().Available()
	}
	return t.cfg.AnalyzerAvailable != nil && t.cfg.AnalyzerAvailable()
}

func (t tools) discoverAt() preedit.Discover {
	if t.cfg.DiscoverRepositoryAt != nil {
		return t.cfg.DiscoverRepositoryAt
	}
	return nil
}

// fillGitContext fills missing Intent Git fields from the trusted workspace.
// Failure to resolve the workspace leaves the input unchanged with a note.
func (t tools) fillGitContext(ctx context.Context, client *httpclient.Client, projectID uuid.UUID, input *protocol.IntentInput) preedit.GitContext {
	worktree, err := resolvedDirectory(workspaceRoot(t.cfg.WorkspaceRoot))
	if err != nil {
		return preedit.GitContext{Filled: []preedit.FilledField{}, Notes: []string{"Git context was not filled: the trusted workspace could not be resolved."}}
	}
	return preedit.FillGitContext(ctx, client, projectID, worktree, t.discoverAt(), input)
}

// rememberFocus records the Intent that belongs to this worktree for the
// optional hooks. The record is a non-authoritative cache and is written
// only when the worktree remote matches the Intent repository. Failures are
// logged and never fail the tool call.
func (t tools) rememberFocus(ctx context.Context, client *httpclient.Client, projectID uuid.UUID, intent protocol.IntentRevision, repository *protocol.RepositoryConnection, related []protocol.CollisionAssessment, asOf int64, known bool) {
	if intent.RepositoryID == nil {
		return
	}
	worktree, err := resolvedDirectory(workspaceRoot(t.cfg.WorkspaceRoot))
	if err != nil {
		return
	}
	if repository == nil || repository.RepositoryID != *intent.RepositoryID {
		repositories, err := client.RepositoryConnections(ctx, projectID)
		if err != nil {
			return
		}
		repository = nil
		for i := range repositories.Data.Repositories {
			if repositories.Data.Repositories[i].RepositoryID == *intent.RepositoryID {
				repository = &repositories.Data.Repositories[i]
			}
		}
		if repository == nil {
			return
		}
		metadata, err := t.discoverAnalysisRepository(ctx, worktree)
		if err != nil || metadata.CanonicalRemote != repository.CanonicalRemote {
			return
		}
	}
	if !known {
		work, err := client.WorkContext(ctx, projectID, uuid.UUID{}, intent.IntentID, nil, 1)
		if err == nil {
			related, asOf, known = work.Data.RelatedCollisions, max(asOf, work.AsOfEvent), true
		}
	}
	store, err := hookstate.Open(ctx, worktree)
	if err != nil {
		return
	}
	states, pairs := map[string]string{}, map[string]string{}
	for _, collision := range related {
		if collision.LeftIntentID == intent.IntentID || collision.RightIntentID == intent.IntentID {
			states[collision.CollisionID.String()] = collision.State
			counterpart := collision.LeftIntentID
			if counterpart == intent.IntentID {
				counterpart = collision.RightIntentID
			}
			if key := hookstate.PairKey(collision.RuleID, counterpart); collision.State != protocol.CollisionStale || pairs[key] == "" {
				pairs[key] = collision.State
			}
		}
	}
	projectCopy, repositoryCopy, intentCopy, participantCopy := projectID, *intent.RepositoryID, intent.IntentID, intent.ParticipantID
	err = store.UpdateFocus(func(focus *hookstate.Focus) {
		sameIntent := focus.IntentID != nil && *focus.IntentID == intent.IntentID
		focus.ServerURL, focus.ProjectID, focus.RepositoryID = client.URL(), &projectCopy, &repositoryCopy
		focus.IntentID, focus.IntentRevision, focus.ParticipantID = &intentCopy, intent.Revision, &participantCopy
		focus.ParticipantAdapter = t.cfg.adapter.kind()
		if known || !sameIntent {
			focus.KnownCollisions, focus.KnownPairs = states, pairs
		}
		if asOf > focus.LastEvent || !sameIntent {
			focus.LastEvent = asOf
		}
	})
	if err != nil && t.cfg.Log != nil {
		t.cfg.Log.Warn("could not record the local Conn focus", "error", err)
	}
}

// forgetFocus clears the local focus after the focused Intent is released.
func (t tools) forgetFocus(ctx context.Context, intentID uuid.UUID) {
	worktree, err := resolvedDirectory(workspaceRoot(t.cfg.WorkspaceRoot))
	if err != nil {
		return
	}
	store, err := hookstate.Open(ctx, worktree)
	if err != nil {
		return
	}
	if err := store.ClearIntent(intentID); err != nil && t.cfg.Log != nil {
		t.cfg.Log.Warn("could not clear the local Conn focus", "error", err)
	}
}

func workspaceRoot(configured string) string {
	if configured == "" {
		return "."
	}
	return configured
}

// envelopeWith adds adapter-level fields beside data and as_of_event.
func envelopeWith(summary string, data []byte, asOfEvent int64, extra map[string]any) (*sdk.CallToolResult, any, error) {
	body := map[string]any{"data": json.RawMessage(data), "as_of_event": asOfEvent}
	for key, value := range extra {
		body[key] = value
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, nil, err
	}
	raw := json.RawMessage(encoded)
	return &sdk.CallToolResult{
		Content: []sdk.Content{&sdk.TextContent{Text: summary}, &sdk.TextContent{Text: string(raw)}},
	}, raw, nil
}
