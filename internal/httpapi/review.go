package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"uuid"

	"github.com/Juanqu261/conn/internal/application"
	"github.com/Juanqu261/conn/internal/httpclient"
	"github.com/Juanqu261/conn/internal/protocol"
	"github.com/Juanqu261/conn/internal/review"
)

// registerReviewRoutes serves the generated review packet (decision 0013,
// amendment 1). The packet is built by the same internal/review loader and
// builder the CLI uses, from the same use cases, without local Git facts.
// It is generated on demand and never stored.
func registerReviewRoutes(mux *http.ServeMux, cfg Config) {
	log, projects := cfg.Log, cfg.Projects
	mux.HandleFunc("GET /v1/projects/{project_id}/intents/{intent_id}/review-packet", authenticated(cfg.Auth, log, func(w http.ResponseWriter, r *http.Request, s application.Session) {
		projectID, ok := pathID(w, r, log, "project_id")
		if !ok {
			return
		}
		intentID, ok := pathID(w, r, log, "intent_id")
		if !ok || !allowQueryParameters(w, r, log, "format") {
			return
		}
		format := r.URL.Query().Get("format")
		if format == "" {
			format = "json"
		}
		if format != "json" && format != "markdown" {
			writeInvalid(w, log, "The format query parameter must be json or markdown.", "")
			return
		}
		view, err := projects.IntentAs(r.Context(), application.HumanReader(s.Human.HumanID), projectID, intentID)
		if err != nil {
			writeProblem(w, log, err)
			return
		}
		records, err := review.Load(r.Context(), inProcessSource{projects, s.Human}, projectID, intentID, review.LoadOptions{Review: true})
		if errors.Is(err, review.ErrStaleContext) {
			writeError(w, log, http.StatusConflict, "stale_context", "The Project changed while the packet was being read. Try again.")
			return
		}
		if err != nil {
			writeProblem(w, log, err)
			return
		}
		owner := "unknown"
		if view.Data.OwnerHumanID != nil {
			if login := records.Logins[*view.Data.OwnerHumanID]; login != "" {
				owner = login
			}
		}
		if view.Data.OwnerHumanID == nil || *view.Data.OwnerHumanID != s.Human.HumanID {
			// Charter acceptance in the packet comes from the reader's own
			// Charters, so a reviewer who is not the owner cannot see it.
			records.Work.KnownUnknowns = append(records.Work.KnownUnknowns,
				"This packet was generated for a reviewer who is not the Intent owner, so Charter acceptance reflects only the reviewer's own Charters.")
		}
		packet, err := review.BuildPacket(review.PacketInput{Records: records, Written: map[string]bool{}, OwnerLogin: owner})
		if err != nil {
			internalError(w, log, "review packet", err)
			return
		}
		if format == "json" {
			respond(w, log, http.StatusOK, application.Result[protocol.ReviewPacket]{Data: packet, AsOfEvent: records.AsOfEvent}, nil)
			return
		}
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Conn-As-Of-Event", strconv.FormatInt(records.AsOfEvent, 10))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(review.RenderMarkdown(packet)))
	}))
}

// inProcessSource gives the review loader the same reads as the HTTP client,
// answered by the use cases for one signed-in Human.
type inProcessSource struct {
	projects *application.Projects
	human    protocol.Human
}

func wrap[T any](result application.Result[T], err error) (httpclient.Result[T], error) {
	return httpclient.Result[T]{Data: result.Data, AsOfEvent: result.AsOfEvent}, err
}

func (s inProcessSource) reader() application.Reader { return application.HumanReader(s.human.HumanID) }

func (s inProcessSource) WorkContext(ctx context.Context, projectID, objectiveID, intentID uuid.UUID, after *int64, limit int) (httpclient.Result[protocol.WorkContext], error) {
	return wrap(s.projects.WorkContext(ctx, s.human, projectID, application.WorkContextQuery{
		ObjectiveID: objectiveID, IntentID: intentID, After: after, Limit: limit, AdapterKind: "cli",
	}))
}

func (s inProcessSource) Radar(ctx context.Context, projectID uuid.UUID) (httpclient.Result[protocol.RadarView], error) {
	return wrap(s.projects.RadarAs(ctx, s.reader(), projectID))
}

func (s inProcessSource) ConflictContext(ctx context.Context, projectID, collisionID uuid.UUID) (httpclient.Result[protocol.ConflictContextPack], error) {
	return wrap(s.projects.ConflictContextAs(ctx, s.reader(), projectID, collisionID))
}

func (s inProcessSource) IntentEvidence(ctx context.Context, projectID, intentID uuid.UUID) (httpclient.Result[protocol.EvidenceReferenceList], error) {
	return wrap(s.projects.IntentEvidenceAs(ctx, s.reader(), projectID, intentID))
}

func (s inProcessSource) ProjectMembers(ctx context.Context, projectID uuid.UUID) (httpclient.Result[protocol.ProjectMemberList], error) {
	return wrap(s.projects.ProjectMembersAs(ctx, s.reader(), projectID))
}

func (s inProcessSource) ResolutionProposals(ctx context.Context, projectID, collisionID uuid.UUID) (httpclient.Result[protocol.ResolutionProposalList], error) {
	return wrap(s.projects.ResolutionProposals(ctx, s.human.HumanID, projectID, collisionID))
}

func (s inProcessSource) ResolutionDecision(ctx context.Context, projectID, decisionID uuid.UUID) (httpclient.Result[protocol.ResolutionDecision], error) {
	return wrap(s.projects.ResolutionDecision(ctx, s.human.HumanID, projectID, decisionID))
}
