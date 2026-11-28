// Package speckitext renders the Conn extension for GitHub Spec Kit:
// extension.yml and one command file per Conn step. Every command body comes
// from internal/agentguide, the same source as the Kiro steering and the
// AGENTS.md section, so the surfaces cannot disagree.
//
// The extension calls Conn at after_tasks (optional plan check),
// before_implement (publish or revise the Intent, then check collisions),
// and after_implement (optional finish and review proof). A Spec Kit hook
// runs an agent command, never a shell command, so the commands tell the
// agent which Conn tool or CLI command to use. Conn exports the package into
// a folder the human names; the human installs it with
// `specify extension add --dev <dir>`. Conn never writes .specify/.
package speckitext

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/Juanqu261/conn/internal/agentguide"
)

// Version is the extension's semantic version. Raise it whenever a rendered
// file changes, so Spec Kit sees an update.
const Version = "1.0.0"

// ID is the Spec Kit extension ID; commands are speckit.<ID>.<name>.
const ID = "conn"

// SpecKitVersion is the supported Spec Kit version range.
const SpecKitVersion = ">=1.4.0,<2.0.0"

// RepositoryURL is the public Conn repository.
const RepositoryURL = "https://github.com/Juanqu261/conn"

// InstallCommand is Spec Kit's command that installs a local extension.
const InstallCommand = "specify extension add --dev"

// Command is one extension command.
type Command struct {
	Name        string
	File        string
	Description string
}

// Commands are the extension's commands, in manifest order.
var Commands = []Command{
	{Name: "speckit.conn.check", File: "commands/check.md", Description: "Check this feature's plan against teammates' in-progress work with Conn, publishing nothing."},
	{Name: "speckit.conn.start", File: "commands/start.md", Description: "Publish or revise this feature's Conn Intent and check collisions before implementing."},
	{Name: "speckit.conn.finish", File: "commands/finish.md", Description: "Record the head revision, analyze it, offer evidence runs, and produce Conn review proof."},
	{Name: "speckit.conn.review", File: "commands/review.md", Description: "Produce the Conn review packet for this feature as a pull-request description."},
}

// Artifact is one rendered file. RelativePath uses forward slashes.
type Artifact struct {
	RelativePath string
	Content      []byte
}

// Render returns the extension files in a fixed order with LF line endings.
func Render() ([]Artifact, error) {
	artifacts := []Artifact{{RelativePath: "extension.yml", Content: []byte(manifest())}}
	for i, command := range Commands {
		body, err := agentguide.SpecKitCommand(agentguide.SpecKitCommands[i])
		if err != nil {
			return nil, err
		}
		if strings.ContainsAny(command.Description, "\"\\\n") {
			return nil, fmt.Errorf("command %s description cannot be a YAML double-quoted string", command.Name)
		}
		content := "---\ndescription: \"" + command.Description + "\"\n---\n\n" +
			"## User Input\n\n```text\n$ARGUMENTS\n```\n\n" +
			"Consider the user input before proceeding. It may name the feature directory, such as `specs/001-feature-name`.\n\n" + body
		artifacts = append(artifacts, Artifact{RelativePath: command.File, Content: lf(content)})
	}
	return artifacts, nil
}

func manifest() string {
	var out strings.Builder
	out.WriteString(`# Conn extension for GitHub Spec Kit. Rendered by conn speckit extension export.
schema_version: "1.0"

extension:
  id: ` + ID + `
  name: "Conn"
  version: "` + Version + `"
  description: "Warns when another in-progress feature changes what this plan changes, and attaches review proof."
  author: "Conn"
  repository: "` + RepositoryURL + `"
  license: "MIT"
  homepage: "` + RepositoryURL + `#readme"

requires:
  speckit_version: "` + SpecKitVersion + `"
  tools:
    - name: "conn"
      required: true

provides:
  commands:
`)
	for _, command := range Commands {
		fmt.Fprintf(&out, "    - name: %q\n      file: %q\n      description: %q\n", command.Name, command.File, command.Description)
	}
	out.WriteString(`
hooks:
  after_tasks:
    command: "speckit.conn.check"
    optional: true
    prompt: "Check this plan against teammates' in-progress work with Conn?"
    description: "Previews collisions for the planned paths. Publishes nothing."
  before_implement:
    command: "speckit.conn.start"
    optional: false
    description: "Conn publishes or revises this feature's Intent after confirmation and warns about collisions. It never blocks."
  after_implement:
    command: "speckit.conn.finish"
    optional: true
    prompt: "Record review proof for this feature with Conn?"
    description: "Records the head revision, analyzes it, offers approved check runs, and produces the review packet."

tags:
  - "coordination"
  - "collisions"
  - "review"
`)
	return out.String()
}

func lf(text string) []byte { return []byte(strings.ReplaceAll(text, "\r\n", "\n")) }

// Action is what export does to one file.
type Action string

const (
	ActionCreate    Action = "create"
	ActionUnchanged Action = "unchanged"
	ActionConflict  Action = "conflict"
)

// Change is one planned export of a file.
type Change struct {
	RelativePath string
	Action       Action
}

// ErrConflict means a target holds different content. Nothing was written.
var ErrConflict = errors.New("the folder holds different content; remove the conflicting files or choose another folder to export this version")

// Export writes the rendered extension into dir. A target with different
// content is a conflict and nothing is written; unchanged files are kept.
// It never deletes a file.
func Export(dir string) ([]Change, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, errors.New("name the folder to export the extension into")
	}
	artifacts, err := Render()
	if err != nil {
		return nil, err
	}
	changes := make([]Change, 0, len(artifacts))
	conflict := false
	for _, artifact := range artifacts {
		target := filepath.Join(dir, filepath.FromSlash(artifact.RelativePath))
		existing, err := readRegular(target)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			changes = append(changes, Change{artifact.RelativePath, ActionCreate})
		case err != nil:
			return nil, err
		case bytes.Equal(existing, artifact.Content):
			changes = append(changes, Change{artifact.RelativePath, ActionUnchanged})
		default:
			changes = append(changes, Change{artifact.RelativePath, ActionConflict})
			conflict = true
		}
	}
	if conflict {
		return changes, ErrConflict
	}
	for i, artifact := range artifacts {
		if changes[i].Action != ActionCreate {
			continue
		}
		target := filepath.Join(dir, filepath.FromSlash(artifact.RelativePath))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return changes, err
		}
		file, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			return changes, err
		}
		_, writeErr := file.Write(artifact.Content)
		if closeErr := file.Close(); writeErr != nil || closeErr != nil {
			return changes, errors.Join(writeErr, closeErr)
		}
	}
	return changes, nil
}

// readRegular reads a file, refusing a directory or symbolic link.
func readRegular(name string) ([]byte, error) {
	info, err := os.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", filepath.ToSlash(name))
	}
	return os.ReadFile(name)
}
