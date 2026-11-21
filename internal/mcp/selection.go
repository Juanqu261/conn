package mcp

import (
	"fmt"
	"strings"
	"uuid"

	"github.com/Juanqu261/conn/internal/protocol"
)

// pickObjective returns the Objective named by id, or the project's only
// Objective when id is zero.
func pickObjective(status protocol.ProjectStatus, id uuid.UUID) (protocol.ObjectiveSummary, error) {
	ids := make([]string, 0, len(status.Objectives))
	for _, o := range status.Objectives {
		if o.ObjectiveID == id {
			return o, nil
		}
		ids = append(ids, o.ObjectiveID.String())
	}
	switch {
	case id != (uuid.UUID{}):
		return protocol.ObjectiveSummary{}, fmt.Errorf("objective %s is not in this project", id)
	case len(status.Objectives) == 1:
		return status.Objectives[0], nil
	case len(status.Objectives) == 0:
		return protocol.ObjectiveSummary{}, fmt.Errorf("the project has no objective yet")
	}
	return protocol.ObjectiveSummary{}, fmt.Errorf("the project has %d objectives; pass objective_id, one of: %s", len(ids), strings.Join(ids, ", "))
}

// pickBlueprint returns the Blueprint named by raw, or the project's only
// Blueprint when raw is empty.
func pickBlueprint(status protocol.ProjectStatus, raw string) (protocol.BlueprintSummary, error) {
	var id uuid.UUID
	if raw != "" {
		var err error
		if id, err = parseID("blueprint_id", raw); err != nil {
			return protocol.BlueprintSummary{}, err
		}
	}
	for _, b := range status.Blueprints {
		if b.BlueprintID == id {
			return b, nil
		}
	}
	switch {
	case raw != "":
		return protocol.BlueprintSummary{}, fmt.Errorf("blueprint %s is not in this project", id)
	case len(status.Blueprints) == 1:
		return status.Blueprints[0], nil
	case len(status.Blueprints) == 0:
		return protocol.BlueprintSummary{}, fmt.Errorf("the project has no blueprint yet")
	}
	return protocol.BlueprintSummary{}, fmt.Errorf("the project has %d blueprints; pass blueprint_id, one of: %s", len(status.Blueprints), describeBlueprints(status.Blueprints))
}

// pickInForce selects a revision in force: the requested one, else the
// active one, else the latest.
func pickInForce(status protocol.ProjectStatus, in blueprintInput) (protocol.BlueprintSummary, protocol.RevisionSummary, error) {
	blueprint, err := pickBlueprint(status, in.BlueprintID)
	if err != nil {
		return blueprint, protocol.RevisionSummary{}, err
	}
	var available []string
	for _, r := range blueprint.Revisions {
		if r.Revision == in.Revision {
			return blueprint, r, nil
		}
		available = append(available, fmt.Sprint(r.Revision))
	}
	if in.Revision != 0 {
		return blueprint, protocol.RevisionSummary{}, fmt.Errorf("revision %d of blueprint %s is not in force; revisions in force: %s",
			in.Revision, blueprint.BlueprintID, strings.Join(available, ", "))
	}
	for _, r := range blueprint.Revisions {
		if r.Status == protocol.BlueprintActive {
			return blueprint, r, nil
		}
	}
	return blueprint, blueprint.Revisions[len(blueprint.Revisions)-1], nil
}

// pickMyInForce selects the revision in force for read_my_charter. Each
// Objective has its own blueprint, so without blueprint_id it looks only at
// the blueprints where the human has a charter. Without a revision it takes
// the active revision when it includes the human, else the newest that does.
func pickMyInForce(status protocol.ProjectStatus, in blueprintInput, me uuid.UUID) (protocol.BlueprintSummary, protocol.RevisionSummary, error) {
	if in.BlueprintID == "" && len(status.Blueprints) > 1 {
		var mine []protocol.BlueprintSummary
		for _, b := range status.Blueprints {
			for _, r := range b.Revisions {
				if includes(r, me) {
					mine = append(mine, b)
					break
				}
			}
		}
		switch len(mine) {
		case 0:
			return protocol.BlueprintSummary{}, protocol.RevisionSummary{}, fmt.Errorf(
				"you have no charter in any of the project's %d blueprints. Use read_blueprint with blueprint_id to see who is accountable for each cell",
				len(status.Blueprints))
		case 1:
			in.BlueprintID = mine[0].BlueprintID.String()
		default:
			return protocol.BlueprintSummary{}, protocol.RevisionSummary{}, fmt.Errorf(
				"you have charters in %d blueprints; pass blueprint_id, one of: %s", len(mine), describeBlueprints(mine))
		}
	}
	blueprint, revision, err := pickInForce(status, in)
	if err != nil || in.Revision != 0 || includes(revision, me) {
		return blueprint, revision, err
	}
	for i := len(blueprint.Revisions) - 1; i >= 0; i-- {
		if includes(blueprint.Revisions[i], me) {
			return blueprint, blueprint.Revisions[i], nil
		}
	}
	return blueprint, revision, nil
}

func includes(revision protocol.RevisionSummary, human uuid.UUID) bool {
	for _, cell := range revision.Cells {
		if cell.AccountableHumanID == human {
			return true
		}
	}
	return false
}

// describeBlueprints names each blueprint with the Objective it divides.
func describeBlueprints(blueprints []protocol.BlueprintSummary) string {
	names := make([]string, 0, len(blueprints))
	for _, b := range blueprints {
		name := b.BlueprintID.String()
		if len(b.Revisions) > 0 {
			name += fmt.Sprintf(" (objective %s)", b.Revisions[0].ObjectiveID)
		}
		names = append(names, name)
	}
	return strings.Join(names, ", ")
}

// newerRevision returns a revision in force newer than revision, if any.
func newerRevision(blueprint protocol.BlueprintSummary, revision int) *protocol.RevisionSummary {
	for i := range blueprint.Revisions {
		if blueprint.Revisions[i].Revision > revision {
			return &blueprint.Revisions[i]
		}
	}
	return nil
}

// approval states plainly whether work may rely on a revision.
func approval(status protocol.BlueprintStatus) string {
	if status == protocol.BlueprintActive {
		return "is active and approved."
	}
	return fmt.Sprintf("is %s and unapproved; do not treat it as the ratified partition.", status)
}
