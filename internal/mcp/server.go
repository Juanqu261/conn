// Package mcp adapts Conn to Kiro through the official MCP Go SDK. It
// exposes only read-only tools, which call the same authenticated backend
// client as the CLI and never touch the database.
package mcp

import (
	"context"
	"errors"
	"io"
	"log/slog"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Juanqu261/conn/internal/httpclient"
)

// Config holds what the tools need.
type Config struct {
	Version string
	// Client returns a backend client authenticated with the human's Conn
	// session. It is called for every tool call, so a `conn login` during
	// the Kiro session takes effect without a restart.
	Client func() (*httpclient.Client, error)
	// DefaultProject is CONN_PROJECT from the Kiro MCP configuration. Tool
	// input may select another project the human belongs to.
	DefaultProject string
	Log            *slog.Logger
}

const instructions = "Conn tools read your team's shared Objective, Blueprint, charters, Intents, collision assessments, and Radar. " +
	"They may publish, revise, or release only the signed-in human's participant-owned Intents and may attach bounded reference-only evidence or non-binding resolution proposals. " +
	"Humans alone make binding decisions. Your human identity comes from the Conn session, not from tool input."

// Serve runs one MCP session over in and out until the client disconnects or
// ctx is cancelled. out must carry nothing but MCP messages.
func Serve(ctx context.Context, in io.ReadCloser, out io.WriteCloser, cfg Config) error {
	server := sdk.NewServer(&sdk.Implementation{Name: "conn", Version: cfg.Version}, &sdk.ServerOptions{
		Instructions: instructions,
		Logger:       cfg.Log,
	})
	registerTools(server, cfg)

	err := server.Run(ctx, &sdk.IOTransport{Reader: in, Writer: out})
	if err == nil || errors.Is(err, io.EOF) || errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}
