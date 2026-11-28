package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Juanqu261/conn/internal/application"
	"github.com/Juanqu261/conn/internal/eventstream"
	"github.com/Juanqu261/conn/internal/protocol"
	"github.com/Juanqu261/conn/internal/secret"
)

const (
	// webSessionCookie holds the Conn Lab browser session token. The
	// __Host- prefix makes browsers require Secure, Path=/, and no Domain,
	// so no sibling subdomain can set or read it.
	webSessionCookie = "__Host-conn_web"
	// webOAuthCookie binds one GitHub authorization to the browser that
	// started it: state, PKCE verifier, and the page to return to.
	webOAuthCookie = "__Host-conn_oauth"
	webOAuthMaxAge = 10 * time.Minute
	csrfHeader     = "X-Conn-CSRF"
	callbackPath   = "/v1/auth/github/web/callback"
	signInPath     = "/sign-in"
	maxReturnPath  = 512
)

// GitHubWebFlow is GitHub's web authorization-code flow with PKCE. The
// production implementation is github.WebFlow; tests use a fake.
type GitHubWebFlow interface {
	AuthorizeURL(state, codeChallenge, redirectURI string) string
	ExchangeCode(ctx context.Context, code string, verifier secret.String, redirectURI string) (secret.String, error)
}

// WebConfig configures Conn Lab (decision 0013). The zero value serves
// the build placeholder, reports sign-in as unavailable, and refuses the
// event stream.
type WebConfig struct {
	// PublicURL is CONN_PUBLIC_URL, the origin users open: scheme and host
	// only. Nil when unset.
	PublicURL *url.URL
	// GitHub is nil while the client ID or secret is missing.
	GitHub GitHubWebFlow
	// MissingSettings names the settings that keep sign-in unavailable.
	MissingSettings []string
	// Assets holds the built Lab, or nil when conn server was built without
	// it.
	Assets fs.FS
	// Streams bounds and wakes browser event streams.
	Streams *eventstream.Hub
	// StreamBacklog bounds the events one stream connect replays before it
	// sends resync instead. Zero selects 1,000.
	StreamBacklog int64
}

func (c WebConfig) signInAvailable() bool { return c.PublicURL != nil && c.GitHub != nil }

// ParsePublicURL validates CONN_PUBLIC_URL: an http or https origin with no
// path, query, fragment, or user information. Plain http is accepted only
// for loopback hosts, as for CONN_SERVER_URL, because browser session
// cookies must never cross the network unencrypted.
func ParsePublicURL(value string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSuffix(value, "/"))
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.Path != "" ||
		parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Opaque != "" {
		return nil, errors.New("CONN_PUBLIC_URL must be an origin such as https://conn.example.com or http://127.0.0.1:8080")
	}
	switch parsed.Scheme {
	case "https":
	case "http":
		host := parsed.Hostname()
		if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
			return nil, errors.New("CONN_PUBLIC_URL may use plain http only for a loopback address; use https")
		}
	default:
		return nil, errors.New("CONN_PUBLIC_URL must use https, or http on a loopback address")
	}
	return &url.URL{Scheme: parsed.Scheme, Host: strings.ToLower(parsed.Host)}, nil
}

func (c WebConfig) origin() string {
	if c.PublicURL == nil {
		return ""
	}
	return c.PublicURL.Scheme + "://" + c.PublicURL.Host
}

