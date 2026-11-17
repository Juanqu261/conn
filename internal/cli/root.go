// Package cli holds Conn's Cobra commands. Commands parse input, call an
// injected operation, and render the result. They contain no domain,
// database, or HTTP behavior (decision 0028).
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"

	"github.com/spf13/cobra"
)

// Streams are the process standard streams.
type Streams struct {
	In  io.Reader
	Out io.Writer
	Err io.Writer
}

// ServerConfig configures `conn server`.
type ServerConfig struct {
	ListenAddress string
}

// Dependencies are the operations commands invoke. The process entry point
// constructs them so this package never depends on adapter packages.
type Dependencies struct {
	Version string

	// Serve runs the backend HTTP service until ctx is cancelled.
	Serve func(ctx context.Context, cfg ServerConfig, log *slog.Logger) error

	// ServeMCP runs the Kiro MCP adapter over standard input and output
	// until the client disconnects or ctx is cancelled.
	ServeMCP func(ctx context.Context, log *slog.Logger) error
}

// Execute runs one command line and returns the process exit code.
// Diagnostics always go to streams.Err.
func Execute(ctx context.Context, args []string, streams Streams, deps Dependencies) int {
	root := newRoot(streams, deps)
	root.SetArgs(args)
	if err := root.ExecuteContext(ctx); err != nil {
		if errors.Is(err, context.Canceled) {
			return 0
		}
		fmt.Fprintf(streams.Err, "conn: %v\n", err)
		return 1
	}
	return 0
}

func newRoot(streams Streams, deps Dependencies) *cobra.Command {
	root := &cobra.Command{
		Use:           "conn",
		Short:         "Coordinate human-agent work cells around a shared blueprint",
		Version:       deps.Version,
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	root.SetIn(streams.In)
	root.SetOut(streams.Out)
	root.SetErr(streams.Err)
	root.CompletionOptions.DisableDefaultCmd = true

	logger := slog.New(slog.NewTextHandler(streams.Err, nil))
	root.AddCommand(
		newServerCommand(deps, logger),
		newMCPCommand(deps, logger),
	)
	return root
}

func newServerCommand(deps Dependencies, logger *slog.Logger) *cobra.Command {
	cfg := ServerConfig{}
	cmd := &cobra.Command{
		Use:   "server",
		Short: "Run the Conn backend HTTP service",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return deps.Serve(cmd.Context(), cfg, logger)
		},
	}
	cmd.Flags().StringVar(&cfg.ListenAddress, "listen", "127.0.0.1:8080", "TCP address to listen on")
	return cmd
}

func newMCPCommand(deps Dependencies, logger *slog.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Run the Kiro MCP adapter over standard input and output",
		Long: "Run the Kiro MCP adapter over standard input and output.\n\n" +
			"Standard output carries only MCP messages; diagnostics go to standard error.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return deps.ServeMCP(cmd.Context(), logger)
		},
	}
}
