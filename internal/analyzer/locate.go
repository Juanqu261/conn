package analyzer

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Juanqu261/conn/internal/envvalue"
)

// Environment variables that override analyzer and Node.js resolution.
const (
	AnalyzerPathEnv = "CONN_ANALYZER_PATH"
	NodePathEnv     = "CONN_NODE_PATH"
)

// Sources of the resolved analyzer and Node.js executables.
const (
	SourceAnalyzerEnv     = "CONN_ANALYZER_PATH"
	SourceExecutableDir   = "executable_directory"
	SourceWorkingDir      = "working_directory"
	SourceNodeEnv         = "CONN_NODE_PATH"
	SourceNodePath        = "PATH"
	relativeAnalyzerPath  = "packages/typescript-analyzer/dist/cli.js"
	nodeVersionProbeLimit = 2 * time.Second
)

// Location is the resolved local analyzer configuration. It never contains
// a credential and is safe to report in diagnostics.
type Location struct {
	Config            Config
	AnalyzerSource    string
	AnalyzerAvailable bool
	NodeSource        string
	NodeAvailable     bool
}

// Available reports whether both the analyzer script and Node.js were found.
func (l Location) Available() bool { return l.AnalyzerAvailable && l.NodeAvailable }

// Locate resolves the analyzer from CONN_ANALYZER_PATH, then relative to the
// conn executable, then relative to the working directory, and Node.js from
// CONN_NODE_PATH, then PATH. An explicit environment value always wins, even
// when it names a missing file, so misconfiguration is reported instead of
// silently replaced.
func Locate(lookupEnv func(string) (string, bool), executable, workingDir string) Location {
	location := Location{Config: Config{Timeout: DefaultTimeout}}
	if value, ok := envvalue.Lookup(lookupEnv, AnalyzerPathEnv); ok {
		location.Config.AnalyzerPath, location.AnalyzerSource = absolute(value), SourceAnalyzerEnv
		location.AnalyzerAvailable = isFile(location.Config.AnalyzerPath)
	} else {
		candidates := []struct{ dir, source string }{}
		if executable != "" {
			if resolved, err := filepath.EvalSymlinks(executable); err == nil {
				executable = resolved
			}
			candidates = append(candidates, struct{ dir, source string }{filepath.Dir(executable), SourceExecutableDir})
		}
		if workingDir != "" {
			candidates = append(candidates, struct{ dir, source string }{workingDir, SourceWorkingDir})
		}
		for _, candidate := range candidates {
			path := filepath.Join(candidate.dir, filepath.FromSlash(relativeAnalyzerPath))
			if isFile(path) {
				location.Config.AnalyzerPath, location.AnalyzerSource, location.AnalyzerAvailable = path, candidate.source, true
				break
			}
		}
		if !location.AnalyzerAvailable && workingDir != "" {
			location.Config.AnalyzerPath = filepath.Join(workingDir, filepath.FromSlash(relativeAnalyzerPath))
		}
	}
	if value, ok := envvalue.Lookup(lookupEnv, NodePathEnv); ok {
		location.Config.NodePath, location.NodeSource = absolute(value), SourceNodeEnv
		location.NodeAvailable = isFile(location.Config.NodePath)
	} else if path, err := exec.LookPath("node"); err == nil {
		location.Config.NodePath, location.NodeSource, location.NodeAvailable = path, SourceNodePath, true
	} else {
		location.Config.NodePath = "node"
	}
	return location
}

// LocateProcess resolves the analyzer for the running conn process.
func LocateProcess() Location {
	executable, _ := os.Executable()
	workingDir, _ := os.Getwd()
	return Locate(os.LookupEnv, executable, workingDir)
}

// NodeVersion runs `node --version` with a short timeout and the analyzer's
// minimal environment. It returns an empty string when Node.js is unusable.
func NodeVersion(ctx context.Context, nodePath string) string {
	if nodePath == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, nodeVersionProbeLimit)
	defer cancel()
	cmd := exec.CommandContext(ctx, nodePath, "--version")
	cmd.Env = localEnvironment()
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	version := strings.TrimSpace(string(output))
	if len(version) > 32 || !strings.HasPrefix(version, "v") {
		return ""
	}
	return version
}

func absolute(path string) string {
	if resolved, err := filepath.Abs(path); err == nil {
		return resolved
	}
	return path
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
