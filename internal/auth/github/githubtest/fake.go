// Package githubtest is a fake GitHub for device-flow and identity tests.
// No test makes live GitHub calls (decision 0032).
package githubtest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Juanqu261/conn/internal/auth/github"
)

// Poll is one scripted access-token response.
type Poll struct {
	Error    string // empty for success
	Interval int    // seconds, sent with slow_down
}

// Server is a fake github.com and api.github.com.
type Server struct {
	ClientID  string
	ExpiresIn int // seconds; 900 when zero
	Interval  int // seconds; 5 when zero
	// Polls are answered in order. After the script ends, polls succeed.
	Polls []Poll
	// Token is issued on success. Users maps tokens to identities.
	Token string
	Users map[string]github.Identity

	mu        sync.Mutex
	pollCount int
	server    *httptest.Server
}

// Start runs the fake and stops it when the test ends.
func (s *Server) Start(t testing.TB) github.Endpoints {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /login/device/code", s.deviceCode)
	mux.HandleFunc("POST /login/oauth/access_token", s.accessToken)
	mux.HandleFunc("GET /user", s.user)
	s.server = httptest.NewServer(mux)
	t.Cleanup(s.server.Close)
	return github.Endpoints{Web: s.server.URL, API: s.server.URL}
}

// PollCount reports how many access-token polls were received.
func (s *Server) PollCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pollCount
}

func (s *Server) deviceCode(w http.ResponseWriter, r *http.Request) {
	if r.PostFormValue("client_id") != s.ClientID {
		writeJSON(w, map[string]any{"error": "incorrect_client_credentials"})
		return
	}
	expires, interval := s.ExpiresIn, s.Interval
	if expires == 0 {
		expires = 900
	}
	if interval == 0 {
		interval = 5
	}
	writeJSON(w, map[string]any{
		"device_code":      "fake-device-code",
		"user_code":        "WDJB-MJHT",
		"verification_uri": "https://github.com/login/device",
		"expires_in":       expires,
		"interval":         interval,
	})
}

func (s *Server) accessToken(w http.ResponseWriter, r *http.Request) {
	if r.PostFormValue("client_id") != s.ClientID ||
		r.PostFormValue("device_code") != "fake-device-code" ||
		r.PostFormValue("grant_type") != "urn:ietf:params:oauth:grant-type:device_code" {
		writeJSON(w, map[string]any{"error": "incorrect_device_code"})
		return
	}
	s.mu.Lock()
	n := s.pollCount
	s.pollCount++
	s.mu.Unlock()

	if n < len(s.Polls) {
		poll := s.Polls[n]
		body := map[string]any{"error": poll.Error}
		if poll.Interval > 0 {
			body["interval"] = poll.Interval
		}
		writeJSON(w, body)
		return
	}
	writeJSON(w, map[string]any{"access_token": s.Token, "token_type": "bearer", "scope": ""})
}

func (s *Server) user(w http.ResponseWriter, r *http.Request) {
	identity, ok := s.Users[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		writeJSON(w, map[string]any{"message": "Bad credentials"})
		return
	}
	writeJSON(w, map[string]any{"id": identity.UserID, "login": identity.Login})
}

func writeJSON(w http.ResponseWriter, body any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}
