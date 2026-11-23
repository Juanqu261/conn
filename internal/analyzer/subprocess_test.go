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
	run := func(body string, timeout time.Duration) error {
		_, err := Run(context.Background(), Config{NodePath: node, AnalyzerPath: script(body), Timeout: timeout}, Request{Worktree: worktree, CanonicalRemote: "github.com/acme/payments"})
		return err
	}

	t.Setenv("CONN_TOKEN", "conn_sess_not_for_analyzer")
	report, err := Run(context.Background(), Config{NodePath: node, AnalyzerPath: script(`console.log(JSON.stringify({report_version:1,repository:{canonical_remote:"github.com/acme/payments"},claims:[],diagnostics:[{code:"environment",message:process.env.CONN_TOKEN ? "token present" : "token absent"}]}));`), Timeout: time.Second}, Request{Worktree: worktree, CanonicalRemote: "github.com/acme/payments"})
	if err != nil {
		t.Fatal(err)
	}
	if got := report.Diagnostics[0].Message; got != "token absent" {
		t.Fatalf("analyzer inherited Conn token: %q", got)
	}

	if err := run(`console.log('{"report_version":1,"repository":{"canonical_remote":"github.com/acme/payments"},"claims":[],"diagnostics":[],"source":"do not accept source"}');`, time.Second); !errors.Is(err, ErrInvalid) {
		t.Fatalf("source field = %v, want invalid report", err)
	}
	if err := run(`console.log('{"report_version":1,"repository":{"canonical_remote":"github.com/acme/payments"},"claims":[],"diagnostics":[],"prompt":"do not accept prompts"}');`, time.Second); !errors.Is(err, ErrInvalid) {
		t.Fatalf("prompt field = %v, want invalid report", err)
	}
	if err := run(`console.log('{"report_version":1,"repository":{"canonical_remote":"github.com/acme/payments"},"claims":[{"subject_kind":"code_symbol","normalized_subject":"symbol:typescript:src/a.ts/value","display_name":"ghp_credential_value","relation":"affects","provenance_kind":"static_analysis"}],"diagnostics":[]}');`, time.Second); !errors.Is(err, ErrInvalid) {
		t.Fatalf("credential-shaped value = %v, want invalid report", err)
	}
	if err := run(`console.log(JSON.stringify({report_version:1,repository:{canonical_remote:"github.com/acme/payments"},claims:[],diagnostics:Array.from({length:101}, (_, i) => ({code:"TS"+i,message:"TypeScript diagnostic TS"+i}))}));`, time.Second); !errors.Is(err, ErrInvalid) {
		t.Fatalf("oversized diagnostics = %v, want invalid report", err)
	}
	if err := run(`setTimeout(() => {}, 2000);`, 20*time.Millisecond); !errors.Is(err, ErrTimedOut) {
		t.Fatalf("timeout = %v, want timeout", err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = Run(cancelled, Config{NodePath: node, AnalyzerPath: script(`setTimeout(() => {}, 2000);`)}, Request{Worktree: worktree, CanonicalRemote: "github.com/acme/payments"})
	if !errors.Is(err, context.Canceled) && !strings.Contains(err.Error(), "canceled") {
		t.Fatalf("cancellation = %v", err)
	}
}
