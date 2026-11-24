package tui

// conn app shows every record by what a person calls it: a project by its
// name, a human by GitHub login, an objective or a cell by its title, a
// blueprint by the objective it divides, a repository by its remote, an
// Intent by its summary, and a collision by its rule and Intents. UUIDs stay
// internal. The Account section shows the two that a person hands on: their
// own human ID, which an owner needs to add them, and the project ID, which
// the conn commands take.

import (
	"fmt"
	"strings"
	"uuid"

	"github.com/Juanqu261/conn/internal/protocol"
)

func quote(name string) string { return "“" + name + "”" }

// memberName names a human by GitHub login.
func (m model) memberName(id uuid.UUID) string {
	for _, member := range m.members.Members {
		if member.Human.HumanID == id {
			return "@" + clean(member.Human.GitHubLogin)
		}
	}
	if id == m.human.HumanID && m.human.GitHubLogin != "" {
		return "@" + clean(m.human.GitHubLogin)
	}
	return "someone outside this project"
}

// objectiveTitle names an Objective by the title of its current revision.
func (m model) objectiveTitle(id uuid.UUID) string {
	for _, objective := range m.radar.Objectives {
		if objective.ObjectiveID == id {
			return clean(objective.Title)
		}
	}
	return "unlisted objective"
}

// blueprintName names a blueprint by the Objective it divides. An Objective
// has at most one blueprint (decision 0005).
func (m model) blueprintName(id uuid.UUID) string {
	for _, blueprint := range m.status.Blueprints {
		if blueprint.BlueprintID == id && len(blueprint.Revisions) > 0 {
			return blueprintLabel(m.objectiveTitle(blueprint.Revisions[len(blueprint.Revisions)-1].ObjectiveID))
		}
	}
	return "Unlisted blueprint"
}

func blueprintLabel(objectiveTitle string) string { return "Blueprint for " + quote(objectiveTitle) }

// cellTitle names a charter by its cell's title.
func (m model) cellTitle(id uuid.UUID) string {
	for _, blueprint := range m.status.Blueprints {
		for _, revision := range blueprint.Revisions {
			for _, cell := range revision.Cells {
				if cell.CellID == id {
					return clean(cell.Title)
				}
			}
		}
	}
	return "unlisted charter"
}

func (m model) repositoryName(id *uuid.UUID) string {
	if id == nil {
		return "none"
	}
	for _, repository := range m.radar.Repositories {
		if repository.RepositoryID == *id {
			return clean(repository.CanonicalRemote)
		}
	}
	return "unlisted repository"
}

// intentTitle names an Intent by the first line of its summary.
func intentTitle(revision protocol.IntentRevision) string {
	title := strings.TrimSpace(strings.TrimLeft(firstLine(revision.SummaryMarkdown, 60), "#"))
	if title == "" {
		return "Untitled Intent"
	}
	return title
}

// intentName is an Intent's quoted title, for use in a sentence.
func (m model) intentName(id uuid.UUID) string {
	for _, intent := range m.intents.Intents {
		if intent.IntentRevision.IntentID == id {
			return quote(intentTitle(intent.IntentRevision))
		}
	}
	for _, revision := range m.radar.ActiveIntents {
		if revision.IntentID == id {
			return quote(intentTitle(revision))
		}
	}
	return "an unlisted Intent"
}

// collisionName names a collision by its rule and the Intents it relates.
func (m model) collisionName(collision protocol.CollisionAssessment) string {
	return fmt.Sprintf("%s between %s and %s", clean(collision.RuleID), m.intentName(collision.LeftIntentID), m.intentName(collision.RightIntentID))
}

// proposalName names a proposal by its kind and author.
func (m model) proposalName(proposal protocol.ResolutionProposal) string {
	author := "a participant"
	if proposal.ProposedByHumanID != nil {
		author = m.memberName(*proposal.ProposedByHumanID)
	}
	return fmt.Sprintf("%s proposal by %s", proposal.Kind, author)
}

// describeEvent says what an event changed, naming the records it touched.
func (m model) describeEvent(event protocol.EventEnvelope) string {
	id := event.EntityID
	switch payload := event.Payload.(type) {
	case protocol.ProjectCreated:
		return "Project created"
	case protocol.ProjectMemberAdded:
		return fmt.Sprintf("%s added as %s", m.memberName(payload.HumanID), payload.Role)
	case protocol.ObjectiveRevisionCreated:
		return fmt.Sprintf("Objective %s revision %d created", quote(m.objectiveTitle(id)), payload.Revision)
	case protocol.ObjectiveImported:
		return fmt.Sprintf("Objective %s revision %d imported", quote(m.objectiveTitle(id)), payload.Revision)
	case protocol.BlueprintRevisionProposed:
		return fmt.Sprintf("%s revision %d proposed", m.blueprintName(id), payload.Revision)
	case protocol.BlueprintRevisionActivated:
		return fmt.Sprintf("%s revision %d activated", m.blueprintName(id), payload.Revision)
	case protocol.BlueprintRevisionRejected:
		return fmt.Sprintf("%s revision %d rejected at charter %s", m.blueprintName(id), payload.Revision, quote(m.cellTitle(payload.CellID)))
	case protocol.CharterAccepted:
		return fmt.Sprintf("Charter %s accepted", quote(m.cellTitle(payload.CellID)))
	case protocol.CharterRejected:
		return fmt.Sprintf("Charter %s rejected", quote(m.cellTitle(payload.CellID)))
	case protocol.RepositoryConnected:
		return fmt.Sprintf("Repository %s connected", m.repositoryName(&id))
	case protocol.IntentPublished:
		return fmt.Sprintf("Intent %s published", m.intentName(id))
	case protocol.IntentRevised:
		return fmt.Sprintf("Intent %s revised to revision %d", m.intentName(id), payload.Revision)
	case protocol.IntentReleased:
		return fmt.Sprintf("Intent %s released", m.intentName(id))
	case protocol.ClaimsObserved:
		return fmt.Sprintf("Claims observed on Intent %s revision %d", m.intentName(payload.IntentID), payload.IntentRevision)
	case protocol.CollisionOpened, protocol.CollisionUpdated:
		verb := "opened"
		if event.EventType() == protocol.EventCollisionUpdated {
			verb = "updated"
		}
		for _, collision := range m.radar.OpenCollisions {
			if collision.CollisionID == id {
				return fmt.Sprintf("Collision %s: %s", verb, m.collisionName(collision))
			}
		}
		return "Collision " + verb
	case protocol.ResolutionProposed:
		return "Resolution proposed"
	case protocol.ResolutionDecided:
		return "Resolution decided"
	case protocol.EvidenceAttached:
		return "Evidence attached"
	}
	return strings.ReplaceAll(string(event.EventType()), "_", " ")
}
