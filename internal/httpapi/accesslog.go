package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"time"
	"uuid"
)

// requestLog collects the safe facts of one request for its access log line.
type requestLog struct {
	actor        uuid.UUID
	ciCredential uuid.UUID
}

type requestLogKey struct{}

// setActor records the authenticated human for the access log.
func setActor(r *http.Request, human uuid.UUID) {
	if entry, ok := r.Context().Value(requestLogKey{}).(*requestLog); ok {
		entry.actor = human
	}
}

// setCICredential records the authenticated CI credential for the access log.
func setCICredential(r *http.Request, credential uuid.UUID) {
	if entry, ok := r.Context().Value(requestLogKey{}).(*requestLog); ok {
		entry.ciCredential = credential
	}
}

// withAccessLog gives each request a random ID, returned in X-Request-Id,
// and logs one line per request: route pattern, status, duration, and the
// actor and project when known. The ID is for diagnostics only and never
// grants authority. Bodies, query strings, and headers are never logged.
func withAccessLog(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		id := newRequestID()
		w.Header().Set("X-Request-Id", id)
		entry := &requestLog{}
		r = r.WithContext(context.WithValue(r.Context(), requestLogKey{}, entry))
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(recorder, r)

		attrs := []any{
			"request_id", id,
			"method", r.Method,
			"route", r.Pattern,
			"status", recorder.status,
			"duration_ms", time.Since(start).Milliseconds(),
		}
		if entry.actor != (uuid.UUID{}) {
			attrs = append(attrs, "actor_human_id", entry.actor.String())
		}
		if entry.ciCredential != (uuid.UUID{}) {
			attrs = append(attrs, "ci_credential_id", entry.ciCredential.String())
		}
		if project, err := uuid.Parse(r.PathValue("project_id")); err == nil {
			attrs = append(attrs, "project_id", project.String())
		}
		log.Info("request", attrs...)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(status int) {
	s.status = status
	s.ResponseWriter.WriteHeader(status)
}

func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

func newRequestID() string {
	random := make([]byte, 8)
	_, _ = rand.Read(random)
	return hex.EncodeToString(random)
}
