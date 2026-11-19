package httpclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"uuid"

	"github.com/Juanqu261/conn/internal/protocol"
)

// Result is a project-scoped response: the data and the project event
// cursor it reflects.
type Result[T any] struct {
	Data      T
	AsOfEvent int64
}

// Projects lists the projects visible to the authenticated human.
func (c *Client) Projects(ctx context.Context) (protocol.ProjectList, error) {
	return get[protocol.ProjectList](ctx, c, "/v1/projects")
}

// ProjectMembers lists every member of a Project visible to the caller.
func (c *Client) ProjectMembers(ctx context.Context, projectID uuid.UUID) (Result[protocol.ProjectMemberList], error) {
	return read[protocol.ProjectMemberList](ctx, c, projectPath(projectID)+"/members")
}

// CreateProject creates a Project owned by the session's human.
func (c *Client) CreateProject(ctx context.Context, input protocol.ProjectCreate) (Result[protocol.Project], error) {
	return post[protocol.Project](ctx, c, "/v1/projects", input)
}

// Project reads a Project the session's human belongs to.
func (c *Client) Project(ctx context.Context, projectID uuid.UUID) (Result[protocol.Project], error) {
	return read[protocol.Project](ctx, c, projectPath(projectID))
}

// AddMember adds an authenticated human to a Project as a member.
func (c *Client) AddMember(ctx context.Context, projectID, humanID uuid.UUID) (Result[protocol.ProjectMembership], error) {
	return post[protocol.ProjectMembership](ctx, c, projectPath(projectID)+"/members", protocol.ProjectMemberAdd{HumanID: humanID})
}

// CreateObjective creates revision 1 of an Objective.
func (c *Client) CreateObjective(ctx context.Context, projectID uuid.UUID, input protocol.ObjectiveRevisionInput) (Result[protocol.Objective], error) {
	return post[protocol.Objective](ctx, c, projectPath(projectID)+"/objectives", input)
}

// ReviseObjective appends the revision after expectedRevision.
func (c *Client) ReviseObjective(ctx context.Context, projectID uuid.UUID, expectedRevision int, input protocol.ObjectiveRevisionInput) (Result[protocol.Objective], error) {
	path := fmt.Sprintf("%s/objectives/%s/revisions?expected_revision=%d", projectPath(projectID), input.ObjectiveID, expectedRevision)
	return post[protocol.Objective](ctx, c, path, input)
}

// ObjectiveRevision reads one immutable Objective revision.
func (c *Client) ObjectiveRevision(ctx context.Context, projectID, objectiveID uuid.UUID, revision int) (Result[protocol.Objective], error) {
	return read[protocol.Objective](ctx, c, fmt.Sprintf("%s/objectives/%s/revisions/%d", projectPath(projectID), objectiveID, revision))
}

// ProposeBlueprint proposes revision 1 of a Blueprint.
func (c *Client) ProposeBlueprint(ctx context.Context, projectID uuid.UUID, input protocol.BlueprintProposal) (Result[protocol.BlueprintRevisionView], error) {
	return post[protocol.BlueprintRevisionView](ctx, c, projectPath(projectID)+"/blueprints", input)
}

// ReviseBlueprint proposes the revision after expectedRevision.
func (c *Client) ReviseBlueprint(ctx context.Context, projectID uuid.UUID, expectedRevision int, input protocol.BlueprintProposal) (Result[protocol.BlueprintRevisionView], error) {
	path := fmt.Sprintf("%s/blueprints/%s/revisions?expected_revision=%d", projectPath(projectID), input.BlueprintID, expectedRevision)
	return post[protocol.BlueprintRevisionView](ctx, c, path, input)
}

// BlueprintRevision reads one BlueprintRevision and its status.
func (c *Client) BlueprintRevision(ctx context.Context, projectID, blueprintID uuid.UUID, revision int) (Result[protocol.BlueprintRevisionView], error) {
	return read[protocol.BlueprintRevisionView](ctx, c, revisionPath(projectID, blueprintID, revision))
}

