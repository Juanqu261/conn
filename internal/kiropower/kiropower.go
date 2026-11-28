// Package kiropower renders the Conn Kiro power in the Agent Plugins format:
// plugin.json, mcp.json, and three skills. Every skill body comes from
// internal/agentguide, the same source as the Kiro steering and AGENTS.md, so
// the power cannot drift from them.
//
// The power declares the conn MCP server and on-demand guidance only. The
// always-on steering and warn-only hooks stay workspace files that
// internal/kirointegration installs. Conn never writes MCP configuration:
// Kiro manages the power's server, and export writes only into a folder the
// human names.
package kiropower

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/Juanqu261/conn/internal/agentguide"
)

// Version is the power's semantic version. Raise it by hand whenever any
// rendered file changes; the committed .digest enforces it.
const Version = "1.0.0"

// MinimumConnVersion is the oldest stamped conn release the power supports.
// A source build without a version stamp reports "dev".
const MinimumConnVersion = "0.4.0"

// Name is the power's stable name. Renaming an installed power requires
// reinstalling it.
const Name = "conn"

// RepositoryURL is the public Conn repository.
const RepositoryURL = "https://github.com/Juanqu261/conn"

// Schema URLs of the Agent Plugins format.
const (
	PluginSchema = "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json"
	MCPSchema    = "https://agent-plugins.org/schemas/1.0.0/mcp.schema.json"
)

// ServerURLPlaceholder is the only value the power passes to conn mcp.
const ServerURLPlaceholder = "${CONN_SERVER_URL}"

// DigestFile records the power version and the digest of the other files.
const DigestFile = ".digest"

// Description is the listing text for the powers panel and the registry.
const Description = "Warns your agent before it edits when a teammate's in-progress work changes the same behavior, contract, or data, and gives reviewers proof of what each change intended, touched, and verified."

// Keywords activate the power. Words too generic to be safe, such as
// "review" or "test", are excluded because unrelated activation costs every
// user context. The activation spike tunes this list.
var Keywords = []string{
	"conn",
	"collision",
	"collisions",
	"parallel work",
	"concurrent changes",
	"teammate",
	"intent",
	"claims",
	"coordination",
	"counterpart",
	"merge conflict",
	"review packet",
	"review proof",
	"evidence",
	"pull request description",
}

//go:embed templates/install-from-source.md
var installFromSource string

// Artifact is one rendered power file. RelativePath uses forward slashes.
type Artifact struct {
	RelativePath string
	Content      []byte
}

type skill struct {
	name, description string
	body              func() (string, error)
}

var skills = []skill{
	{
		name:        "conn-setup",
		description: "Set up Conn in this workspace: check the conn executable, the backend, your session, and the Project, then turn on pre-edit warnings.",
		body: func() (string, error) {
			body, err := agentguide.Render(agentguide.AudiencePowerSetup)
			return strings.ReplaceAll(body, agentguide.MinimumVersionPlaceholder, MinimumConnVersion), err
		},
	},
	{
		name:        "conn-work-loop",
		description: "Follow Conn's work loop: start or resume an Intent before editing, sync teammates' changes, review advisory collisions, and finish the Intent.",
		body: func() (string, error) {
			guide, err := agentguide.Render(agentguide.AudienceKiro)
			if err != nil {
				return "", err
			}
			title, rest, _ := strings.Cut(guide, "\n")
			lead := "\nIf this workspace has no `.kiro/steering/conn-work-loop.md`, run step 6 of the `conn-setup` skill so that pre-edit warnings stay on while this power is inactive.\n\n" +
				"The `/conn-*` commands are Conn's MCP prompts. When Kiro does not list them as slash commands, follow the same steps with the tools they name, starting with `read_work_context`.\n"
			return title + "\n" + lead + rest, nil
		},
	},
	{
		name:        "conn-review-proof",
		description: "Prepare review proof with Conn: Conn-Intent commit trailers, checks from .conn/checks.json, labelled evidence, and the review packet as a pull-request description.",
		body:        func() (string, error) { return agentguide.Render(agentguide.AudiencePowerReview) },
	},
}

// Render returns the power files without the digest, in a fixed order, with
// LF line endings and byte-stable JSON.
func Render() ([]Artifact, error) {
	plugin, err := renderJSON(map[string]any{
		"$schema":     PluginSchema,
		"name":        Name,
		"version":     Version,
		"description": Description,
		"author":      map[string]any{"name": "Conn"},
		"license":     "MIT",
		"repository":  RepositoryURL,
		"homepage":    RepositoryURL + "#readme",
		"keywords":    Keywords,
	})
	if err != nil {
		return nil, err
	}
	server, err := renderJSON(map[string]any{
		"$schema": MCPSchema,
		"mcpServers": map[string]any{
			"conn": map[string]any{
				"type":    "stdio",
				"command": "conn",
				"args":    []string{"mcp"},
				"env":     map[string]any{"CONN_SERVER_URL": ServerURLPlaceholder},
			},
		},
	})
	if err != nil {
		return nil, err
	}
	artifacts := []Artifact{{RelativePath: "plugin.json", Content: plugin}, {RelativePath: "mcp.json", Content: server}}
	for _, skill := range skills {
		body, err := skill.body()
		if err != nil {
			return nil, err
		}
		if strings.ContainsAny(skill.description, "\"\\\n") {
			return nil, fmt.Errorf("skill %s description cannot be a YAML double-quoted string", skill.name)
		}
		content := "---\nname: " + skill.name + "\ndescription: \"" + skill.description + "\"\n---\n\n" + body
		artifacts = append(artifacts, Artifact{RelativePath: "skills/" + skill.name + "/SKILL.md", Content: lf(content)})
	}
	artifacts = append(artifacts, Artifact{RelativePath: "skills/conn-setup/references/install-from-source.md", Content: lf(installFromSource)})
	return artifacts, nil
}

