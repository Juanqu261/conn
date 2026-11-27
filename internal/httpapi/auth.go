package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/Juanqu261/conn/internal/application"
	"github.com/Juanqu261/conn/internal/credentials"
	"github.com/Juanqu261/conn/internal/protocol"
	"github.com/Juanqu261/conn/internal/secret"
)

// authenticated runs next only for a valid Conn session.
func authenticated(auth *application.Auth, log *slog.Logger, next func(http.ResponseWriter, *http.Request, application.Session)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, ok := bearer(r)
		if ok && isCIToken(token) {
			// CI credentials reach only the routes registered with readable.
			writeError(w, log, http.StatusForbidden, "forbidden", "A CI credential may read only the Conn check routes of its own Project.")
			return
		}
		if !ok || !credentials.ValidToken(token) {
			unauthenticated(w, log)
			return
		}
		session, err := auth.Authenticate(r.Context(), token)
		if errors.Is(err, application.ErrUnauthenticated) {
			unauthenticated(w, log)
			return
		}
		if err != nil {
			internalError(w, log, "authenticate", err)
			return
		}
		setActor(r, session.Human.HumanID)
		next(w, r, session)
	}
}

func unauthenticated(w http.ResponseWriter, log *slog.Logger) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="conn"`)
	writeError(w, log, http.StatusUnauthorized, "unauthenticated",
		"The Conn session is missing, expired, or revoked. Run conn login.")
}

func internalError(w http.ResponseWriter, log *slog.Logger, operation string, err error) {
	log.Error("request failed", "operation", operation, "error", err)
	writeError(w, log, http.StatusInternalServerError, "internal", "The server could not complete the request.")
}

// bearer reads an Authorization: Bearer credential. The value is wrapped
// immediately so it cannot reach a log line.
func bearer(r *http.Request) (secret.String, bool) {
	scheme, value, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || value == "" {
		return secret.String{}, false
	}
	return secret.New(value), true
}

func registerAuthRoutes(mux *http.ServeMux, cfg Config) {
	log := cfg.Log

	mux.HandleFunc("GET /v1/auth/github/client", func(w http.ResponseWriter, _ *http.Request) {
		if cfg.GitHubClientID == "" {
			writeError(w, log, http.StatusServiceUnavailable, "login_not_configured",
				"This Conn server has no GitHub OAuth client configured.")
			return
		}
		writeData(w, log, http.StatusOK, protocol.GitHubLogin{ClientID: cfg.GitHubClientID})
	})

	// The CLI sends the temporary GitHub token once, in the Authorization
	// header. It is verified with GitHub and then discarded.
	mux.HandleFunc("POST /v1/auth/github/exchange", func(w http.ResponseWriter, r *http.Request) {
		githubToken, ok := bearer(r)
		if !ok {
			writeError(w, log, http.StatusUnauthorized, "github_token_required",
				"Send the GitHub access token as a bearer credential.")
			return
		}
		grant, err := cfg.Auth.Exchange(r.Context(), githubToken)
		switch {
		case errors.Is(err, application.ErrGitHubTokenRejected):
			writeError(w, log, http.StatusUnauthorized, "github_token_rejected",
				"GitHub did not accept the access token. Run conn login again.")
		case errors.Is(err, application.ErrGitHubUnavailable):
			log.Warn("GitHub identity verification failed", "error", err)
			writeError(w, log, http.StatusBadGateway, "github_unavailable",
				"GitHub could not be reached to verify the login. Try again.")
		case err != nil:
			internalError(w, log, "exchange", err)
		default:
			log.Info("session issued", "human_id", grant.Human.HumanID)
			writeData(w, log, http.StatusCreated, grant)
		}
	})

	mux.HandleFunc("GET /v1/humans/me", authenticated(cfg.Auth, log, func(w http.ResponseWriter, _ *http.Request, session application.Session) {
		writeData(w, log, http.StatusOK, session.Human)
	}))

	mux.HandleFunc("DELETE /v1/session", authenticated(cfg.Auth, log, func(w http.ResponseWriter, r *http.Request, session application.Session) {
		if err := cfg.Auth.Logout(r.Context(), session); err != nil {
			internalError(w, log, "logout", err)
			return
		}
		log.Info("session revoked", "human_id", session.Human.HumanID)
		w.WriteHeader(http.StatusNoContent)
	}))
}
