package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"uuid"

	"github.com/Juanqu261/conn/internal/httpclient"
	"github.com/Juanqu261/conn/internal/protocol"
	"github.com/Juanqu261/conn/internal/review"
)

const (
	maxHandoffClaims       = 500
	maxHandoffCollisions   = 100
	maxHandoffUpdates      = 100
	maxHandoffUnknowns     = 100
	completionBoundaryNote = "Intent release does not prove Objective, Charter, pull-request, deployment, or Project completion."
)

type handoffSnapshot struct {
	SafeHandlingNote   string                           `json:"safe_handling_note"`
	Project            protocol.WorkContextProject      `json:"project"`
	Reader             handoffReader                    `json:"reader"`
	Objective          handoffRevision                  `json:"objective"`
	Blueprint          *handoffBlueprint                `json:"blueprint,omitempty"`
	OwnCharters        []handoffCharter                 `json:"own_charters"`
	Intent             handoffIntent                    `json:"intent"`
	DeclaredClaims     []handoffClaim                   `json:"declared_claims"`
	ObservedClaims     []handoffObservedClaim           `json:"observed_claims"`
	Collisions         []handoffCollision               `json:"collisions"`
	DecisionRefs       []handoffDecisionRef             `json:"decision_refs"`
	EvidenceCoverage   []protocol.RadarEvidenceCoverage `json:"evidence_coverage"`
	Updates            []handoffEvent                   `json:"updates"`
	ContinuationAfter  *int64                           `json:"continuation_after,omitempty"`
	KnownUnknowns      []string                         `json:"known_unknowns"`
	UnresolvedItems    []string                         `json:"unresolved_items"`
	AsOfEvent          int64                            `json:"as_of_event"`
	CompletionBoundary string                           `json:"completion_boundary"`
}

type handoffReader struct {
	HumanID       uuid.UUID  `json:"human_id"`
	GitHubLogin   string     `json:"github_login"`
	ParticipantID *uuid.UUID `json:"participant_id,omitempty"`
	AdapterKind   string     `json:"adapter_kind,omitempty"`
}

type handoffRevision struct {
	ID       uuid.UUID `json:"id"`
	Revision int       `json:"revision"`
}

type handoffBlueprint struct {
	ID                uuid.UUID                `json:"id"`
	Revision          int                      `json:"revision"`
	Status            protocol.BlueprintStatus `json:"status"`
	ObjectiveRevision int                      `json:"objective_revision"`
}

type handoffCharter struct {
	CellID   uuid.UUID             `json:"cell_id"`
	Decision protocol.CellDecision `json:"decision"`
}

type handoffIntent struct {
	IntentID          uuid.UUID  `json:"intent_id"`
	Revision          int        `json:"revision"`
	ParticipantID     uuid.UUID  `json:"participant_id"`
	Active            bool       `json:"active"`
	ObjectiveID       uuid.UUID  `json:"objective_id"`
	ObjectiveRevision int        `json:"objective_revision"`
	BlueprintID       *uuid.UUID `json:"blueprint_id,omitempty"`
	BlueprintRevision *int       `json:"blueprint_revision,omitempty"`
	CharterID         *uuid.UUID `json:"charter_id,omitempty"`
	RepositoryID      *uuid.UUID `json:"repository_id,omitempty"`
	BaseRevisionRef   string     `json:"base_revision_ref,omitempty"`
	HeadRevisionRef   string     `json:"head_revision_ref,omitempty"`
	BranchRef         string     `json:"branch_ref,omitempty"`
	Assumptions       []string   `json:"assumptions"`
}

type handoffClaim struct {
	SubjectKind       string   `json:"subject_kind"`
	NormalizedSubject string   `json:"normalized_subject"`
	DisplayName       string   `json:"display_name"`
	Relation          string   `json:"relation"`
	ProvenanceKind    string   `json:"provenance_kind"`
	Confidence        *float64 `json:"confidence,omitempty"`
}