func registerWebRoutes(mux *http.ServeMux, cfg Config) {
	log, web := cfg.Log, cfg.Web

	mux.HandleFunc("GET /v1/web/lab", func(w http.ResponseWriter, _ *http.Request) {
		status := protocol.WebLabStatus{
			LabBuilt:        web.Assets != nil,
			SignInAvailable: web.signInAvailable(),
			MissingSettings: append([]string{}, web.MissingSettings...),
			LabURL:          web.origin(),
		}
		writeData(w, log, http.StatusOK, status)
	})

	mux.HandleFunc("GET /v1/auth/github/web/start", func(w http.ResponseWriter, r *http.Request) {
		noStore(w)
		if !web.signInAvailable() {
			redirectToSignIn(w, r, "not_configured")
			return
		}
		state, err := randomToken()
		if err != nil {
			internalError(w, log, "web sign-in start", err)
			return
		}
		verifier, err := randomToken()
		if err != nil {
			internalError(w, log, "web sign-in start", err)
			return
		}
		returnTo := safeReturnPath(r.URL.Query().Get("return_to"))
		http.SetCookie(w, &http.Cookie{
			Name:     webOAuthCookie,
			Value:    state + "." + verifier + "." + base64.RawURLEncoding.EncodeToString([]byte(returnTo)),
			Path:     "/",
			MaxAge:   int(webOAuthMaxAge.Seconds()),
			HttpOnly: true,
			Secure:   true,
			// The callback is a top-level navigation from GitHub, which Lax
			// cookies accompany.
			SameSite: http.SameSiteLaxMode,
		})
		challenge := sha256.Sum256([]byte(verifier))
		http.Redirect(w, r, web.GitHub.AuthorizeURL(state, base64.RawURLEncoding.EncodeToString(challenge[:]), web.origin()+callbackPath), http.StatusFound)
	})

	mux.HandleFunc("GET "+callbackPath, func(w http.ResponseWriter, r *http.Request) {
		noStore(w)
		if !web.signInAvailable() {
			redirectToSignIn(w, r, "not_configured")
			return
		}
		state, verifier, returnTo, ok := readOAuthCookie(r)
		clearCookie(w, webOAuthCookie)
		query := r.URL.Query()
		if !ok || subtle.ConstantTimeCompare([]byte(state), []byte(query.Get("state"))) != 1 {
			redirectToSignIn(w, r, "state_mismatch")
			return
		}
		if query.Get("error") != "" {
			code := "failed"
			if query.Get("error") == "access_denied" {
				code = "denied"
			}
			redirectToSignIn(w, r, code)
			return
		}
		code := query.Get("code")
		if code == "" || len(code) > 512 {
			redirectToSignIn(w, r, "failed")
			return
		}
		githubToken, err := web.GitHub.ExchangeCode(r.Context(), code, secret.New(verifier), web.origin()+callbackPath)
		if err != nil {
			log.Warn("GitHub web authorization failed", "error", err)
			redirectToSignIn(w, r, "code_rejected")
			return
		}
		grant, err := cfg.Auth.ExchangeBrowser(r.Context(), githubToken)
		switch {
		case errors.Is(err, application.ErrGitHubTokenRejected):
			redirectToSignIn(w, r, "code_rejected")
			return
		case errors.Is(err, application.ErrGitHubUnavailable):
			log.Warn("GitHub identity verification failed", "error", err)
			redirectToSignIn(w, r, "github_unavailable")
			return
		case err != nil:
			log.Error("request failed", "operation", "web sign-in", "error", err)
			redirectToSignIn(w, r, "failed")
			return
		}
		http.SetCookie(w, &http.Cookie{
			Name:     webSessionCookie,
			Value:    grant.Token.Reveal(),
			Path:     "/",
			MaxAge:   max(1, int(time.Until(grant.ExpiresAt).Seconds())),
			HttpOnly: true,
			Secure:   true,
			SameSite: http.SameSiteLaxMode,
		})
		log.Info("browser session issued", "human_id", grant.Human.HumanID)
		http.Redirect(w, r, returnTo, http.StatusFound)
	})

	mux.HandleFunc("GET /v1/web/session", func(w http.ResponseWriter, r *http.Request) {
		session, token, ok := browserSession(w, r, cfg.Auth, log)
		if !ok {
			return
		}
		writeData(w, log, http.StatusOK, protocol.WebSession{
			Human:     session.Human,
			CSRFToken: application.CSRFToken(token),
			ExpiresAt: protocol.NewTimestamp(session.ExpiresAt),
		})
	})

	mux.HandleFunc("DELETE /v1/web/session", func(w http.ResponseWriter, r *http.Request) {
		token, found := browserSessionCookie(r)
		if !found {
			browserUnauthenticated(w, log)
			return
		}
		// The origin and CSRF checks come before any lookup or write, so a
		// cross-site request has no effect at all.
		if !sameOrigin(r, web) || !application.ValidCSRF(token, r.Header.Get(csrfHeader)) {
			csrfRefused(w, log)
			return
		}
		session, err := cfg.Auth.AuthenticateBrowser(r.Context(), token)
		if errors.Is(err, application.ErrUnauthenticated) {
			clearCookie(w, webSessionCookie)
			browserUnauthenticated(w, log)
			return
		}
		if err != nil {
			internalError(w, log, "authenticate", err)
			return
		}
		if err := cfg.Auth.Logout(r.Context(), session); err != nil {
			internalError(w, log, "logout", err)
			return
		}
		setActor(r, session.Human.HumanID)
		clearCookie(w, webSessionCookie)
		log.Info("browser session revoked", "human_id", session.Human.HumanID)
		w.WriteHeader(http.StatusNoContent)
	})

	registerStreamRoute(mux, cfg)
}

