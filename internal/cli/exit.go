package cli

import (
	"context"
	"errors"
	"net/http"

	"github.com/Juanqu261/conn/internal/analyzer"
	"github.com/Juanqu261/conn/internal/auth/github"
	"github.com/Juanqu261/conn/internal/credentials"
	"github.com/Juanqu261/conn/internal/httpclient"
)

// Exit codes are a stable public contract for scripts.
const (
	ExitOK          = 0
	ExitFailure     = 1 // unexpected failure, a server error, or a failed doctor check
	ExitUsage       = 2 // invalid command line or input document
	ExitAuth        = 3 // not logged in, session expired or revoked, or credential store unavailable
	ExitAccess      = 4 // the resource does not exist, or you may not read or change it
	ExitConflict    = 5 // stale revision, reused ID, a charter that is already decided, or an objective that already has a blueprint
	ExitUnavailable = 6 // the server is unreachable or not ready
	ExitInterrupted = 130
)

const exitCodeHelp = `Exit codes:
  0    success
  1    unexpected failure, server error, or failed doctor check
  2    invalid command line or input document
  3    not logged in, session expired or revoked, or credential store unavailable
  4    not found, or not permitted in this project
  5    conflict: stale revision, reused ID, a charter already decided, or an
       objective that already has a blueprint
  6    server unreachable or not ready
  130  interrupted`

// usageError marks a problem with the command line or an input file.
type usageError struct{ err error }

func (u usageError) Error() string { return u.err.Error() }
func (u usageError) Unwrap() error { return u.err }

// hinted replaces an error's message with one that names the next step,
// while keeping the original error for exit classification.
type hinted struct {
	message string
	err     error
}

func (h *hinted) Error() string { return h.message }
func (h *hinted) Unwrap() error { return h.err }

func hint(err error, message string) error { return &hinted{message: message, err: err} }

// exitCode classifies err. started reports whether a command's own logic
// ran; Cobra's flag, argument, and command errors happen before it.
func exitCode(err error, started bool) int {
	var usage usageError
	var apiErr *httpclient.APIError
	switch {
	case errors.Is(err, context.Canceled):
		return ExitInterrupted
	case errors.As(err, &usage):
		return ExitUsage
	case errors.Is(err, credentials.ErrNotLoggedIn),
		errors.Is(err, credentials.ErrUnavailable),
		errors.Is(err, credentials.ErrInvalid),
		errors.Is(err, github.ErrAccessDenied),
		errors.Is(err, github.ErrExpired):
		return ExitAuth
	case errors.Is(err, analyzer.ErrInvalid):
		return ExitUsage
	case errors.Is(err, analyzer.ErrUnavailable), errors.Is(err, analyzer.ErrTimedOut), errors.Is(err, httpclient.ErrUnreachable):
		return ExitUnavailable
	case errors.As(err, &apiErr):
		return apiExitCode(apiErr)
	case !started:
		return ExitUsage
	}
	return ExitFailure
}

func apiExitCode(err *httpclient.APIError) int {
	switch err.Code {
	case "unauthenticated", "github_token_rejected":
		return ExitAuth
	case "not_found", "forbidden":
		return ExitAccess
	case "id_conflict", "stale_revision", "charter_decided", "objective_has_blueprint":
		return ExitConflict
	case "invalid_input", "invalid_json", "request_too_large", "unsupported_media_type":
		return ExitUsage
	case "github_unavailable", "login_not_configured", "not_ready":
		return ExitUnavailable
	}
	switch err.Status {
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return ExitUnavailable
	}
	return ExitFailure
}
