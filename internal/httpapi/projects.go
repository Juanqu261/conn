package httpapi

import (
	"math"
	"net/http"

	"github.com/Juanqu261/conn/internal/application"
	"github.com/Juanqu261/conn/internal/protocol"
)

const defaultEventPage = 100

// registerProjectRoutes serves the project-scoped /v1 routes. Handlers
// authenticate the session and parse the request; the application
// authorizes project membership and role inside the same transaction as
// the operation, so authorization cannot drift from the state it guards.
func registerProjectRoutes(mux *http.ServeMux, cfg Config) {
	log, projects := cfg.Log, cfg.Projects
	handle := func(pattern string, h func(http.ResponseWriter, *http.Request, application.Session)) {
		mux.HandleFunc(pattern, authenticated(cfg.Auth, log, h))
	}

	handle("GET /v1/projects", func(w http.ResponseWriter, r *http.Request, s application.Session) {
		list, err := projects.ProjectsForHuman(r.Context(), s.Human.HumanID)
		if err != nil {
			writeProblem(w, log, err)
			return
		}
		writeData(w, log, http.StatusOK, list)
	})

	handle("POST /v1/projects", func(w http.ResponseWriter, r *http.Request, s application.Session) {
		input, ok := readDocument[protocol.ProjectCreate](w, r, log)
		if !ok {
			return
		}
		result, err := projects.CreateProject(r.Context(), s.Human.HumanID, input)
		respond(w, log, http.StatusCreated, result, err)
	})

	handle("GET /v1/projects/{project_id}", func(w http.ResponseWriter, r *http.Request, s application.Session) {
		projectID, ok := pathID(w, r, log, "project_id")
		if !ok {
			return
		}
		result, err := projects.Project(r.Context(), s.Human.HumanID, projectID)
		respond(w, log, http.StatusOK, result, err)
	})

	handle("GET /v1/projects/{project_id}/members", func(w http.ResponseWriter, r *http.Request, s application.Session) {
		projectID, ok := pathID(w, r, log, "project_id")
		if !ok {
			return
		}
		result, err := projects.ProjectMembers(r.Context(), s.Human.HumanID, projectID)
		respond(w, log, http.StatusOK, result, err)
	})

	handle("POST /v1/projects/{project_id}/members", func(w http.ResponseWriter, r *http.Request, s application.Session) {
		projectID, ok := pathID(w, r, log, "project_id")
		if !ok {
			return
		}
		input, ok := readDocument[protocol.ProjectMemberAdd](w, r, log)
		if !ok {
			return
		}
		result, err := projects.AddMember(r.Context(), s.Human.HumanID, projectID, input.HumanID)
		respond(w, log, http.StatusCreated, result, err)
	})

	handle("GET /v1/projects/{project_id}/radar", func(w http.ResponseWriter, r *http.Request, s application.Session) {
		projectID, ok := pathID(w, r, log, "project_id")
		if !ok {
			return
		}
		result, err := projects.Radar(r.Context(), s.Human.HumanID, projectID)
		respond(w, log, http.StatusOK, result, err)
	})

	handle("GET /v1/projects/{project_id}/collisions/{collision_id}/context", func(w http.ResponseWriter, r *http.Request, s application.Session) {
		projectID, ok := pathID(w, r, log, "project_id")
		if !ok {
			return
		}
		collisionID, ok := pathID(w, r, log, "collision_id")
		if !ok {
			return
		}
		result, err := projects.ConflictContext(r.Context(), s.Human.HumanID, projectID, collisionID)
		respond(w, log, http.StatusOK, result, err)
	})

	handle("POST /v1/projects/{project_id}/collisions/{collision_id}/proposals", func(w http.ResponseWriter, r *http.Request, s application.Session) {
		projectID, ok := pathID(w, r, log, "project_id")
		if !ok {
			return
		}
		collisionID, ok := pathID(w, r, log, "collision_id")
		if !ok {
			return
		}
		input, ok := readDocument[protocol.ResolutionProposalInput](w, r, log)
		if !ok {
			return
		}
		participant, err := projects.ResolveParticipant(r.Context(), s.Human.HumanID, projectID, participantAdapter(r), "")
		if err != nil {
			writeProblem(w, log, err)
			return
		}
		result, err := projects.ProposeResolution(r.Context(), s.Human.HumanID, projectID, participant.Data.ParticipantID, collisionID, input)
		respond(w, log, http.StatusCreated, result, err)
	})

	handle("POST /v1/projects/{project_id}/collisions/{collision_id}/decisions", func(w http.ResponseWriter, r *http.Request, s application.Session) {
		projectID, ok := pathID(w, r, log, "project_id")
		if !ok {
			return
		}
		collisionID, ok := pathID(w, r, log, "collision_id")
		if !ok {
			return
		}
		input, ok := readDocument[protocol.ResolutionDecisionInput](w, r, log)
		if !ok {
			return
		}
		result, err := projects.DecideResolution(r.Context(), s.Human.HumanID, projectID, collisionID, input)
		respond(w, log, http.StatusCreated, result, err)
	})

	handle("GET /v1/projects/{project_id}/repositories", func(w http.ResponseWriter, r *http.Request, s application.Session) {
		projectID, ok := pathID(w, r, log, "project_id")
		if !ok {
			return
		}
		result, err := projects.RepositoryConnections(r.Context(), s.Human.HumanID, projectID)
		respond(w, log, http.StatusOK, result, err)
	})

	handle("POST /v1/projects/{project_id}/repositories", func(w http.ResponseWriter, r *http.Request, s application.Session) {
		projectID, ok := pathID(w, r, log, "project_id")
		if !ok {
			return
		}
		input, ok := readDocument[protocol.RepositoryConnectInput](w, r, log)
		if !ok {
			return
		}
		result, err := projects.ConnectRepository(r.Context(), s.Human.HumanID, projectID, input)
		respond(w, log, http.StatusCreated, result, err)
	})

	handle("POST /v1/projects/{project_id}/intents", func(w http.ResponseWriter, r *http.Request, s application.Session) {
		projectID, ok := pathID(w, r, log, "project_id")
		if !ok {
			return
		}
		input, ok := readDocument[protocol.IntentInput](w, r, log)
		if !ok {
			return
		}
		participant, err := projects.ResolveParticipant(r.Context(), s.Human.HumanID, projectID, participantAdapter(r), "")
		if err != nil {
			writeProblem(w, log, err)
			return
		}
		result, err := projects.PublishIntent(r.Context(), s.Human.HumanID, projectID, participant.Data.ParticipantID, input)
		respond(w, log, http.StatusCreated, result, err)
	})

	handle("POST /v1/projects/{project_id}/intents/{intent_id}/revisions", func(w http.ResponseWriter, r *http.Request, s application.Session) {
		projectID, ok := pathID(w, r, log, "project_id")
		if !ok {
			return
		}
		intentID, ok := pathID(w, r, log, "intent_id")
		if !ok {
			return
		}
		expected, ok := queryInt(w, r, log, "expected_revision", 1, math.MaxInt32-1, nil)
		if !ok {
			return
		}
		input, ok := readDocument[protocol.IntentInput](w, r, log)
		if !ok {
			return
		}
		if input.IntentID != intentID {
			writeInvalid(w, log, "The intent_id must match the Intent in the request path.", "/intent_id")
			return
		}
		participant, err := projects.ResolveParticipant(r.Context(), s.Human.HumanID, projectID, participantAdapter(r), "")
		if err != nil {
			writeProblem(w, log, err)
			return
		}
		result, err := projects.ReviseIntent(r.Context(), s.Human.HumanID, projectID, participant.Data.ParticipantID, int(expected), input)
		respond(w, log, http.StatusCreated, result, err)
	})

	handle("POST /v1/projects/{project_id}/intents/{intent_id}/release", func(w http.ResponseWriter, r *http.Request, s application.Session) {
		projectID, ok := pathID(w, r, log, "project_id")
		if !ok {
			return
		}
		intentID, ok := pathID(w, r, log, "intent_id")
		if !ok {
			return
		}
		participant, err := projects.ResolveParticipant(r.Context(), s.Human.HumanID, projectID, participantAdapter(r), "")
		if err != nil {
			writeProblem(w, log, err)
			return
		}
		result, err := projects.ReleaseIntent(r.Context(), s.Human.HumanID, projectID, participant.Data.ParticipantID, intentID)
		respond(w, log, http.StatusCreated, result, err)
	})

	handle("GET /v1/projects/{project_id}/intents", func(w http.ResponseWriter, r *http.Request, s application.Session) {
		projectID, ok := pathID(w, r, log, "project_id")
		if !ok {
			return
		}
		result, err := projects.Intents(r.Context(), s.Human.HumanID, projectID)
		respond(w, log, http.StatusOK, result, err)
	})

	handle("GET /v1/projects/{project_id}/intents/{intent_id}", func(w http.ResponseWriter, r *http.Request, s application.Session) {
		projectID, ok := pathID(w, r, log, "project_id")
		if !ok {
			return
		}
		intentID, ok := pathID(w, r, log, "intent_id")
		if !ok {
			return
		}
		result, err := projects.Intent(r.Context(), s.Human.HumanID, projectID, intentID)
		respond(w, log, http.StatusOK, result, err)
	})

	handle("POST /v1/projects/{project_id}/intents/{intent_id}/evidence", func(w http.ResponseWriter, r *http.Request, s application.Session) {
		projectID, ok := pathID(w, r, log, "project_id")
		if !ok {
			return
		}
		intentID, ok := pathID(w, r, log, "intent_id")
		if !ok {
			return
		}
		input, ok := readDocument[protocol.EvidenceAttachmentInput](w, r, log)
		if !ok {
			return
		}
		if input.IntentID != intentID {
			writeInvalid(w, log, "The intent_id must match the Intent in the request path.", "/intent_id")
			return
		}
		participant, err := projects.ResolveParticipant(r.Context(), s.Human.HumanID, projectID, participantAdapter(r), "")
		if err != nil {
			writeProblem(w, log, err)
			return
		}
		result, err := projects.AttachEvidence(r.Context(), s.Human.HumanID, projectID, participant.Data.ParticipantID, input)
		respond(w, log, http.StatusCreated, result, err)
	})

	handle("POST /v1/projects/{project_id}/intents/{intent_id}/analysis", func(w http.ResponseWriter, r *http.Request, s application.Session) {
		projectID, ok := pathID(w, r, log, "project_id")
		if !ok {
			return
		}
		intentID, ok := pathID(w, r, log, "intent_id")
		if !ok {
			return
		}
		report, ok := readDocument[protocol.LocalAnalyzerReport](w, r, log)
		if !ok {
			return
		}
		participant, err := projects.ResolveParticipant(r.Context(), s.Human.HumanID, projectID, participantAdapter(r), "")
		if err != nil {
			writeProblem(w, log, err)
			return
		}
		result, err := projects.AttachStaticAnalysis(r.Context(), s.Human.HumanID, projectID, participant.Data.ParticipantID, intentID, report)
		respond(w, log, http.StatusCreated, result, err)
	})

	handle("POST /v1/projects/{project_id}/objectives", func(w http.ResponseWriter, r *http.Request, s application.Session) {
		projectID, ok := pathID(w, r, log, "project_id")
		if !ok {
			return
		}
		input, ok := readDocument[protocol.ObjectiveRevisionInput](w, r, log)
		if !ok {
			return
		}
		result, err := projects.CreateObjectiveRevision(r.Context(), s.Human.HumanID, projectID, input, 0)
		respond(w, log, http.StatusCreated, result, err)
	})

	handle("POST /v1/projects/{project_id}/objectives/{objective_id}/revisions", func(w http.ResponseWriter, r *http.Request, s application.Session) {
		projectID, ok := pathID(w, r, log, "project_id")
		if !ok {
			return
		}
		objectiveID, ok := pathID(w, r, log, "objective_id")
		if !ok {
			return
		}
		expected, ok := queryInt(w, r, log, "expected_revision", 1, math.MaxInt32-1, nil)
		if !ok {
			return
		}
		input, ok := readDocument[protocol.ObjectiveRevisionInput](w, r, log)
		if !ok {
			return
		}
		if input.ObjectiveID != objectiveID {
			writeInvalid(w, log, "The objective_id must match the Objective in the request path.", "/objective_id")
			return
		}
		result, err := projects.CreateObjectiveRevision(r.Context(), s.Human.HumanID, projectID, input, int(expected))
		respond(w, log, http.StatusCreated, result, err)
	})

	handle("GET /v1/projects/{project_id}/objectives/{objective_id}/revisions/{revision}", func(w http.ResponseWriter, r *http.Request, s application.Session) {
		projectID, ok := pathID(w, r, log, "project_id")
		if !ok {
			return
		}
		objectiveID, ok := pathID(w, r, log, "objective_id")
		if !ok {
			return
		}
		revision, ok := pathRevision(w, r, log, "revision")
		if !ok {
			return
		}
		result, err := projects.ObjectiveRevision(r.Context(), s.Human.HumanID, projectID, objectiveID, revision)
		respond(w, log, http.StatusOK, result, err)
	})

	handle("POST /v1/projects/{project_id}/blueprints", func(w http.ResponseWriter, r *http.Request, s application.Session) {
		projectID, ok := pathID(w, r, log, "project_id")
		if !ok {
			return
		}
		input, ok := readDocument[protocol.BlueprintProposal](w, r, log)
		if !ok {
			return
		}
		result, err := projects.ProposeBlueprintRevision(r.Context(), s.Human.HumanID, projectID, input, 0)
		respond(w, log, http.StatusCreated, result, err)
	})

	handle("POST /v1/projects/{project_id}/blueprints/{blueprint_id}/revisions", func(w http.ResponseWriter, r *http.Request, s application.Session) {
		projectID, ok := pathID(w, r, log, "project_id")
		if !ok {
			return
		}
		blueprintID, ok := pathID(w, r, log, "blueprint_id")
		if !ok {
			return
		}
		expected, ok := queryInt(w, r, log, "expected_revision", 1, math.MaxInt32-1, nil)
		if !ok {
			return
		}
		input, ok := readDocument[protocol.BlueprintProposal](w, r, log)
		if !ok {
			return
		}
		if input.BlueprintID != blueprintID {
			writeInvalid(w, log, "The blueprint_id must match the Blueprint in the request path.", "/blueprint_id")
			return
		}
		result, err := projects.ProposeBlueprintRevision(r.Context(), s.Human.HumanID, projectID, input, int(expected))
		respond(w, log, http.StatusCreated, result, err)
	})

	handle("GET /v1/projects/{project_id}/blueprints/{blueprint_id}/revisions/{revision}", func(w http.ResponseWriter, r *http.Request, s application.Session) {
		projectID, ok := pathID(w, r, log, "project_id")
		if !ok {
			return
		}
		blueprintID, ok := pathID(w, r, log, "blueprint_id")
		if !ok {
			return
		}
		revision, ok := pathRevision(w, r, log, "revision")
		if !ok {
			return
		}
		result, err := projects.BlueprintRevision(r.Context(), s.Human.HumanID, projectID, blueprintID, revision)
		respond(w, log, http.StatusOK, result, err)
	})

	decide := func(reject bool) func(http.ResponseWriter, *http.Request, application.Session) {
		return func(w http.ResponseWriter, r *http.Request, s application.Session) {
			projectID, ok := pathID(w, r, log, "project_id")
			if !ok {
				return
			}
			blueprintID, ok := pathID(w, r, log, "blueprint_id")
			if !ok {
				return
			}
			revision, ok := pathRevision(w, r, log, "revision")
			if !ok {
				return
			}
			cellID, ok := pathID(w, r, log, "cell_id")
			if !ok {
				return
			}
			if !reject {
				result, err := projects.AcceptCharter(r.Context(), s.Human.HumanID, projectID, blueprintID, revision, cellID)
				respond(w, log, http.StatusCreated, result, err)
				return
			}
			input, ok := readDocument[protocol.CharterRejection](w, r, log)
			if !ok {
				return
			}
			result, err := projects.RejectCharter(r.Context(), s.Human.HumanID, projectID, blueprintID, revision, cellID, input.Rationale)
			respond(w, log, http.StatusCreated, result, err)
		}
	}
	handle("POST /v1/projects/{project_id}/blueprints/{blueprint_id}/revisions/{revision}/cells/{cell_id}/accept", decide(false))
	handle("POST /v1/projects/{project_id}/blueprints/{blueprint_id}/revisions/{revision}/cells/{cell_id}/reject", decide(true))

	handle("GET /v1/projects/{project_id}/status", func(w http.ResponseWriter, r *http.Request, s application.Session) {
		projectID, ok := pathID(w, r, log, "project_id")
		if !ok {
			return
		}
		result, err := projects.Status(r.Context(), s.Human.HumanID, projectID)
		respond(w, log, http.StatusOK, result, err)
	})

	handle("GET /v1/projects/{project_id}/events", func(w http.ResponseWriter, r *http.Request, s application.Session) {
		projectID, ok := pathID(w, r, log, "project_id")
		if !ok {
			return
		}
		zero, page := int64(0), int64(defaultEventPage)
		after, ok := queryInt(w, r, log, "after", 0, 9007199254740991, &zero)
		if !ok {
			return
		}
		limit, ok := queryInt(w, r, log, "limit", 1, application.MaxEventPage, &page)
		if !ok {
			return
		}
		result, err := projects.Events(r.Context(), s.Human.HumanID, projectID, after, int(limit))
		respondEvents(w, log, result, err)
	})
}

// participantAdapter accepts only Conn's built-in Kiro identifier. It never
// selects a Human or local session reference; those remain session-derived and
// unavailable to HTTP/MCP input. Other clients retain the CLI participant.
func participantAdapter(r *http.Request) string {
	if r.Header.Get("X-Conn-Participant-Adapter") == "kiro" {
		return "kiro"
	}
	return "cli"
}
