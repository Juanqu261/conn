package checkrun

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"uuid"

	"github.com/Juanqu261/conn/internal/protocol"
	"github.com/Juanqu261/conn/internal/repositorygit"
)

// MaxLogBytes bounds the local run log. The output hash covers all output.
const MaxLogBytes = 10 << 20

// RunsDir is the run-log directory under the worktree's Git directory.
const RunsDir = "runs"

// killGrace bounds how long a run waits for output pipes after the command
// exits or is killed.
const killGrace = 5 * time.Second

// Execution describes one check to run in a resolved repository.
type Execution struct {
	// Root is the worktree's top level; GitDir is its Git directory.
	Root   string
	GitDir string
	Key    string
	Check  protocol.CheckDefinition
	// ConfigSHA256 is the hash of the exact configuration bytes.
	ConfigSHA256 string
	// Output receives the combined output as it streams.
	Output io.Writer
	// Environ is the caller's environment. CONN_ variables are removed.
	Environ []string
	Now     func() time.Time
}

// Result is one finished run. LogPath holds at most MaxLogBytes of output.
type Result struct {
	Observation  protocol.EvidenceObservation
	LogPath      string
	LogTruncated bool
}

// Execute runs one check without a shell and records its observation. It
// returns an error only when the run could not start or its facts could not
// be read; a failing or timed-out command is a Result.
func Execute(ctx context.Context, run Execution) (Result, error) {
	now := run.Now
	if now == nil {
		now = time.Now
	}
	directory, err := WorkingDirectory(run.Root, run.Check)
	if err != nil {
		return Result{}, err
	}
	commit, err := repositorygit.Head(ctx, run.Root)
	if err != nil {
		return Result{}, errors.New("the worktree has no HEAD commit to record the run against")
	}
	status, err := repositorygit.Output(ctx, run.Root, 8<<20, "status", "--porcelain", "--untracked-files=normal")
	if err != nil {
		return Result{}, errors.New("git could not report whether the worktree has uncommitted changes")
	}
	dirty := strings.TrimSpace(status) != ""

	runs := filepath.Join(run.GitDir, "conn", RunsDir)
	if err := os.MkdirAll(runs, 0o700); err != nil {
		return Result{}, fmt.Errorf("create the local run-log directory: %w", err)
	}
	logPath := filepath.Join(runs, uuid.NewV7().String()+".log")
	logFile, err := os.OpenFile(logPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return Result{}, fmt.Errorf("create the local run log: %w", err)
	}
	output := &capture{terminal: run.Output, hash: sha256.New(), log: logFile, limit: MaxLogBytes}

	timeout := time.Duration(run.Check.TimeoutSeconds) * time.Second
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, run.Check.Command[0], run.Check.Command[1:]...)
	cmd.Dir, cmd.Env = directory, Environment(run.Environ, run.Check.Env)
	cmd.Stdout, cmd.Stderr, cmd.Stdin = output, output, nil
	cmd.WaitDelay = killGrace
	isolate(cmd)

	started := now()
	runErr := cmd.Run()
	duration := now().Sub(started)
	closeErr := logFile.Close()
	timedOut := errors.Is(runCtx.Err(), context.DeadlineExceeded)
	if ctx.Err() != nil {
		return Result{}, ctx.Err()
	}
	exitCode := int64(0)
	var exitErr *exec.ExitError
	switch {
	case runErr == nil:
	case errors.As(runErr, &exitErr):
		exitCode = int64(exitErr.ExitCode())
	case timedOut || errors.Is(runErr, exec.ErrWaitDelay):
		exitCode = -1
	default:
		_ = os.Remove(logPath)
		return Result{}, &ConfigError{Message: fmt.Sprintf("check %q could not start: %v", run.Key, runErr)}
	}
	// A run Conn stopped reports -1 on every platform; Windows would
	// otherwise report the killed process as exit code 1.
	if timedOut || exitCode < -1 {
		exitCode = -1
	}
	if closeErr != nil {
		return Result{}, fmt.Errorf("write the local run log: %w", closeErr)
	}
	return Result{
		Observation: protocol.EvidenceObservation{
			Method: protocol.ObservationMethodCheck, Runner: protocol.ObservationRunnerLocal, CheckKey: run.Key,
			ConfigSHA256: run.ConfigSHA256, CommandSHA256: CommandSHA256(run.Check), Commit: commit, WorktreeDirty: dirty,
			ExitCode: exitCode, TimedOut: timedOut, DurationMS: max(0, min(duration.Milliseconds(), 86_400_000)),
			OutputSHA256: hex.EncodeToString(output.hash.Sum(nil)), StartedAt: protocol.NewTimestamp(started),
		},
		LogPath: logPath, LogTruncated: output.truncated,
	}, nil
}

// Passed reports whether an observation is passing evidence.
func Passed(observation protocol.EvidenceObservation) bool {
	return observation.ExitCode == 0 && !observation.TimedOut
}

// Environment returns environ without any CONN_ variable, with the
// configured literal values added. Names compare case-insensitively, as
// Windows does.
func Environment(environ []string, configured map[string]string) []string {
	env := make([]string, 0, len(environ)+len(configured))
	for _, item := range environ {
		name, _, _ := strings.Cut(item, "=")
		upper := strings.ToUpper(name)
		if strings.HasPrefix(upper, "CONN_") {
			continue
		}
		if _, overridden := configuredName(configured, upper); overridden {
			continue
		}
		env = append(env, item)
	}
	for _, name := range sortedKeys(configured) {
		env = append(env, name+"="+configured[name])
	}
	return env
}

func configuredName(configured map[string]string, upper string) (string, bool) {
	for name := range configured {
		if strings.ToUpper(name) == upper {
			return name, true
		}
	}
	return "", false
}

// capture streams output to the terminal, hashes all of it, and keeps the
// first limit bytes in the local log. exec serializes writes because Stdout
// and Stderr are the same writer.
type capture struct {
	terminal  io.Writer
	hash      hash.Hash
	log       *os.File
	written   int64
	limit     int64
	truncated bool
}

func (c *capture) Write(data []byte) (int, error) {
	c.hash.Write(data)
	if c.terminal != nil {
		_, _ = c.terminal.Write(data)
	}
	if remaining := c.limit - c.written; remaining > 0 {
		chunk := data
		if int64(len(chunk)) > remaining {
			chunk, c.truncated = chunk[:remaining], true
		}
		n, err := c.log.Write(chunk)
		c.written += int64(n)
		if err != nil {
			return 0, err
		}
	} else if len(data) > 0 {
		c.truncated = true
	}
	return len(data), nil
}
