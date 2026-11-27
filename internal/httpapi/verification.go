package httpapi

import (
	"errors"
	"math"
	"net/http"
	"regexp"
	"strings"

	"uuid"

	"github.com/Juanqu261/conn/internal/application"
	"github.com/Juanqu261/conn/internal/protocol"
	"github.com/Juanqu261/conn/internal/secret"
)

var ciTokenPattern = regexp.MustCompile(`^conn_ci_[A-Za-z0-9_-]{43}$`)

// isCIToken reports whether a bearer credential claims to be a CI token.
func isCIToken(token secret.String) bool {
	return strings.HasPrefix(token.Reveal(), protocol.CITokenPrefix)
}

func ciRouteForbidden(w http.ResponseWriter, cfg Config) {
	writeError(w, cfg.Log, http.StatusForbidden, "forbidden", "A CI credential may read only the Conn check routes of its own Project.")
}

// authenticateCI resolves a CI token, writing 401 for an unknown, expired,
// or revoked one.
func authenticateCI(w http.ResponseWriter, r *http.Request, cfg Config, token secret.String) (application.CIPrincipal, bool) {
	if !ciTokenPattern.MatchString(token.Reveal()) {
		ciUnauthenticated(w, cfg)
		return application.CIPrincipal{}, false
	}
	principal, err := cfg.Projects.AuthenticateCI(r.Context(), token)
	if errors.Is(err, application.ErrUnauthenticated) {
		ciUnauthenticated(w, cfg)
		return application.CIPrincipal{}, false
	}
	if err != nil {
		internalError(w, cfg.Log, "authenticate CI credential", err)
		return application.CIPrincipal{}, false
	}
	setCICredential(r, principal.CredentialID)
	return principal, true
}

