package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// The test binary re-executes itself as `conn` when this variable is set, so
// subprocess tests exercise the real entry point without a separate build.
const runMainEnv = "CONN_TEST_RUN_MAIN"

func TestMain(m *testing.M) {
	if os.Getenv(runMainEnv) == "1" {
		main()
		return
	}
	os.Exit(m.Run())
}

// TestMCPStdio protects the Kiro integration contract (decision 0010): Kiro
// starts `conn mcp` as a subprocess, completes MCP initialization over
// standard input and output, and the process exits cleanly when the client
// disconnects. Task 12 extends this scenario with read-only tool calls.
func TestMCPStdio(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.Command(os.Args[0], "mcp")
	cmd.Env = append(os.Environ(), runMainEnv+"=1")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	client := sdk.NewClient(&sdk.Implementation{Name: "conn-test", Version: "0"}, nil)
	session, err := client.Connect(ctx, &sdk.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatalf("initialize: %v\nstderr:\n%s", err, stderr.String())
	}
	if name := session.InitializeResult().ServerInfo.Name; name != "conn" {
		t.Fatalf("server name = %q, want conn", name)
	}

	if err := session.Close(); err != nil {
		t.Fatalf("close: %v\nstderr:\n%s", err, stderr.String())
	}
	if cmd.ProcessState == nil || cmd.ProcessState.ExitCode() != 0 {
		t.Fatalf("conn mcp did not exit cleanly: %v\nstderr:\n%s", cmd.ProcessState, stderr.String())
	}
}
