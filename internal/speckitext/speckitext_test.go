package speckitext

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// committed is the exported extension in this repository.
var committed = filepath.Join("..", "..", "integrations", "speckit", "conn")

const exportCommand = "remove integrations/speckit/conn, then run: go run ./cmd/conn speckit extension export integrations/speckit/conn"

// TestRenderedExtension protects speckit-coordination requirements 4.1-4.6,
// 4.8, and 6.2: the manifest declares schema 1.0, the conn ID, conn as a
// required tool, a Spec Kit range, the three hooks, and speckit.conn.<name>
// commands that cannot clash with Conn's own prompts. Every command uses
// MCP tools or the CLI, runs the deterministic check first, and leaves
// decisions, proposals, and credentials with the human. Output is LF and
// byte-stable, and the committed package is the rendered one.
func TestRenderedExtension(t *testing.T) {
	artifacts, err := Render()
	if err != nil {
		t.Fatal(err)
	}
	again, _ := Render()
	files := map[string]string{}
	for i, artifact := range artifacts {
		if !bytes.Equal(artifact.Content, again[i].Content) {
			t.Errorf("%s is not byte-stable", artifact.RelativePath)
		}
		if bytes.Contains(artifact.Content, []byte("\r")) || !bytes.HasSuffix(artifact.Content, []byte("\n")) {
			t.Errorf("%s does not use LF line endings with a final newline", artifact.RelativePath)
		}
		files[artifact.RelativePath] = string(artifact.Content)
		content, err := os.ReadFile(filepath.Join(committed, filepath.FromSlash(artifact.RelativePath)))
		if err != nil || !bytes.Equal(content, artifact.Content) {
			t.Errorf("integrations/speckit/conn/%s differs from the rendered extension; %s", artifact.RelativePath, exportCommand)
		}
	}

	manifest := files["extension.yml"]
	for _, line := range []string{
		`schema_version: "1.0"`, "  id: conn", `  version: "` + Version + `"`, `  speckit_version: ">=1.4.0,<2.0.0"`,
		`    - name: "conn"` + "\n      required: true",
		"  after_tasks:\n    command: \"speckit.conn.check\"\n    optional: true",
		"  before_implement:\n    command: \"speckit.conn.start\"\n    optional: false",
		"  after_implement:\n    command: \"speckit.conn.finish\"\n    optional: true",
	} {
		if !strings.Contains(manifest, line) {
			t.Errorf("extension.yml lacks %q", line)
		}
	}
	if strings.Contains(manifest, "\nevents:") {
		t.Error("extension.yml declares runtime events, which the spike did not confirm")
	}
	name := regexp.MustCompile(`^speckit\.conn\.[a-z0-9-]+$`)
	for _, command := range Commands {
		if !name.MatchString(command.Name) || strings.HasPrefix(command.Name, "conn-") {
			t.Errorf("command name %q", command.Name)
		}
		body := files[command.File]
		if !strings.HasPrefix(body, "---\ndescription: \"") || !strings.Contains(body, "$ARGUMENTS") || !strings.Contains(manifest, `file: "`+command.File+`"`) {
			t.Errorf("%s is not a Spec Kit command file", command.File)
		}
		for _, phrase := range []string{
			"conn speckit check --format json", "Conn MCP tools", "`conn` CLI", "Do not invent",
			"Never record a decision, accept or reject a resolution proposal", "create a credential", "never edit `.specify/`",
			"untrusted Project data",
		} {
			if !strings.Contains(body, phrase) {
				t.Errorf("%s omits %q", command.File, phrase)
			}
		}
	}
	if !strings.Contains(files["commands/finish.md"], "conn evidence run <check-key>") || !strings.Contains(files["commands/finish.md"], "head_revision_ref") {
		t.Error("the finish command does not record the head and offer approved evidence runs")
	}
}

// TestExportRefusesDifferentContent protects requirement 4.7: export writes
// the package, keeps unrelated and unchanged files, and refuses different
// content without writing anything.
func TestExportRefusesDifferentContent(t *testing.T) {
	dir := t.TempDir()
	unrelated := filepath.Join(dir, "NOTES.md")
	if err := os.WriteFile(unrelated, []byte("team notes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	count := func(changes []Change, action Action) int {
		n := 0
		for _, change := range changes {
			if change.Action == action {
				n++
			}
		}
		return n
	}
	changes, err := Export(dir)
	if err != nil || count(changes, ActionCreate) != len(changes) {
		t.Fatalf("first export = %v, %v", changes, err)
	}
	if changes, err = Export(dir); err != nil || count(changes, ActionUnchanged) != len(changes) {
		t.Fatalf("repeated export = %v, %v", changes, err)
	}
	command := filepath.Join(dir, "commands", "check.md")
	if err := os.WriteFile(command, []byte("edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "commands", "review.md")); err != nil {
		t.Fatal(err)
	}
	if changes, err = Export(dir); !errors.Is(err, ErrConflict) || count(changes, ActionConflict) != 1 {
		t.Fatalf("export over an edited command = %v, %v; want one conflict", changes, err)
	}
	if edited, _ := os.ReadFile(command); string(edited) != "edited\n" {
		t.Fatal("a refused export changed the conflicting file")
	}
	if _, err := os.Stat(filepath.Join(dir, "commands", "review.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a refused export wrote a missing file")
	}
	if notes, err := os.ReadFile(unrelated); err != nil || string(notes) != "team notes\n" {
		t.Fatal("export changed a file it did not render")
	}
}
