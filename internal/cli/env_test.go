package cli_test

import (
	"bytes"
	"context"
	"log/slog"
	"reflect"
	"strings"
	"testing"

	"github.com/Juanqu261/conn/internal/analyzer"
	"github.com/Juanqu261/conn/internal/cli"
	"github.com/Juanqu261/conn/internal/httpclient"
)

// TestEmptyConnEnvironmentValuesAreUnset protects kiro-power requirement 2.3:
// an MCP client may pass an empty string for an unset ${VAR} placeholder, and
// Conn then uses the same default as when the variable is absent. A
// placeholder the client did not substitute is rejected by name.
func TestEmptyConnEnvironmentValuesAreUnset(t *testing.T) {
	unset := func(string) (string, bool) { return "", false }
	executable, workingDir := t.TempDir()+"/conn", t.TempDir()
	for _, name := range []string{cli.ServerURLEnv, analyzer.AnalyzerPathEnv, analyzer.NodePathEnv} {
		for _, value := range []string{"", "  \t"} {
			lookup := func(key string) (string, bool) {
				if key == name {
					return value, true
				}
				return "", false
			}
			if name != cli.ServerURLEnv {
				if got, want := analyzer.Locate(lookup, executable, workingDir), analyzer.Locate(unset, executable, workingDir); !reflect.DeepEqual(got, want) {
					t.Errorf("%s=%q resolved %+v, want the default %+v", name, value, got, want)
				}
				continue
			}
			var served cli.MCPConfig
			code := cli.Execute(context.Background(), []string{"mcp"}, cli.Streams{In: strings.NewReader(""), Out: &bytes.Buffer{}, Err: &bytes.Buffer{}}, cli.Dependencies{
				Version: "test", LookupEnv: lookup,
				ServeMCP: func(_ context.Context, cfg cli.MCPConfig, _ *slog.Logger) error { served = cfg; return nil },
			})
			if code != cli.ExitOK || served.ServerURL != httpclient.DefaultServerURL {
				t.Errorf("%s=%q served %q (exit %d), want %s", name, value, served.ServerURL, code, httpclient.DefaultServerURL)
			}
		}
	}
	if _, err := httpclient.ParseServerURL("${CONN_SERVER_URL}"); err == nil || !strings.Contains(err.Error(), "CONN_SERVER_URL") {
		t.Fatalf("an unsubstituted placeholder = %v, want an error naming CONN_SERVER_URL", err)
	}
}