// AcceptCharter accepts the session human's own charter.
func (c *Client) AcceptCharter(ctx context.Context, projectID, blueprintID uuid.UUID, revision int, cellID uuid.UUID) (Result[protocol.CharterDecision], error) {
	path := fmt.Sprintf("%s/cells/%s/accept", revisionPath(projectID, blueprintID, revision), cellID)
	body, err := c.send(ctx, http.MethodPost, path, c.token, nil)
	if err != nil {
		return Result[protocol.CharterDecision]{}, err
	}
	return decodeResult[protocol.CharterDecision](body)
}

// RejectCharter rejects the session human's own charter with a rationale.
func (c *Client) RejectCharter(ctx context.Context, projectID, blueprintID uuid.UUID, revision int, cellID uuid.UUID, rationale string) (Result[protocol.CharterDecision], error) {
	path := fmt.Sprintf("%s/cells/%s/reject", revisionPath(projectID, blueprintID, revision), cellID)
	return post[protocol.CharterDecision](ctx, c, path, protocol.CharterRejection{Rationale: rationale})
}

// Status reads the Project's coordination state.
func (c *Client) Status(ctx context.Context, projectID uuid.UUID) (Result[protocol.ProjectStatus], error) {
	return read[protocol.ProjectStatus](ctx, c, projectPath(projectID)+"/status")
}

// Events reads at most limit committed events after the cursor.
func (c *Client) Events(ctx context.Context, projectID uuid.UUID, after int64, limit int) (Result[[]protocol.EventEnvelope], error) {
	query := url.Values{"after": {strconv.FormatInt(after, 10)}, "limit": {strconv.Itoa(limit)}}
	body, err := c.send(ctx, http.MethodGet, projectPath(projectID)+"/events?"+query.Encode(), c.token, nil)
	if err != nil {
		return Result[[]protocol.EventEnvelope]{}, err
	}
	var envelope struct {
		Data      []json.RawMessage `json:"data"`
		AsOfEvent *int64            `json:"as_of_event"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || envelope.Data == nil || envelope.AsOfEvent == nil {
		return Result[[]protocol.EventEnvelope]{}, errors.New("the Conn server returned an unreadable response")
	}
	result := Result[[]protocol.EventEnvelope]{Data: make([]protocol.EventEnvelope, 0, len(envelope.Data)), AsOfEvent: *envelope.AsOfEvent}
	for _, raw := range envelope.Data {
		event, err := protocol.Decode[protocol.EventEnvelope](raw)
		if err != nil {
			return Result[[]protocol.EventEnvelope]{}, fmt.Errorf("the Conn server returned an invalid event: %w", err)
		}
		result.Data = append(result.Data, event)
	}
	return result, nil
}

// post encodes input, which validates it against its schema before it
// leaves the process, and sends it.
func post[T protocol.Document](ctx context.Context, c *Client, path string, input protocol.Document) (Result[T], error) {
	data, err := protocol.Encode(input)
	if err != nil {
		return Result[T]{}, err
	}
	body, err := c.send(ctx, http.MethodPost, path, c.token, data)
	if err != nil {
		return Result[T]{}, err
	}
	return decodeResult[T](body)
}

func read[T protocol.Document](ctx context.Context, c *Client, path string) (Result[T], error) {
	body, err := c.send(ctx, http.MethodGet, path, c.token, nil)
	if err != nil {
		return Result[T]{}, err
	}
	return decodeResult[T](body)
}

func decodeResult[T protocol.Document](body []byte) (Result[T], error) {
	doc, err := decodeData[T](body)
	if err != nil {
		return Result[T]{}, err
	}
	var envelope struct {
		AsOfEvent *int64 `json:"as_of_event"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || envelope.AsOfEvent == nil {
		return Result[T]{}, errors.New("the Conn server response has no event cursor")
	}
	return Result[T]{Data: doc, AsOfEvent: *envelope.AsOfEvent}, nil
}

func projectPath(projectID uuid.UUID) string { return "/v1/projects/" + projectID.String() }

func revisionPath(projectID, blueprintID uuid.UUID, revision int) string {
	return fmt.Sprintf("%s/blueprints/%s/revisions/%d", projectPath(projectID), blueprintID, revision)
}
