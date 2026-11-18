// Package cli holds Conn's Cobra commands. Commands parse input, call an
// injected operation, and render the result. They contain no domain,
// database, or HTTP behavior.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"

	"github.com/spf13/cobra"

	"github.com/Juanqu261/conn/internal/account"
	"github.com/Juanqu261/conn/internal/devenv"
)

// Streams are the process standard streams.
type Streams struct {
	In  io.Reader
	Out io.Writer
	Err io.Writer
}

// ServerConfig configures `conn server`. The database URL contains a
// credential, so it comes from the environment and never from a flag.
type ServerConfig struct {
	ListenAddress  string
	DatabaseURL    string
	GitHubClientID string
}

// Dependencies are the operations commands invoke. The process entry point
// constructs them, so commands never build adapters themselves.
type Dependencies struct {
	Version     string
	DevSettings *devenv.Result

	// LookupEnv reads configuration from the environment.
	LookupEnv func(key string) (string, bool)

	// Serve runs the backend HTTP service until ctx is cancelled.
	Serve func(ctx context.Context, cfg ServerConfig, log *slog.Logger) error

	// ServeMCP runs the Kiro MCP adapter over standard input and output
	// until the client disconnects or ctx is cancelled.
	ServeMCP func(ctx context.Context, log *slog.Logger) error

	// Account returns local account operations against serverURL.
	Account func(serverURL string) (*account.Service, error)
}

// Execute runs one command line and returns the process exit code.
// Diagnostics always go to streams.Err.
func Execute(ctx context.Context, args []string, streams Streams, deps Dependencies) int {
	root := newRoot(streams, deps)
	root.SetArgs(args)
	err := root.ExecuteContext(ctx)
	switch {
	case err == nil:
		return 0
	case errors.Is(err, errReported):
		return 1
	case errors.Is(err, context.Canceled):
		fmt.Fprintln(streams.Err, "conn: interrupted")
		return 130
	default:
		fmt.Fprintf(streams.Err, "conn: %v\n", err)
		return 1
	}
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
		newLoginCommand(deps, streams),
		newLogoutCommand(deps, streams),
		newWhoAmICommand(deps, streams),
		newDoctorCommand(deps, streams),
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
		Long: "Run the Conn backend HTTP service.\n\n" +
			"Environment:\n" +
			"  CONN_DATABASE_URL      PostgreSQL connection URL (required)\n" +
			"  CONN_GITHUB_CLIENT_ID  public GitHub OAuth client ID that enables conn login\n\n" +
			"The server never changes the database schema. GET /readyz reports\n" +
			"not ready until the schema matches this build.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg.DatabaseURL, _ = deps.LookupEnv("CONN_DATABASE_URL")
			cfg.GitHubClientID, _ = deps.LookupEnv("CONN_GITHUB_CLIENT_ID")
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
