// Package analyzer runs bounded, local source analyzers. It deliberately has no
// HTTP, database, or Conn credential dependency.
package analyzer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Juanqu261/conn/internal/protocol"
)

const (
	MaxInputBytes  = 64 << 10
	MaxOutputBytes = 1 << 20
	MaxStderrBytes = 16 << 10
	DefaultTimeout = 30 * time.Second
)

var (
	ErrUnavailable = errors.New("local TypeScript analyzer is unavailable")
	ErrTimedOut    = errors.New("local TypeScript analyzer timed out")
	ErrInvalid     = errors.New("local TypeScript analyzer returned an invalid report")
)

// Request is the source-free, credential-free subprocess input contract.
// Planned requests name the paths a participant intends to change; observed
// requests compare BaseRevisionRef with the worktree. BaseRevisionRef is sent
// only in observed mode and Symbols only in planned mode.
type Request struct {
	RequestVersion  int                 `json:"request_version"`
	Mode            string              `json:"mode"`
	Worktree        string              `json:"worktree"`
	CanonicalRemote string              `json:"canonical_remote"`
	TSConfigPath    string              `json:"tsconfig_path,omitempty"`
	Paths           []string            `json:"paths,omitempty"`
	Symbols         map[string][]string `json:"symbols,omitempty"`
	BaseRevisionRef string              `json:"base_revision_ref,omitempty"`
}

// Planned and observed request bounds.
const (
	MaxPlannedPaths  = 50
	MaxObservedPaths = 500
)

// Config locates the Node executable and built analyzer program.
type Config struct {
	NodePath     string
	AnalyzerPath string
	Timeout      time.Duration
}

