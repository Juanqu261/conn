package kiropower

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Registration is a manual Kiro MCP entry that runs `conn mcp`, found by a
// read-only scan. With the power installed, it starts a second copy of the
// same server.
type Registration struct {
	Scope    string // "workspace" or "user"
	Path     string
	Name     string
	Disabled bool
}

// KiroMCPConfig is the Kiro MCP configuration file below a workspace root or
// the user's home directory.
var KiroMCPConfig = filepath.Join(".kiro", "settings", "mcp.json")

// Registrations reads the workspace and user Kiro MCP configuration and
// returns every entry that runs `conn mcp`. It never writes. A missing file
// is not an error; an unreadable or invalid one is returned as an error with
// the registrations found elsewhere.
func Registrations(workspaceRoot, home string) ([]Registration, error) {
	var found []Registration
	var problems []error
	seen := map[string]bool{}
	for _, location := range []struct{ scope, root string }{{"workspace", workspaceRoot}, {"user", home}} {
		if location.root == "" {
			continue
		}
		path := filepath.Join(location.root, KiroMCPConfig)
		if absolute, err := filepath.Abs(path); err == nil {
			path = absolute
		}
		// A workspace at the home directory shares one file.
		if seen[strings.ToLower(path)] {
			continue
		}
		seen[strings.ToLower(path)] = true
		content, err := os.ReadFile(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			problems = append(problems, err)
			continue
		}
		var document struct {
			Servers map[string]struct {
				Command  string   `json:"command"`
				Args     []string `json:"args"`
				Disabled bool     `json:"disabled"`
			} `json:"mcpServers"`
		}
		if err := json.Unmarshal(content, &document); err != nil {
			problems = append(problems, errors.New(filepath.ToSlash(path)+" is not valid JSON"))
			continue
		}
		names := make([]string, 0, len(document.Servers))
		for name := range document.Servers {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			server := document.Servers[name]
			if runsConnMCP(server.Command, server.Args) {
				found = append(found, Registration{Scope: location.scope, Path: path, Name: name, Disabled: server.Disabled})
			}
		}
	}
	return found, errors.Join(problems...)
}

// runsConnMCP reports whether a command and arguments start `conn mcp`,
// whether conn is named on PATH or by an absolute path.
func runsConnMCP(command string, args []string) bool {
	base := strings.ToLower(filepath.Base(strings.ReplaceAll(strings.Trim(command, `"' `), `\`, "/")))
	base = strings.TrimSuffix(base, ".exe")
	return base == "conn" && len(args) > 0 && args[0] == "mcp"
}
