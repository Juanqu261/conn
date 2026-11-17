// Package mcp adapts Conn to Kiro through the official MCP Go SDK
// (decision 0013). SDK types stay inside this package.
package mcp

import (
	"context"
	"errors"
	"io"
	"log/slog"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Serve runs one MCP session over in and out until the client disconnects or
// ctx is cancelled. out must carry nothing but MCP messages.
func Serve(ctx context.Context, in io.ReadCloser, out io.WriteCloser, version string, log *slog.Logger) error {
	server := sdk.NewServer(&sdk.Implementation{Name: "conn", Version: version}, &sdk.ServerOptions{
		Logger: log,
	})

	err := server.Run(ctx, &sdk.IOTransport{Reader: in, Writer: out})
	if err == nil || errors.Is(err, io.EOF) || errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}