type handoffObservedClaim struct {
	ClaimID               uuid.UUID  `json:"claim_id"`
	IntentRevision        int        `json:"intent_revision"`
	SubjectKind           string     `json:"subject_kind"`
	NormalizedSubject     string     `json:"normalized_subject"`
	DisplayName           string     `json:"display_name"`
	Relation              string     `json:"relation"`
	ProvenanceKind        string     `json:"provenance_kind"`
	Confidence            *float64   `json:"confidence,omitempty"`
	ProducerParticipantID *uuid.UUID `json:"producer_participant_id,omitempty"`
}

type handoffCollision struct {
	CollisionID         uuid.UUID   `json:"collision_id"`
	LeftIntentID        uuid.UUID   `json:"left_intent_id"`
	LeftIntentRevision  int         `json:"left_intent_revision"`
	RightIntentID       uuid.UUID   `json:"right_intent_id"`
	RightIntentRevision int         `json:"right_intent_revision"`
	RuleID              string      `json:"rule_id"`
	Severity            string      `json:"severity"`
	Confidence          float64     `json:"confidence"`
	State               string      `json:"state"`
	MatchingClaimRefs   []uuid.UUID `json:"matching_claim_refs"`
	DecisionRefs        []uuid.UUID `json:"decision_refs"`
	UnresolvedQuestion  string      `json:"unresolved_question,omitempty"`
	Uncertainties       []string    `json:"uncertainties"`
}

type handoffDecisionRef struct {
	DecisionID  uuid.UUID `json:"decision_id"`
	CollisionID uuid.UUID `json:"collision_id"`
}

type handoffEvent struct {
	EventSequence int64               `json:"event_sequence"`
	EventID       uuid.UUID           `json:"event_id"`
	EventType     protocol.EventType  `json:"event_type"`
	EntityType    protocol.EntityType `json:"entity_type"`
	EntityID      uuid.UUID           `json:"entity_id"`
	ActorHumanID  uuid.UUID           `json:"actor_human_id"`
	OccurredAt    protocol.Timestamp  `json:"occurred_at"`
}

// loadHandoff reads the handoff records through the shared review loader,
// which confirms one event cursor across every read and retries once.
func loadHandoff(ctx context.Context, client *httpclient.Client, projectID, intentID uuid.UUID, after int64) (handoffSnapshot, error) {
	records, err := review.Load(ctx, client, projectID, intentID, review.LoadOptions{After: &after, UpdateLimit: maxHandoffUpdates, ConflictContexts: true})
	if err != nil {
		return handoffSnapshot{}, err
	}
	return buildHandoff(records.Work, records.Radar, records.Contexts), nil
}