// RenderAll returns the power files followed by the digest file.
func RenderAll() ([]Artifact, error) {
	artifacts, err := Render()
	if err != nil {
		return nil, err
	}
	return append(artifacts, Artifact{RelativePath: DigestFile, Content: []byte(Version + " sha256:" + Digest(artifacts) + "\n")}), nil
}

// Digest hashes the power files by path and content, ignoring the digest
// file itself.
func Digest(artifacts []Artifact) string {
	sorted := append([]Artifact(nil), artifacts...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].RelativePath < sorted[j].RelativePath })
	hash := sha256.New()
	for _, artifact := range sorted {
		if artifact.RelativePath == DigestFile {
			continue
		}
		fmt.Fprintf(hash, "%s\x00%d\x00", artifact.RelativePath, len(artifact.Content))
		hash.Write(artifact.Content)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

// ParseDigest reads a digest file as its recorded version and digest.
func ParseDigest(content []byte) (version, digest string, err error) {
	version, rest, found := strings.Cut(strings.TrimSpace(string(content)), " ")
	digest, prefixed := strings.CutPrefix(rest, "sha256:")
	if !found || !prefixed || version == "" || len(digest) != sha256.Size*2 {
		return "", "", errors.New("the power digest file is not `<version> sha256:<hex>`")
	}
	return version, digest, nil
}

func renderJSON(value any) ([]byte, error) {
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func lf(text string) []byte {
	return []byte(strings.ReplaceAll(text, "\r\n", "\n"))
}

// Action is what export does to one file.
type Action string

const (
	ActionCreate    Action = "create"
	ActionUnchanged Action = "unchanged"
	ActionReplace   Action = "replace"
	ActionConflict  Action = "conflict"
)

// Change is one planned export of a file.
type Change struct {
	RelativePath string
	Action       Action
}

var (
	// ErrConflict means a target holds different content and --replace was
	// not given. Nothing was written.
	ErrConflict = errors.New("the power folder holds different content; pass --replace to overwrite Conn's files")
	// ErrVersionNotRaised means the rendered power changed while Version
	// stayed the version the folder's digest records.
	ErrVersionNotRaised = errors.New("the rendered power changed but kiropower.Version did not; raise Version before exporting")
)

// Export writes the rendered power and its digest into dir. Without replace,
// a target with different content is a conflict and nothing is written. It
// never deletes a file, and refuses to record changed content under an
// unchanged version.
func Export(dir string, replace bool) ([]Change, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, errors.New("name the folder to export the power into")
	}
	artifacts, err := RenderAll()
	if err != nil {
		return nil, err
	}
	digest := Digest(artifacts)
	changes := make([]Change, 0, len(artifacts))
	conflict := false
	for _, artifact := range artifacts {
		path := filepath.Join(dir, filepath.FromSlash(artifact.RelativePath))
		existing, err := readRegular(path)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			changes = append(changes, Change{artifact.RelativePath, ActionCreate})
			continue
		case err != nil:
			return nil, err
		}
		if artifact.RelativePath == DigestFile {
			if recorded, recordedDigest, err := ParseDigest(existing); err == nil && recorded == Version && recordedDigest != digest {
				return nil, ErrVersionNotRaised
			}
		}
		switch {
		case bytes.Equal(existing, artifact.Content):
			changes = append(changes, Change{artifact.RelativePath, ActionUnchanged})
		case replace:
			changes = append(changes, Change{artifact.RelativePath, ActionReplace})
		default:
			changes = append(changes, Change{artifact.RelativePath, ActionConflict})
			conflict = true
		}
	}
	if conflict {
		return changes, ErrConflict
	}
	for i, artifact := range artifacts {
		if changes[i].Action == ActionUnchanged {
			continue
		}
		path := filepath.Join(dir, filepath.FromSlash(artifact.RelativePath))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return changes, err
		}
		if err := os.WriteFile(path, artifact.Content, 0o644); err != nil {
			return changes, err
		}
	}
	return changes, nil
}

// readRegular reads a file, refusing a directory or symbolic link at a
// target path.
func readRegular(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", filepath.ToSlash(path))
	}
	return os.ReadFile(path)
}

// VersionStatus compares a conn version with MinimumConnVersion.
type VersionStatus string

const (
	VersionSupported    VersionStatus = "supported"
	VersionTooOld       VersionStatus = "too_old"
	VersionSourceDev    VersionStatus = "source_build"
	VersionUnrecognized VersionStatus = "unrecognized"
)

// CheckVersion classifies the running conn version against the power's
// minimum. "dev" is a source build without a version stamp.
func CheckVersion(version string) VersionStatus {
	version = strings.TrimSpace(version)
	if version == "" || version == "dev" {
		return VersionSourceDev
	}
	have, ok := parseVersion(version)
	if !ok {
		return VersionUnrecognized
	}
	want, _ := parseVersion(MinimumConnVersion)
	for i := range have {
		if have[i] != want[i] {
			if have[i] > want[i] {
				return VersionSupported
			}
			return VersionTooOld
		}
	}
	return VersionSupported
}

func parseVersion(version string) ([3]int, bool) {
	version = strings.TrimPrefix(version, "v")
	if cut := strings.IndexAny(version, "-+"); cut >= 0 {
		version = version[:cut]
	}
	parts := strings.Split(version, ".")
	var out [3]int
	if len(parts) != 3 {
		return out, false
	}
	for i, part := range parts {
		value, err := strconv.Atoi(part)
		if err != nil || value < 0 {
			return out, false
		}
		out[i] = value
	}
	return out, true
}