// Run starts one local Node analyzer and validates its sole JSON response.
// It supplies a minimal environment which intentionally excludes every CONN_
// variable, including Conn session tokens and database credentials.
func Run(ctx context.Context, cfg Config, request Request) (protocol.LocalAnalyzerReport, error) {
	if cfg.NodePath == "" || cfg.AnalyzerPath == "" {
		return protocol.LocalAnalyzerReport{}, fmt.Errorf("%w: configure Node.js and the built TypeScript analyzer", ErrUnavailable)
	}
	if request.Worktree == "" || request.CanonicalRemote == "" {
		return protocol.LocalAnalyzerReport{}, fmt.Errorf("%w: worktree and canonical remote are required", ErrInvalid)
	}
	// The child's stderr is never surfaced, so diagnose the most common
	// setup failure here rather than as an opaque process failure.
	if info, err := os.Stat(cfg.AnalyzerPath); err != nil || info.IsDir() {
		return protocol.LocalAnalyzerReport{}, fmt.Errorf("%w: analyzer script %q was not found; build it with `pnpm build`, set %s, or pass --analyzer-script", ErrUnavailable, cfg.AnalyzerPath, AnalyzerPathEnv)
	}
	request.RequestVersion = protocol.AnalyzerReportVersion
	switch request.Mode {
	case protocol.AnalyzerModePlanned:
		if len(request.Paths) == 0 || len(request.Paths) > MaxPlannedPaths {
			return protocol.LocalAnalyzerReport{}, fmt.Errorf("%w: planned analysis needs 1 to %d paths", ErrInvalid, MaxPlannedPaths)
		}
		request.BaseRevisionRef = ""
	case protocol.AnalyzerModeObserved:
		if len(request.Paths) > MaxObservedPaths {
			return protocol.LocalAnalyzerReport{}, fmt.Errorf("%w: observed analysis accepts at most %d paths", ErrInvalid, MaxObservedPaths)
		}
		if request.BaseRevisionRef != "" && !ValidRevisionRef(request.BaseRevisionRef) {
			return protocol.LocalAnalyzerReport{}, fmt.Errorf("%w: the base revision reference is not a commit or safe ref name", ErrInvalid)
		}
		request.Symbols = nil
	default:
		return protocol.LocalAnalyzerReport{}, fmt.Errorf("%w: mode must be planned or observed", ErrInvalid)
	}
	input, err := json.Marshal(request)
	if err != nil || len(input) > MaxInputBytes {
		return protocol.LocalAnalyzerReport{}, fmt.Errorf("%w: request exceeds %d bytes", ErrInvalid, MaxInputBytes)
	}
	worktree, err := filepath.Abs(request.Worktree)
	if err != nil {
		return protocol.LocalAnalyzerReport{}, fmt.Errorf("%w: resolve worktree", ErrInvalid)
	}
	request.Worktree = worktree
	if input, err = json.Marshal(request); err != nil || len(input) > MaxInputBytes {
		return protocol.LocalAnalyzerReport{}, fmt.Errorf("%w: request exceeds %d bytes", ErrInvalid, MaxInputBytes)
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	childCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(childCtx, cfg.NodePath, cfg.AnalyzerPath)
	cmd.Dir = worktree
	cmd.Env = localEnvironment()
	cmd.Stdin = bytes.NewReader(input)
	var stdout, stderr limitedBuffer
	stdout.limit, stderr.limit = MaxOutputBytes, MaxStderrBytes
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		switch {
		case errors.Is(ctx.Err(), context.Canceled):
			return protocol.LocalAnalyzerReport{}, ctx.Err()
		case errors.Is(childCtx.Err(), context.DeadlineExceeded):
			return protocol.LocalAnalyzerReport{}, ErrTimedOut
		case errors.Is(err, exec.ErrNotFound):
			return protocol.LocalAnalyzerReport{}, fmt.Errorf("%w: Node.js executable %q was not found", ErrUnavailable, cfg.NodePath)
		default:
			// stderr can contain repository data. Direct the user to local stderr
			// without returning it through Conn or persisting it.
			return protocol.LocalAnalyzerReport{}, fmt.Errorf("%w: process failed; inspect the local analyzer installation", ErrUnavailable)
		}
	}
	if stdout.exceeded {
		return protocol.LocalAnalyzerReport{}, fmt.Errorf("%w: report exceeds %d bytes", ErrInvalid, MaxOutputBytes)
	}
	report, err := protocol.Decode[protocol.LocalAnalyzerReport](stdout.Bytes())
	if err != nil {
		return protocol.LocalAnalyzerReport{}, fmt.Errorf("%w: report does not match Conn's local-analyzer-report contract", ErrInvalid)
	}
	if err := ValidateReport(report); err != nil {
		return protocol.LocalAnalyzerReport{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if report.Mode != request.Mode {
		return protocol.LocalAnalyzerReport{}, fmt.Errorf("%w: report mode %q does not match the requested mode", ErrInvalid, report.Mode)
	}
	return report, nil
}

// HasDiagnostic reports whether report carries the diagnostic code.
func HasDiagnostic(report protocol.LocalAnalyzerReport, code string) bool {
	for _, diagnostic := range report.Diagnostics {
		if diagnostic.Code == code {
			return true
		}
	}
	return false
}

// Diagnostic codes the Go adapter acts on.
const (
	DiagnosticBaseUnavailable = "conn-base-unavailable"
	DiagnosticPlannedPathNew  = "conn-planned-path-new"
)

var (
	commitPattern  = regexp.MustCompile(`^[0-9a-f]{7,64}$`)
	refNamePattern = regexp.MustCompile(`^[A-Za-z0-9._/-]{1,256}$`)
)

// ValidRevisionRef accepts an abbreviated or full commit ID, or a ref name
// that cannot be read as a Git option.
func ValidRevisionRef(ref string) bool {
	return commitPattern.MatchString(ref) || (refNamePattern.MatchString(ref) && !strings.HasPrefix(ref, "-") && !strings.Contains(ref, ".."))
}

func localEnvironment() []string {
	// HOME and USERPROFILE let the analyzer's read-only Git calls find the
	// user's safe.directory configuration. No CONN_ variable is ever passed.
	allow := map[string]bool{"PATH": true, "SYSTEMROOT": true, "COMSPEC": true, "TEMP": true, "TMP": true, "HOME": true, "USERPROFILE": true}
	env := make([]string, 0, len(allow))
	for _, item := range os.Environ() {
		key, _, _ := strings.Cut(item, "=")
		if allow[strings.ToUpper(key)] {
			env = append(env, item)
		}
	}
	return env
}

// ValidateReport enforces the source-free, secret-free subset of the canonical
// schema. HTTP callers use it too, so bypassing the local CLI cannot persist a
// credential-shaped analyzer report.
func ValidateReport(report protocol.LocalAnalyzerReport) error {
	if report.ReportVersion != protocol.AnalyzerReportVersion || len(report.Claims) > 500 || len(report.Diagnostics) > 100 {
		return errors.New("report exceeds a fixed contract bound")
	}
	if report.Mode != protocol.AnalyzerModePlanned && report.Mode != protocol.AnalyzerModeObserved {
		return errors.New("report mode is not planned or observed")
	}
	for _, claim := range report.Claims {
		if claim.ProvenanceKind != "static_analysis" || hasSensitiveValue(claim.DisplayName) || hasSensitiveValue(claim.NormalizedSubject) {
			return errors.New("report contains disallowed claim data")
		}
		for _, evidence := range claim.EvidenceRefs {
			if hasSensitiveValue(evidence.Locator) || hasSensitiveValue(evidence.Path) {
				return errors.New("report contains credential-shaped evidence")
			}
		}
	}
	for _, diagnostic := range report.Diagnostics {
		if len(diagnostic.Message) > 1000 || hasSensitiveValue(diagnostic.Message) || hasSensitiveValue(diagnostic.Path) {
			return errors.New("report contains unsafe diagnostic data")
		}
	}
	return nil
}

func hasSensitiveValue(value string) bool {
	lower := strings.ToLower(value)
	return strings.Contains(lower, "conn_sess_") || strings.Contains(lower, "ghp_") ||
		strings.Contains(lower, "github_pat_") || strings.Contains(lower, "bearer ") ||
		strings.Contains(lower, "password=") || strings.Contains(lower, "secret=") ||
		strings.Contains(lower, "prompt:") || strings.Contains(lower, "source_text")
}

type limitedBuffer struct {
	bytes.Buffer
	limit    int
	exceeded bool
}

func (b *limitedBuffer) Write(data []byte) (int, error) {
	remaining := b.limit - b.Len()
	if remaining <= 0 {
		b.exceeded = true
		return len(data), nil
	}
	if len(data) > remaining {
		_, _ = b.Buffer.Write(data[:remaining])
		b.exceeded = true
		return len(data), nil
	}
	return b.Buffer.Write(data)
}

var _ io.Writer = (*limitedBuffer)(nil)