func buildHandoff(work protocol.WorkContext, radar protocol.RadarView, contexts map[uuid.UUID]protocol.ConflictContextPack) handoffSnapshot {
	focus := work.Focus
	intent := *focus.SelectedIntent
	snapshot := handoffSnapshot{
		SafeHandlingNote: safeHandlingNote, Project: work.Project,
		Reader:      handoffReader{HumanID: work.Reader.Human.HumanID, GitHubLogin: work.Reader.Human.GitHubLogin},
		Objective:   handoffRevision{ID: focus.Objective.ObjectiveID, Revision: focus.Objective.Revision},
		OwnCharters: []handoffCharter{}, DeclaredClaims: []handoffClaim{}, ObservedClaims: []handoffObservedClaim{},
		Collisions: []handoffCollision{}, DecisionRefs: []handoffDecisionRef{}, EvidenceCoverage: work.AcceptanceEvidenceCoverage,
		Updates: []handoffEvent{}, KnownUnknowns: boundedStrings(work.KnownUnknowns, maxHandoffUnknowns),
		UnresolvedItems: []string{}, AsOfEvent: work.AsOfEvent, ContinuationAfter: work.ContinuationAfter,
		CompletionBoundary: completionBoundaryNote,
	}
	if work.Reader.Participant != nil {
		snapshot.Reader.ParticipantID = &work.Reader.Participant.ParticipantID
		snapshot.Reader.AdapterKind = work.Reader.Participant.AdapterKind
	}
	if focus.Blueprint != nil {
		snapshot.Blueprint = &handoffBlueprint{
			ID: focus.Blueprint.BlueprintID, Revision: focus.Blueprint.Revision, Status: focus.Blueprint.Status,
			ObjectiveRevision: focus.Blueprint.ObjectiveRevision,
		}
	}
	for _, own := range focus.OwnCharters {
		snapshot.OwnCharters = append(snapshot.OwnCharters, handoffCharter{CellID: own.Charter.CellID, Decision: own.Decision})
	}
	active := false
	for _, current := range radar.ActiveIntents {
		if current.IntentID == intent.IntentID && current.Revision == intent.Revision {
			active = true
			break
		}
	}
	snapshot.Intent = handoffIntent{
		IntentID: intent.IntentID, Revision: intent.Revision, ParticipantID: intent.ParticipantID, Active: active,
		ObjectiveID: intent.ObjectiveID, ObjectiveRevision: intent.ObjectiveRevision,
		BlueprintID: intent.BlueprintID, BlueprintRevision: intent.BlueprintRevision, CharterID: intent.CharterID,
		RepositoryID: intent.RepositoryID, BaseRevisionRef: intent.BaseRevisionRef, HeadRevisionRef: intent.HeadRevisionRef,
		BranchRef: intent.BranchRef, Assumptions: boundedStrings(intent.Assumptions, 100),
	}
	for _, claim := range intent.DeclaredClaims {
		if len(snapshot.DeclaredClaims) >= maxHandoffClaims {
			break
		}
		snapshot.DeclaredClaims = append(snapshot.DeclaredClaims, handoffClaim{
			SubjectKind: claim.SubjectKind, NormalizedSubject: claim.NormalizedSubject, DisplayName: claim.DisplayName,
			Relation: claim.Relation, ProvenanceKind: claim.ProvenanceKind, Confidence: claim.Confidence,
		})
	}
	for _, claim := range radar.SemanticClaims {
		if claim.IntentID != intent.IntentID || claim.IntentRevision != intent.Revision || claim.ProvenanceKind == "declared" {
			continue
		}
		if len(snapshot.ObservedClaims) >= maxHandoffClaims {
			snapshot.KnownUnknowns = append(snapshot.KnownUnknowns, "Observed claims were truncated at the handoff bound.")
			break
		}
		snapshot.ObservedClaims = append(snapshot.ObservedClaims, handoffObservedClaim{
			ClaimID: claim.ClaimID, IntentRevision: claim.IntentRevision, SubjectKind: claim.SubjectKind,
			NormalizedSubject: claim.NormalizedSubject, DisplayName: claim.DisplayName, Relation: claim.Relation,
			ProvenanceKind: claim.ProvenanceKind, Confidence: claim.Confidence, ProducerParticipantID: claim.ProducerParticipantID,
		})
	}
	for _, collision := range intentCollisions(work.RelatedCollisions, intent.IntentID, intent.Revision) {
		if len(snapshot.Collisions) >= maxHandoffCollisions {
			snapshot.KnownUnknowns = append(snapshot.KnownUnknowns, "Related collisions were truncated at the handoff bound.")
			break
		}
		contextPack := contexts[collision.CollisionID]
		item := handoffCollision{
			CollisionID: collision.CollisionID, LeftIntentID: collision.LeftIntentID, LeftIntentRevision: collision.LeftIntentRevision,
			RightIntentID: collision.RightIntentID, RightIntentRevision: collision.RightIntentRevision,
			RuleID: collision.RuleID, Severity: collision.Severity, Confidence: collision.Confidence, State: collision.State,
			MatchingClaimRefs: collision.MatchingClaimRefs, DecisionRefs: contextPack.ExistingDecisionRefs,
			UnresolvedQuestion: contextPack.UnresolvedQuestion, Uncertainties: boundedStrings(contextPack.Uncertainties, 50),
		}
		snapshot.Collisions = append(snapshot.Collisions, item)
		for _, decisionID := range contextPack.ExistingDecisionRefs {
			snapshot.DecisionRefs = append(snapshot.DecisionRefs, handoffDecisionRef{DecisionID: decisionID, CollisionID: collision.CollisionID})
		}
		if collision.State == "open" || collision.State == "under_review" {
			snapshot.UnresolvedItems = append(snapshot.UnresolvedItems, fmt.Sprintf("Collision %s remains %s.", collision.CollisionID, collision.State))
		}
		if contextPack.UnresolvedQuestion != "" {
			snapshot.UnresolvedItems = append(snapshot.UnresolvedItems, contextPack.UnresolvedQuestion)
		}
	}
	for _, coverage := range snapshot.EvidenceCoverage {
		if coverage.State != "passing" {
			snapshot.UnresolvedItems = append(snapshot.UnresolvedItems, fmt.Sprintf("Acceptance key %s has evidence state %s.", coverage.AcceptanceKey, coverage.State))
		}
	}
	for _, event := range work.Updates {
		if len(snapshot.Updates) >= maxHandoffUpdates {
			break
		}
		snapshot.Updates = append(snapshot.Updates, handoffEvent{
			EventSequence: event.EventSequence, EventID: event.EventID, EventType: event.EventType(),
			EntityType: event.EntityType(), EntityID: event.EntityID, ActorHumanID: event.ActorHumanID, OccurredAt: event.OccurredAt,
		})
	}
	if snapshot.ContinuationAfter != nil {
		snapshot.UnresolvedItems = append(snapshot.UnresolvedItems, fmt.Sprintf("More Project events remain after cursor %d.", *snapshot.ContinuationAfter))
	}
	snapshot.UnresolvedItems = uniqueHandoffStrings(snapshot.UnresolvedItems, maxHandoffUnknowns)
	snapshot.KnownUnknowns = uniqueHandoffStrings(snapshot.KnownUnknowns, maxHandoffUnknowns)
	return snapshot
}

