// Package mcp adapts Conn to Kiro through the official MCP Go SDK. It
// exposes authenticated tools, guided prompts, optional focused resources,
// direct account interactions, process-local Project selection, generated
// handoffs, and bounded local analysis over stdio. It calls the same backend
// and account boundaries as the CLI and never touches the database.
package mcp

import (
	"context"
	"errors"
	"io"
	"log/slog"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Juanqu261/conn/internal/account"
	"github.com/Juanqu261/conn/internal/analyzer"
	"github.com/Juanqu261/conn/internal/httpclient"
	"github.com/Juanqu261/conn/internal/repositorygit"
)

// Config holds what the tools need.
type Config struct {
	Version string
	// Account owns authentication, credential precedence, keyring access, and
	// logout behavior used by interactive MCP account tools.
	Account *account.Service
	// Client returns a backend client authenticated with the human's Conn
	// session. It is called for every tool call, so a login during the Kiro
	// session takes effect without a restart.
	Client func() (*httpclient.Client, error)
	// DefaultProject is CONN_PROJECT from the Kiro MCP configuration. Tool
	// input may select another project the human belongs to.
	DefaultProject string
	// AnalyzerAvailable reports whether the local Node.js analyzer artifact is
	// available. It performs no analysis and reads no credential.
	AnalyzerAvailable func() bool
	// AnalyzerLocation, when set, reports the resolved analyzer and Node.js
	// sources for diagnostics. It takes precedence over AnalyzerAvailable.
	AnalyzerLocation func() analyzer.Location
	// Analyzer configures the existing bounded Node.js subprocess adapter.
	// Its program path is trusted local Conn configuration, never tool input.
	Analyzer analyzer.Config
	// WorkspaceRoot is the trusted local repository worktree. Empty means the
	// MCP process working directory. Requested worktrees and paths cannot escape it.
	WorkspaceRoot string
	// DiscoverRepository reads normalized, credential-free Git identity from
	// the current worktree for Project matching. Nil uses the process worktree.
	DiscoverRepository func(context.Context) (repositorygit.Metadata, error)
	// DiscoverRepositoryAt reads normalized, credential-free Git identity from
	// the trusted analysis worktree. Nil uses repositorygit.Discover.
	DiscoverRepositoryAt func(context.Context, string) (repositorygit.Metadata, error)
	Log                  *slog.Logger
}

const instructions = "Conn tells you, before you edit, when other in-progress work changes the same behavior, contract, or data. " +
	"Before editing, call list_concepts and suggest_claims for the planned paths, publish or revise your Intent with the chosen claims, and call check_collisions. " +
	"Tools read your team's Objectives, optional Blueprints and Charters, Intents, collision assessments, and Radar. " +
	"They may publish, revise, or release only the signed-in human's participant-owned Intents and may attach bounded reference-only evidence or non-binding resolution proposals. " +
	"Before review, read_review_packet states what the change intended, touched, and verified; observed check runs come only from `conn evidence run` in the shell, with the human's approval. " +
	"Humans alone make binding decisions. Your human identity comes from the Conn session, not from tool input."

// Serve runs one MCP session over in and out until the client disconnects or
// ctx is cancelled. out must carry nothing but MCP messages.
func Serve(ctx context.Context, in io.ReadCloser, out io.WriteCloser, cfg Config) error {
	server := sdk.NewServer(&sdk.Implementation{Name: "conn", Version: cfg.Version}, &sdk.ServerOptions{
		Instructions: instructions,
		Logger:       cfg.Log,
	})
	interactions := registerTools(server, cfg)
	registerWorkflows(server, cfg)
	defer interactions.clear()

	err := server.Run(ctx, &sdk.IOTransport{Reader: in, Writer: out})
	if err == nil || errors.Is(err, io.EOF) || errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}