func ciUnauthenticated(w http.ResponseWriter, cfg Config) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="conn"`)
	writeError(w, cfg.Log, http.StatusUnauthorized, "unauthenticated",
		"The Conn CI credential is missing, expired, or revoked. Ask a Project owner to run conn ci credential create.")
}

// readable serves an allowlisted read route to a member session or to a CI
// credential whose Project is the route's {project_id}. A CI credential for
// another Project is refused without revealing whether that Project exists.
func readable(cfg Config, next func(http.ResponseWriter, *http.Request, application.Reader)) http.HandlerFunc {
	session := authenticated(cfg.Auth, cfg.Log, func(w http.ResponseWriter, r *http.Request, s application.Session) {
		next(w, r, application.HumanReader(s.Human.HumanID))
	})
	return func(w http.ResponseWriter, r *http.Request) {
		token, ok := bearer(r)
		if !ok || !isCIToken(token) {
			session(w, r)
			return
		}
		principal, ok := authenticateCI(w, r, cfg, token)
		if !ok {
			return
		}
		projectID, err := uuid.Parse(r.PathValue("project_id"))
		if err != nil || projectID != principal.ProjectID {
			ciRouteForbidden(w, cfg)
			return
		}
		next(w, r, application.CIReader(principal))
	}
}

// agentSurface reports a request from an MCP adapter, which may never
// create credentials.
func agentSurface(r *http.Request) bool {
	adapter := participantAdapter(r)
	return adapter == "kiro" || adapter == "mcp"
}

func registerVerificationRoutes(mux *http.ServeMux, cfg Config) {
	log, projects := cfg.Log, cfg.Projects
	handle := func(pattern string, h func(http.ResponseWriter, *http.Request, application.Session)) {
		mux.HandleFunc(pattern, authenticated(cfg.Auth, log, h))
	}

	mux.HandleFunc("GET /v1/ci/credential", func(w http.ResponseWriter, r *http.Request) {
		token, ok := bearer(r)
		if !ok {
			ciUnauthenticated(w, cfg)
			return
		}
		if !isCIToken(token) {
			writeError(w, log, http.StatusForbidden, "forbidden", "This route requires a Conn CI credential.")
			return
		}
		principal, ok := authenticateCI(w, r, cfg, token)
		if !ok {
			return
		}
		result, err := projects.CICredentialContext(r.Context(), principal)
		respond(w, log, http.StatusOK, result, err)
	})

	mux.HandleFunc("POST /v1/projects/{project_id}/collision-previews", readable(cfg, func(w http.ResponseWriter, r *http.Request, reader application.Reader) {
		projectID, ok := pathID(w, r, log, "project_id")
		if !ok {
			return
		}
		input, ok := readDocument[protocol.CollisionPreviewInput](w, r, log)
		if !ok {
			return
		}
		result, err := projects.PreviewCollisions(r.Context(), reader, projectID, input)
		respond(w, log, http.StatusOK, result, err)
	}))

	handle("POST /v1/projects/{project_id}/collisions/{collision_id}/combination-checks", func(w http.ResponseWriter, r *http.Request, s application.Session) {
		projectID, ok := pathID(w, r, log, "project_id")
		if !ok {
			return
		}
		collisionID, ok := pathID(w, r, log, "collision_id")
		if !ok {
			return
		}
		input, ok := readDocument[protocol.CombinationCheckInput](w, r, log)
		if !ok {
			return
		}
		participant, err := projects.ResolveParticipant(r.Context(), s.Human.HumanID, projectID, participantAdapter(r), "")
		if err != nil {
			writeProblem(w, log, err)
			return
		}
		result, err := projects.RecordCombinationCheck(r.Context(), s.Human.HumanID, projectID, participant.Data.ParticipantID, collisionID, input)
		respond(w, log, http.StatusCreated, result, err)
	})

	handle("POST /v1/projects/{project_id}/ci-credentials", func(w http.ResponseWriter, r *http.Request, s application.Session) {
		projectID, ok := pathID(w, r, log, "project_id")
		if !ok {
			return
		}
		input, ok := readDocument[protocol.CICredentialCreate](w, r, log)
		if !ok {
			return
		}
		result, err := projects.CreateCICredential(r.Context(), s.Human.HumanID, projectID, input, agentSurface(r))
		if err == nil {
			log.Info("CI credential created", "credential_id", result.Data.Credential.CredentialID, "project_id", projectID)
		}
		respond(w, log, http.StatusCreated, result, err)
	})

	handle("GET /v1/projects/{project_id}/ci-credentials", func(w http.ResponseWriter, r *http.Request, s application.Session) {
		projectID, ok := pathID(w, r, log, "project_id")
		if !ok {
			return
		}
		result, err := projects.CICredentials(r.Context(), s.Human.HumanID, projectID)
		respond(w, log, http.StatusOK, result, err)
	})

	handle("POST /v1/projects/{project_id}/ci-credentials/{credential_id}/revoke", func(w http.ResponseWriter, r *http.Request, s application.Session) {
		projectID, ok := pathID(w, r, log, "project_id")
		if !ok {
			return
		}
		credentialID, ok := pathID(w, r, log, "credential_id")
		if !ok {
			return
		}
		result, err := projects.RevokeCICredential(r.Context(), s.Human.HumanID, projectID, credentialID)
		respond(w, log, http.StatusOK, result, err)
	})

	handle("GET /v1/projects/{project_id}/insights", func(w http.ResponseWriter, r *http.Request, s application.Session) {
		projectID, ok := pathID(w, r, log, "project_id")
		if !ok || !allowQueryParameters(w, r, log, "since_days") {
			return
		}
		fallback := int64(protocol.DefaultInsightDays)
		days, ok := queryInt(w, r, log, "since_days", 1, protocol.MaxInsightDays, &fallback)
		if !ok {
			return
		}
		result, err := projects.Insights(r.Context(), s.Human.HumanID, projectID, int(days))
		respond(w, log, http.StatusOK, result, err)
	})

	handle("POST /v1/projects/{project_id}/objective-imports/github-issue", func(w http.ResponseWriter, r *http.Request, s application.Session) {
		projectID, ok := pathID(w, r, log, "project_id")
		if !ok {
			return
		}
		zero := int64(0)
		expected, ok := queryInt(w, r, log, "expected_revision", 0, math.MaxInt32-1, &zero)
		if !ok {
			return
		}
		input, ok := readDocument[protocol.ObjectiveImportInput](w, r, log)
		if !ok {
			return
		}
		result, err := projects.ImportObjective(r.Context(), s.Human.HumanID, projectID, application.SourceGitHubIssue, input, int(expected))
		respond(w, log, http.StatusCreated, result, err)
	})
}