func renderHandoffJSON(snapshot handoffSnapshot) (string, error) {
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func renderHandoffMarkdown(snapshot handoffSnapshot) string {
	var out strings.Builder
	out.WriteString("# Conn Intent Handoff\n\n")
	out.WriteString("**Safe handling:** " + snapshot.SafeHandlingNote + "\n\n")
	fmt.Fprintf(&out, "- Project: `%s`\n- Authoritative cursor: `%d`\n", snapshot.Project.ProjectID, snapshot.AsOfEvent)
	fmt.Fprintf(&out, "- Human: `%s` (@%s)\n", snapshot.Reader.HumanID, safeMarkdownLine(snapshot.Reader.GitHubLogin))
	if snapshot.Reader.ParticipantID != nil {
		fmt.Fprintf(&out, "- Participant: `%s` (%s)\n", *snapshot.Reader.ParticipantID, safeMarkdownLine(snapshot.Reader.AdapterKind))
	}
	fmt.Fprintf(&out, "- Objective: `%s` revision `%d`\n", snapshot.Objective.ID, snapshot.Objective.Revision)
	if snapshot.Blueprint != nil {
		fmt.Fprintf(&out, "- Blueprint: `%s` revision `%d` (%s)\n", snapshot.Blueprint.ID, snapshot.Blueprint.Revision, snapshot.Blueprint.Status)
	}
	for _, charter := range snapshot.OwnCharters {
		fmt.Fprintf(&out, "- Own Charter: `%s` (%s)\n", charter.CellID, charter.Decision)
	}
	fmt.Fprintf(&out, "- Intent: `%s` revision `%d` (active: `%t`)\n", snapshot.Intent.IntentID, snapshot.Intent.Revision, snapshot.Intent.Active)

	out.WriteString("\n## Claims and assumptions\n\n")
	for _, assumption := range snapshot.Intent.Assumptions {
		fmt.Fprintf(&out, "- Assumption, untrusted Project data: %s\n", safeMarkdownLine(assumption))
	}
	for _, claim := range snapshot.DeclaredClaims {
		fmt.Fprintf(&out, "- Declared `%s` `%s` (%s)\n", claim.Relation, safeMarkdownLine(claim.NormalizedSubject), claim.ProvenanceKind)
	}
	for _, claim := range snapshot.ObservedClaims {
		fmt.Fprintf(&out, "- Observed claim `%s`: `%s` `%s` (%s)\n", claim.ClaimID, claim.Relation, safeMarkdownLine(claim.NormalizedSubject), claim.ProvenanceKind)
	}
	if len(snapshot.Intent.Assumptions)+len(snapshot.DeclaredClaims)+len(snapshot.ObservedClaims) == 0 {
		out.WriteString("- None recorded.\n")
	}

	out.WriteString("\n## Collisions and decisions\n\n")
	if len(snapshot.Collisions) == 0 {
		out.WriteString("- No related open collision is included.\n")
	}
	for _, collision := range snapshot.Collisions {
		fmt.Fprintf(&out, "- Collision `%s`: %s, %s, state `%s`\n", collision.CollisionID, collision.RuleID, collision.Severity, collision.State)
		for _, decisionID := range collision.DecisionRefs {
			fmt.Fprintf(&out, "  - Recorded decision reference: `%s`\n", decisionID)
		}
	}

	out.WriteString("\n## Evidence coverage\n\n")
	if len(snapshot.EvidenceCoverage) == 0 {
		out.WriteString("- No keyed acceptance evidence coverage is available.\n")
	}
	for _, coverage := range snapshot.EvidenceCoverage {
		fmt.Fprintf(&out, "- `%s`: `%s` (%d evidence reference(s))\n", safeMarkdownLine(coverage.AcceptanceKey), coverage.State, len(coverage.EvidenceRefs))
	}

	out.WriteString("\n## Relevant updates\n\n")
	if len(snapshot.Updates) == 0 {
		out.WriteString("- No updates are included for the requested cursor range.\n")
	}
	for _, event := range snapshot.Updates {
		fmt.Fprintf(&out, "- Event `%d`: `%s` on %s `%s`\n", event.EventSequence, event.EventType, event.EntityType, event.EntityID)
	}
	if snapshot.ContinuationAfter != nil {
		fmt.Fprintf(&out, "- Continue updates after: `%d`\n", *snapshot.ContinuationAfter)
	}

	out.WriteString("\n## Unresolved items and known unknowns\n\n")
	if len(snapshot.UnresolvedItems)+len(snapshot.KnownUnknowns) == 0 {
		out.WriteString("- None recorded. Absence does not prove completion.\n")
	}
	for _, item := range snapshot.UnresolvedItems {
		fmt.Fprintf(&out, "- Unresolved: %s\n", safeMarkdownLine(item))
	}
	for _, unknown := range snapshot.KnownUnknowns {
		fmt.Fprintf(&out, "- Known unknown, untrusted Project data: %s\n", safeMarkdownLine(unknown))
	}
	out.WriteString("\n" + snapshot.CompletionBoundary + "\n")
	return out.String()
}

func safeMarkdownLine(value string) string {
	value = strings.ReplaceAll(value, "\r", " ")
	value = strings.ReplaceAll(value, "\n", " ")
	return strings.TrimSpace(value)
}

func boundedStrings(values []string, limit int) []string {
	if len(values) > limit {
		values = values[:limit]
	}
	return append([]string(nil), values...)
}

func uniqueHandoffStrings(values []string, limit int) []string {
	seen := make(map[string]bool, len(values))
	result := make([]string, 0, min(len(values), limit))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
		if len(result) == limit {
			break
		}
	}
	return result
}
