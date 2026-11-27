package review

import (
	"context"
	"errors"

	"uuid"

	"github.com/Juanqu261/conn/internal/httpclient"
	"github.com/Juanqu261/conn/internal/protocol"
)

// IndependentRecords are packet records loaded without a Human reader.
type IndependentRecords struct {
	Records    Records
	View       protocol.IntentView
	OwnerLogin string
}

// LoadIndependent reads the records for one Intent's packet through the
// routes a CI credential may use: the Intent, its Objective revision, Radar,
// the Intent's evidence, and Project members. It needs no Human reader, so it
// loads no own Charters, decision details, or proposal counts. Every response
// must reflect the same Project event cursor; it retries once, then returns
// ErrStaleContext.
func LoadIndependent(ctx context.Context, client *httpclient.Client, projectID, intentID uuid.UUID) (IndependentRecords, error) {
	for attempt := 0; attempt < 2; attempt++ {
		records, err := loadIndependentOnce(ctx, client, projectID, intentID)
		if errors.Is(err, errCursorMismatch) {
			continue
		}
		return records, err
	}
	return IndependentRecords{}, ErrStaleContext
}

func loadIndependentOnce(ctx context.Context, client *httpclient.Client, projectID, intentID uuid.UUID) (IndependentRecords, error) {
	view, err := client.Intent(ctx, projectID, intentID)
	if err != nil {
		return IndependentRecords{}, err
	}
	asOf := view.AsOfEvent
	same := func(other int64) error {
		if other != asOf {
			return errCursorMismatch
		}
		return nil
	}
	revision := view.Data.IntentRevision
	radar, err := client.Radar(ctx, projectID)
	if err != nil {
		return IndependentRecords{}, err
	}
	if err := same(radar.AsOfEvent); err != nil {
		return IndependentRecords{}, err
	}
	objective, err := client.ObjectiveRevision(ctx, projectID, revision.ObjectiveID, revision.ObjectiveRevision)
	if err != nil {
		return IndependentRecords{}, err
	}
	if err := same(objective.AsOfEvent); err != nil {
		return IndependentRecords{}, err
	}
	evidence, err := client.IntentEvidence(ctx, projectID, intentID)
	if err != nil {
		return IndependentRecords{}, err
	}
	if err := same(evidence.AsOfEvent); err != nil {
		return IndependentRecords{}, err
	}
	members, err := client.ProjectMembers(ctx, projectID)
	if err != nil {
		return IndependentRecords{}, err
	}
	if err := same(members.AsOfEvent); err != nil {
		return IndependentRecords{}, err
	}
	logins := map[uuid.UUID]string{}
	for _, member := range members.Data.Members {
		logins[member.Human.HumanID] = member.Human.GitHubLogin
	}
	owner := ""
	if view.Data.OwnerHumanID != nil {
		owner = logins[*view.Data.OwnerHumanID]
	}
	if owner == "" {
		owner = "unknown"
	}
	collisions := append(append([]protocol.CollisionAssessment{}, radar.Data.OpenCollisions...), radar.Data.DecidedCollisions...)
	work := protocol.WorkContext{
		Project: protocol.WorkContextProject{ProjectID: radar.Data.Project.ProjectID, Name: radar.Data.Project.Name, Role: protocol.RoleMember},
		Focus:   &protocol.WorkContextFocus{Objective: objective.Data, SelectedIntent: &revision, OwnCharters: []protocol.OwnCharter{}},
		KnownUnknowns: []string{
			"This packet was generated without a Human reader, so Charter acceptance, decision details, and proposal counts are not loaded.",
		},
		AsOfEvent: asOf,
	}
	return IndependentRecords{
		Records: Records{
			Work: work, Radar: radar.Data, Collisions: IntentCollisions(collisions, intentID, revision.Revision),
			Contexts: map[uuid.UUID]protocol.ConflictContextPack{}, Evidence: evidence.Data.Evidence, Logins: logins,
			Proposals: map[uuid.UUID]int{}, Decisions: map[uuid.UUID]protocol.ResolutionDecision{}, AsOfEvent: asOf,
		},
		View: view.Data, OwnerLogin: owner,
	}, nil
}