// browserSession authenticates the session cookie and returns the session
// with its raw token, or writes 401.
func browserSession(w http.ResponseWriter, r *http.Request, auth *application.Auth, log *slog.Logger) (application.Session, secret.String, bool) {
	token, found := browserSessionCookie(r)
	if !found {
		browserUnauthenticated(w, log)
		return application.Session{}, secret.String{}, false
	}
	session, err := auth.AuthenticateBrowser(r.Context(), token)
	if errors.Is(err, application.ErrUnauthenticated) {
		clearCookie(w, webSessionCookie)
		browserUnauthenticated(w, log)
		return application.Session{}, secret.String{}, false
	}
	if err != nil {
		internalError(w, log, "authenticate", err)
		return application.Session{}, secret.String{}, false
	}
	setActor(r, session.Human.HumanID)
	return session, token, true
}

// browserSessionCookie reads the browser session cookie. The value is
// wrapped immediately so it cannot reach a log line.
func browserSessionCookie(r *http.Request) (secret.String, bool) {
	cookie, err := r.Cookie(webSessionCookie)
	if err != nil || cookie.Value == "" {
		return secret.String{}, false
	}
	token := secret.New(cookie.Value)
	return token, application.IsBrowserToken(token)
}

// sameOrigin requires the Origin header to equal CONN_PUBLIC_URL exactly.
func sameOrigin(r *http.Request, web WebConfig) bool {
	origin := web.origin()
	return origin != "" && r.Header.Get("Origin") == origin
}

func readOAuthCookie(r *http.Request) (state, verifier, returnTo string, ok bool) {
	cookie, err := r.Cookie(webOAuthCookie)
	if err != nil {
		return "", "", "", false
	}
	parts := strings.Split(cookie.Value, ".")
	if len(parts) != 3 || len(parts[0]) != 43 || len(parts[1]) != 43 {
		return "", "", "", false
	}
	path, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return "", "", "", false
	}
	return parts[0], parts[1], safeReturnPath(string(path)), true
}

// safeReturnPath keeps a post-sign-in redirect on this origin and off the
// API: a rooted path that is not protocol-relative, has no backslash or
// control character, and does not start with /v1/.
func safeReturnPath(path string) string {
	if path == "" || len(path) > maxReturnPath || path[0] != '/' ||
		strings.HasPrefix(path, "//") || strings.ContainsAny(path, "\\\r\n\t") ||
		strings.HasPrefix(path, "/v1/") || path == "/v1" {
		return "/"
	}
	for _, r := range path {
		if r < 0x20 || r == 0x7f {
			return "/"
		}
	}
	return path
}

func redirectToSignIn(w http.ResponseWriter, r *http.Request, code string) {
	http.Redirect(w, r, signInPath+"?error="+url.QueryEscape(code), http.StatusFound)
}

func clearCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
}

func noStore(w http.ResponseWriter) { w.Header().Set("Cache-Control", "no-store") }

func browserUnauthenticated(w http.ResponseWriter, log *slog.Logger) {
	writeError(w, log, http.StatusUnauthorized, "unauthenticated",
		"The Conn Lab session is missing, expired, or revoked. Sign in again.")
}

func csrfRefused(w http.ResponseWriter, log *slog.Logger) {
	writeError(w, log, http.StatusForbidden, "csrf_refused",
		"The request did not come from Conn Lab. Reload the page and try again.")
}

func randomToken() (string, error) {
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(random), nil
}
