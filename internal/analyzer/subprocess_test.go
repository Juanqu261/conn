package analyzer

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const reportHeader = `report_version:2,mode:"observed",repository:{canonical_remote:"github.com/acme/payments"}`

func TestRunBoundsOutputAndStripsConnEnvironment(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node.js is required for the local analyzer subprocess fixture")
	}
	worktree := t.TempDir()
	script := func(body string) string {
		path := filepath.Join(t.TempDir(), "analyzer.mjs")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	request := Request{Mode: "observed", Worktree: worktree, CanonicalRemote: "github.com/acme/payments", BaseRevisionRef: "abcdef1"}
	run := func(body string, timeout time.Duration) error {
		_, err := Run(context.Background(), Config{NodePath: node, AnalyzerPath: script(body), Timeout: timeout}, request)
		return err
	}

	t.Setenv("CONN_TOKEN", "conn_sess_not_for_analyzer")
	report, err := Run(context.Background(), Config{NodePath: node, AnalyzerPath: script(`
let input = ""; process.stdin.on("data", c => input += c); process.stdin.on("end", () => {
  const request = JSON.parse(input);
  const ok = request.request_version === 2 && request.mode === "observed" && request.base_revision_ref === "abcdef1" && !("symbols" in request);
  console.log(JSON.stringify({` + reportHeader + `,claims:[],diagnostics:[{code:"environment",message:(process.env.CONN_TOKEN ? "token present" : "token absent") + (ok ? " request ok" : " request wrong")}]}));
});`), Timeout: time.Second}, request)
	if err != nil {
		t.Fatal(err)
	}
	if got := report.Diagnostics[0].Message; got != "token absent request ok" {
		t.Fatalf("analyzer environment or request = %q", got)
	}

	for _, tc := range []struct {
		name string
		body string
		want error
	}{
		{"source field", `console.log(JSON.stringify({` + reportHeader + `,claims:[],diagnostics:[],source:"do not accept source"}));`, ErrInvalid},
		{"prompt field", `console.log(JSON.stringify({` + reportHeader + `,claims:[],diagnostics:[],prompt:"do not accept prompts"}));`, ErrInvalid},
		{"credential-shaped value", `console.log(JSON.stringify({` + reportHeader + `,claims:[{subject_kind:"code_symbol",normalized_subject:"symbol:typescript:src/a.ts/value",display_name:"ghp_credential_value",relation:"changes",provenance_kind:"static_analysis"}],diagnostics:[]}));`, ErrInvalid},
		{"oversized diagnostics", `console.log(JSON.stringify({` + reportHeader + `,claims:[],diagnostics:Array.from({length:101}, (_, i) => ({code:"TS"+i,message:"TypeScript diagnostic TS"+i}))}));`, ErrInvalid},
		{"removed version 1", `console.log(JSON.stringify({report_version:1,repository:{canonical_remote:"github.com/acme/payments"},claims:[],diagnostics:[]}));`, ErrInvalid},
		{"mode mismatch", `console.log(JSON.stringify({report_version:2,mode:"planned",repository:{canonical_remote:"github.com/acme/payments"},claims:[],diagnostics:[]}));`, ErrInvalid},
	} {
		if err := run(tc.body, time.Second); !errors.Is(err, tc.want) {
			t.Errorf("%s = %v, want %v", tc.name, err, tc.want)
		}
	}
	if err := run(`setTimeout(() => {}, 2000);`, 20*time.Millisecond); !errors.Is(err, ErrTimedOut) {
		t.Fatalf("timeout = %v, want timeout", err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = Run(cancelled, Config{NodePath: node, AnalyzerPath: script(`setTimeout(() => {}, 2000);`)}, request)
	if !errors.Is(err, context.Canceled) && !strings.Contains(err.Error(), "canceled") {
		t.Fatalf("cancellation = %v", err)
	}
	// Option-like references and planned requests without paths never start
	// the subprocess.
	for _, invalid := range []Request{
		{Mode: "observed", Worktree: worktree, CanonicalRemote: "github.com/acme/payments", BaseRevisionRef: "--output=/tmp/x"},
		{Mode: "planned", Worktree: worktree, CanonicalRemote: "github.com/acme/payments"},
		{Mode: "unknown", Worktree: worktree, CanonicalRemote: "github.com/acme/payments"},
	} {
		if _, err := Run(context.Background(), Config{NodePath: node, AnalyzerPath: script(`process.exit(3);`)}, invalid); !errors.Is(err, ErrInvalid) {
			t.Errorf("invalid request %+v = %v, want invalid", invalid, err)
		}
	}
}

func TestRunReportsMissingAnalyzerScript(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "dist", "cli.js")
	_, err := Run(context.Background(), Config{NodePath: "node", AnalyzerPath: missing}, Request{Mode: "observed", Worktree: t.TempDir(), CanonicalRemote: "github.com/acme/payments"})
	if !errors.Is(err, ErrUnavailable) || !strings.Contains(err.Error(), "pnpm build") {
		t.Fatalf("missing analyzer = %v, want an unavailable error with a build hint", err)
	}
}

// TestLocateResolutionOrder protects requirement 7.1: the analyzer resolves
// from CONN_ANALYZER_PATH, then beside the conn executable, then the working
// directory, and Node.js from CONN_NODE_PATH before PATH.
func TestLocateResolutionOrder(t *testing.T) {
	withAnalyzer := func(t *testing.T) string {
		dir := t.TempDir()
		path := filepath.Join(dir, "packages", "typescript-analyzer", "dist", "cli.js")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("// analyzer\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	explicit := filepath.Join(withAnalyzer(t), "packages", "typescript-analyzer", "dist", "cli.js")
	executableDir, workingDir, emptyDir := withAnalyzer(t), withAnalyzer(t), t.TempDir()
	executable := filepath.Join(executableDir, "conn.exe")
	node := filepath.Join(t.TempDir(), "node.exe")
	if err := os.WriteFile(node, []byte("node"), 0o700); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name       string
		env        map[string]string
		executable string
		workingDir string
		wantSource string
		wantPath   string
		available  bool
	}{
		{"environment wins", map[string]string{AnalyzerPathEnv: explicit}, executable, workingDir, SourceAnalyzerEnv, explicit, true},
		{"explicit missing path is reported", map[string]string{AnalyzerPathEnv: filepath.Join(emptyDir, "missing.js")}, executable, workingDir, SourceAnalyzerEnv, filepath.Join(emptyDir, "missing.js"), false},
		{"executable directory", nil, executable, workingDir, SourceExecutableDir, filepath.Join(executableDir, "packages", "typescript-analyzer", "dist", "cli.js"), true},
		{"working directory", nil, filepath.Join(emptyDir, "conn.exe"), workingDir, SourceWorkingDir, filepath.Join(workingDir, "packages", "typescript-analyzer", "dist", "cli.js"), true},
		{"unavailable", nil, filepath.Join(emptyDir, "conn.exe"), emptyDir, "", filepath.Join(emptyDir, "packages", "typescript-analyzer", "dist", "cli.js"), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env := map[string]string{NodePathEnv: node}
			for key, value := range tc.env {
				env[key] = value
			}
			location := Locate(func(key string) (string, bool) { value, ok := env[key]; return value, ok }, tc.executable, tc.workingDir)
			if location.AnalyzerSource != tc.wantSource || location.AnalyzerAvailable != tc.available || !samePath(location.Config.AnalyzerPath, tc.wantPath) {
				t.Fatalf("location = %+v, want source %q path %q available %v", location, tc.wantSource, tc.wantPath, tc.available)
			}
			if location.NodeSource != SourceNodeEnv || !location.NodeAvailable || location.Config.NodePath != node {
				t.Fatalf("node = %+v, want %s from %s", location, node, SourceNodeEnv)
			}
		})
	}
}

func samePath(left, right string) bool {
	resolve := func(path string) string {
		dir, err := filepath.EvalSymlinks(filepath.Dir(path))
		if err != nil {
			return filepath.Clean(path)
		}
		return filepath.Join(dir, filepath.Base(path))
	}
	return strings.EqualFold(resolve(left), resolve(right))
}
