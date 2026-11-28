package kiropower

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// committed is the power folder in this repository.
var committed = filepath.Join("..", "..", "powers", "conn")

const exportCommand = "go run ./cmd/conn kiro power export powers/conn --replace"

// TestRenderedPowerIsSafeAndStable protects kiro-power requirements 1, 2, 5,
// and 9.1: the manifest has the required keys, the server runs conn mcp from
// PATH with only the server URL placeholder, every skill has front matter,
// output is LF and byte-stable, and no file holds a credential, address,
// machine path, or identifier.
func TestRenderedPowerIsSafeAndStable(t *testing.T) {
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
	}

	var plugin map[string]any
	if err := json.Unmarshal([]byte(files["plugin.json"]), &plugin); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"$schema", "name", "version", "description", "author", "license", "repository", "keywords"} {
		if plugin[key] == nil {
			t.Errorf("plugin.json lacks %s", key)
		}
	}
	if plugin["$schema"] != PluginSchema || plugin["name"] != "conn" || plugin["version"] != Version || plugin["author"].(map[string]any)["name"] == "" {
		t.Errorf("plugin.json = %v", plugin)
	}
	for _, generic := range []string{"review", "test", "code", "git", "edit"} {
		for _, keyword := range Keywords {
			if keyword == generic {
				t.Errorf("keyword %q is too generic to activate the power safely", keyword)
			}
		}
	}

	var server struct {
		Schema  string `json:"$schema"`
		Servers map[string]struct {
			Type    string            `json:"type"`
			Command string            `json:"command"`
			Args    []string          `json:"args"`
			Env     map[string]string `json:"env"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal([]byte(files["mcp.json"]), &server); err != nil {
		t.Fatal(err)
	}
	conn := server.Servers["conn"]
	if server.Schema != MCPSchema || len(server.Servers) != 1 || conn.Type != "stdio" || conn.Command != "conn" ||
		strings.Join(conn.Args, " ") != "mcp" || len(conn.Env) != 1 || conn.Env["CONN_SERVER_URL"] != ServerURLPlaceholder {
		t.Errorf("mcp.json = %s", files["mcp.json"])
	}

	frontMatter := regexp.MustCompile(`\A---\nname: ([a-z-]+)\ndescription: "[^"\n]+"\n---\n\n# `)
	for _, name := range []string{"conn-setup", "conn-work-loop", "conn-review-proof"} {
		content := files["skills/"+name+"/SKILL.md"]
		if match := frontMatter.FindStringSubmatch(content); match == nil || match[1] != name {
			t.Errorf("skill %s front matter = %.120q", name, content)
		}
	}
	if !strings.Contains(files["skills/conn-setup/SKILL.md"], MinimumConnVersion) || strings.Contains(files["skills/conn-setup/SKILL.md"], "{{") {
		t.Error("the setup skill does not name the minimum conn version")
	}
	if files["skills/conn-setup/references/install-from-source.md"] == "" {
		t.Error("the setup skill has no installation reference")
	}

	allowedURL := regexp.MustCompile(`^https://(agent-plugins\.org/schemas/|github\.com/Juanqu261/conn([.#/]|$))`)
	url := regexp.MustCompile(`https?://[^\s"'<>)\x60]+`)
	sensitive := map[string]*regexp.Regexp{
		"token prefix":  regexp.MustCompile(`conn_`),
		"UUID":          regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`),
		"absolute path": regexp.MustCompile(`(?i)(\b[a-z]:[\\/]|(^|[\s"'(\x60])/(home|users|usr|opt|tmp|var|etc|private)/)`),
	}
	for path, content := range files {
		for _, found := range url.FindAllString(content, -1) {
			if !allowedURL.MatchString(found) {
				t.Errorf("%s contains the address %s", path, found)
			}
		}
		for label, pattern := range sensitive {
			if found := pattern.FindString(content); found != "" {
				t.Errorf("%s contains a %s: %q", path, label, found)
			}
		}
	}
}

// TestExportRefusesDifferentContent protects requirement 6.2: export writes
// the power, keeps unrelated files, refuses different content without
// --replace, and refuses to record changed content under the same version.
func TestExportRefusesDifferentContent(t *testing.T) {
	dir := t.TempDir()
	unrelated := filepath.Join(dir, "NOTES.md")
	if err := os.WriteFile(unrelated, []byte("team notes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	actions := func(changes []Change) map[Action]int {
		counts := map[Action]int{}
		for _, change := range changes {
			counts[change.Action]++
		}
		return counts
	}
	changes, err := Export(dir, false)
	if err != nil || actions(changes)[ActionCreate] != len(changes) {
		t.Fatalf("first export = %v, %v", changes, err)
	}
	if changes, err = Export(dir, false); err != nil || actions(changes)[ActionUnchanged] != len(changes) {
		t.Fatalf("repeated export = %v, %v", changes, err)
	}

	skill := filepath.Join(dir, "skills", "conn-setup", "SKILL.md")
	before, _ := os.ReadFile(filepath.Join(dir, "plugin.json"))
	if err := os.WriteFile(skill, []byte("edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if changes, err = Export(dir, false); !errors.Is(err, ErrConflict) || actions(changes)[ActionConflict] != 1 {
		t.Fatalf("export over an edited skill = %v, %v; want one conflict", changes, err)
	}
	if edited, _ := os.ReadFile(skill); string(edited) != "edited\n" {
		t.Fatal("a refused export wrote the conflicting file")
	}
	if changes, err = Export(dir, true); err != nil || actions(changes)[ActionReplace] != 1 {
		t.Fatalf("export with replace = %v, %v", changes, err)
	}
	if after, _ := os.ReadFile(filepath.Join(dir, "plugin.json")); !bytes.Equal(before, after) {
		t.Fatal("export rewrote an unchanged file")
	}
	if notes, err := os.ReadFile(unrelated); err != nil || string(notes) != "team notes\n" {
		t.Fatal("export changed a file it did not render")
	}

	stale := Version + " sha256:" + strings.Repeat("0", 64) + "\n"
	if err := os.WriteFile(filepath.Join(dir, DigestFile), []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Export(dir, true); !errors.Is(err, ErrVersionNotRaised) {
		t.Fatalf("export over a same-version digest = %v, want ErrVersionNotRaised", err)
	}
}

// TestCommittedPowerMatchesRender protects requirements 6.3 and 6.4: the
// committed power is the rendered power, and its content never changes under
// an unchanged version.
func TestCommittedPowerMatchesRender(t *testing.T) {
	artifacts, err := Render()
	if err != nil {
		t.Fatal(err)
	}
	recorded, err := os.ReadFile(filepath.Join(committed, DigestFile))
	if err != nil {
		t.Fatalf("read the committed power digest: %v\nRun: %s", err, exportCommand)
	}
	version, digest, err := ParseDigest(recorded)
	if err != nil {
		t.Fatal(err)
	}
	if version == Version && digest != Digest(artifacts) {
		t.Fatalf("the rendered power changed but kiropower.Version is still %s; raise Version, then run: %s", Version, exportCommand)
	}
	all, _ := RenderAll()
	for _, artifact := range all {
		content, err := os.ReadFile(filepath.Join(committed, filepath.FromSlash(artifact.RelativePath)))
		if err != nil || !bytes.Equal(content, artifact.Content) {
			t.Errorf("powers/conn/%s differs from the rendered power", artifact.RelativePath)
		}
	}
	if t.Failed() {
		t.Logf("Run: %s", exportCommand)
	}
}

func TestCheckVersion(t *testing.T) {
	for version, want := range map[string]VersionStatus{
		"dev": VersionSourceDev, "": VersionSourceDev, MinimumConnVersion: VersionSupported, "v" + MinimumConnVersion: VersionSupported,
		"99.0.0": VersionSupported, "0.0.1": VersionTooOld, "0.0.1-rc.1": VersionTooOld, "abc1234": VersionUnrecognized,
	} {
		if got := CheckVersion(version); got != want {
			t.Errorf("CheckVersion(%q) = %s, want %s", version, got, want)
		}
	}
}

// TestRegistrationsFindManualConnServers protects requirement 7.3: conn
// doctor finds, without writing, every workspace and user Kiro MCP entry that
// runs conn mcp, by PATH name or absolute path.
func TestRegistrationsFindManualConnServers(t *testing.T) {
	workspace, home := t.TempDir(), t.TempDir()
	write := func(root, content string) {
		t.Helper()
		path := filepath.Join(root, KiroMCPConfig)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if found, err := Registrations(workspace, home); err != nil || len(found) != 0 {
		t.Fatalf("no configuration = %v, %v", found, err)
	}
	write(workspace, `{"mcpServers": {"conn": {"command": "C:\\tools\\conn\\conn.exe", "args": ["mcp"]}, "other": {"command": "npx", "args": ["mcp"]}, "cli": {"command": "conn", "args": ["doctor"]}}}`)
	write(home, `{"mcpServers": {"team-conn": {"command": "/opt/conn/conn", "args": ["mcp"], "disabled": true}}}`)
	before, _ := os.ReadFile(filepath.Join(workspace, KiroMCPConfig))
	found, err := Registrations(workspace, home)
	if err != nil || len(found) != 2 || found[0].Scope != "workspace" || found[0].Name != "conn" ||
		found[1].Scope != "user" || found[1].Name != "team-conn" || !found[1].Disabled {
		t.Fatalf("registrations = %+v, %v", found, err)
	}
	if after, _ := os.ReadFile(filepath.Join(workspace, KiroMCPConfig)); !bytes.Equal(before, after) {
		t.Fatal("the scan changed MCP configuration")
	}
	write(home, `{not json`)
	if found, err := Registrations(workspace, home); err == nil || len(found) != 1 {
		t.Fatalf("an invalid user file = %+v, %v; want the workspace entry and an error", found, err)
	}
}
