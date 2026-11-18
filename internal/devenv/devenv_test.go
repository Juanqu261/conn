package devenv_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Juanqu261/conn/internal/devenv"
)

// TestLoadFile protects the development .env boundary: real environment
// variables win, only CONN_ keys are read, CONN_TOKEN is never loaded from
// a file, and warnings never echo values.
func TestLoadFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	content := strings.Join([]string{
		"\ufeff# comment",
		"",
		"CONN_DATABASE_URL=postgres://conn:pw@127.0.0.1:55432/conn?sslmode=disable#not-a-comment",
		"export CONN_SERVER_URL = \"http://127.0.0.1:9090\"",
		"CONN_DEV_DB_PORT='55433'",
		"CONN_GITHUB_CLIENT_ID=",
		"CONN_ALREADY_SET=from-file",
		"CONN_TOKEN=conn_sess_should-never-load",
		"DATABASE_URL=another-projects-secret",
		"not a setting with secret-value",
	}, "\n")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	env := map[string]string{"CONN_ALREADY_SET": "from-environment"}
	lookup := func(key string) (string, bool) { value, ok := env[key]; return value, ok }
	set := func(key, value string) error { env[key] = value; return nil }

	result, err := devenv.LoadFile(path, lookup, set)
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]string{
		"CONN_DATABASE_URL": "postgres://conn:pw@127.0.0.1:55432/conn?sslmode=disable#not-a-comment",
		"CONN_SERVER_URL":   "http://127.0.0.1:9090",
		"CONN_DEV_DB_PORT":  "55433",
		"CONN_ALREADY_SET":  "from-environment",
	}
	for key, value := range want {
		if env[key] != value {
			t.Errorf("%s = %q, want %q", key, env[key], value)
		}
	}
	for _, key := range []string{"CONN_GITHUB_CLIENT_ID", "CONN_TOKEN", "DATABASE_URL"} {
		if _, ok := env[key]; ok {
			t.Errorf("%s must not be set", key)
		}
	}
	if !slices.Equal(result.Overridden, []string{"CONN_ALREADY_SET"}) || len(result.Loaded) != 3 {
		t.Errorf("loaded %v, overridden %v", result.Loaded, result.Overridden)
	}

	warnings := strings.Join(result.Warnings, "\n")
	if !strings.Contains(warnings, "CONN_TOKEN") || !strings.Contains(warnings, "line 10") {
		t.Errorf("expected warnings for CONN_TOKEN and the malformed line, got:\n%s", warnings)
	}
	if strings.Contains(warnings, "should-never-load") || strings.Contains(warnings, "secret-value") {
		t.Errorf("warnings echo values:\n%s", warnings)
	}

	if result, err := devenv.LoadFile(filepath.Join(t.TempDir(), ".env"), lookup, set); result != nil || err != nil {
		t.Errorf("missing file: %v, %v", result, err)
	}
}
