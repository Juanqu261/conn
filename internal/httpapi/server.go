// Package httpapi holds Conn's backend HTTP service built on net/http
package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/Juanqu261/conn/internal/application"
	"github.com/Juanqu261/conn/internal/webui"
)

const (
	shutdownTimeout  = 10 * time.Second
	readinessTimeout = 3 * time.Second
)

// ReadinessCheck reports whether the server can safely serve requests, for
// example whether its database schema matches this build.
type ReadinessCheck func(ctx context.Context) error

// Config holds the backend's dependencies.
type Config struct {
	Ready    ReadinessCheck
	Auth     *application.Auth
	Projects *application.Projects
	// GitHubClientID is the public OAuth client ID served to the CLI. Login
	// is unavailable while it is empty.
	GitHubClientID string
	// Web configures Conn Lab: browser sign-in, the event stream, and the
	// embedded assets (decision 0013).
	Web WebConfig
	Log *slog.Logger
}

// NewHandler returns the backend routes.
//
// GET /healthz reports that the process is running. GET /readyz reports
// whether cfg.Ready succeeds; failure details are logged, never returned.
func NewHandler(cfg Config) http.Handler {
	log := cfg.Log
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeStatus(w, http.StatusOK, "ok")
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), readinessTimeout)
		defer cancel()
		if err := cfg.Ready(ctx); err != nil {
			log.Warn("not ready", "error", err)
			writeStatus(w, http.StatusServiceUnavailable, "not_ready")
			return
		}
		writeStatus(w, http.StatusOK, "ready")
	})
	registerAuthRoutes(mux, cfg)
	registerProjectRoutes(mux, cfg)
	registerVerificationRoutes(mux, cfg)
	registerWebRoutes(mux, cfg)
	mux.HandleFunc("/v1/", func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, log, http.StatusNotFound, "not_found", "No such Conn API route.")
	})
	// Every other path belongs to Conn Lab's single-page application.
	mux.Handle("/", webui.Handler(cfg.Web.Assets))
	return withAccessLog(log, mux)
}
func writeStatus(w http.ResponseWriter, code int, status string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_, _ = fmt.Fprintf(w, "{\"status\":%q}\n", status)
}

// Serve listens on address and serves handler until ctx is cancelled, then
// drains in-flight requests.
func Serve(ctx context.Context, address string, handler http.Handler, log *slog.Logger) error {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", address, err)
	}

	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}

	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	log.Info("conn server listening", "address", listener.Addr().String())

	select {
	case err := <-served:
		return err
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shut down: %w", err)
	}
	if err := <-served; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	log.Info("conn server stopped")
	return nil
}
