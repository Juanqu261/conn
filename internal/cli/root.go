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

// ProjectEnv selects the default project for `conn mcp`.
const ProjectEnv = "CONN_PROJECT"

// MCPConfig configures `conn mcp`.
type MCPConfig struct {
	ServerURL string
	// DefaultProject is CONN_PROJECT; tool input may override it.
	DefaultProject string
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
	ServeMCP func(ctx context.Context, cfg MCPConfig, log *slog.Logger) error

	// Account returns local account operations against serverURL.
	Account func(serverURL string) (*account.Service, error)

	// RunApp starts the interactive terminal application against serverURL.
	RunApp func(ctx context.Context, in io.Reader, out, err io.Writer, serverURL string) error
}

// Execute runs one command line and returns the process exit code.
// Results go to streams.Out; diagnostics always go to streams.Err.
func Execute(ctx context.Context, args []string, streams Streams, deps Dependencies) int {
	root := newRoot(streams, deps)
	started := false
	markStarted(root, &started)
	root.SetArgs(args)
	err := root.ExecuteContext(ctx)
	switch {
	case err == nil:
		return ExitOK
	case errors.Is(err, errReported):
		return ExitFailure
	}
	code := exitCode(err, started)
	if code == ExitInterrupted {
		fmt.Fprintln(streams.Err, "conn: interrupted")
	} else {
		fmt.Fprintf(streams.Err, "conn: %v\n", explain(err))
	}
	return code
}

// markStarted wraps every command's RunE to record that the command's own
// logic began. Errors raised before that come from Cobra's parsing of
// commands, arguments, and flags, and are usage errors.
func markStarted(cmd *cobra.Command, started *bool) {
	if run := cmd.RunE; run != nil {
		cmd.RunE = func(c *cobra.Command, args []string) error {
			*started = true
			return run(c, args)
		}
	}
	for _, child := range cmd.Commands() {
		markStarted(child, started)
	}
}

func newRoot(streams Streams, deps Dependencies) *cobra.Command {
	root := &cobra.Command{
		Use:   "conn",
		Short: "Coordinate human-agent work cells around a shared blueprint",
		Long: "Coordinate human-agent work cells around a shared blueprint.\n\n" +
			"Structured commands accept --json and then print {\"data\": ..., \"as_of_event\": n}\n" +
			"on standard output. Diagnostics always go to standard error.\n\n" + exitCodeHelp,
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
		newProjectCommand(deps, streams),
		newRepositoryCommand(deps, streams),
		newIntentCommand(deps, streams),
		newObjectiveCommand(deps, streams),
		newBlueprintCommand(deps, streams),
		newCharterCommand(deps, streams),
		newStatusCommand(deps, streams),
		newSyncCommand(deps, streams),
		newDoctorCommand(deps, streams),
		newAppCommand(deps, streams),
		newServerCommand(deps, logger),
		newMCPCommand(deps, logger),
	)
	return root
}

func newAppCommand(deps Dependencies, streams Streams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "app",
		Short: "Open the interactive Conn terminal application",
		Long: "Open Conn's keyboard-first terminal application. It reads your projects, plan, team,\n" +
			"updates, and account from the Conn server. Use the regular conn commands for scripts.\n\n" +
			"The app requires an interactive input and output terminal.",
		Args: cobra.NoArgs,
	}
	server := serverFlag(cmd, deps)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		if deps.RunApp == nil {
			return errors.New("the interactive Conn application is not configured")
		}
		return deps.RunApp(cmd.Context(), streams.In, streams.Out, streams.Err, server())
	}
	return cmd
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
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Run the Kiro MCP adapter over standard input and output",
		Long: "Run the Kiro MCP adapter over standard input and output.\n\n" +
			"It exposes read-only tools: read_objective, read_blueprint, read_my_charter,\n" +
			"and check_updates. They act as the human signed in with conn login, or\n" +
			"CONN_TOKEN when set.\n\n" +
			"Environment:\n" +
			"  CONN_SERVER_URL  Conn server URL\n" +
			"  CONN_PROJECT     default project ID for tool calls\n\n" +
			"Standard output carries only MCP messages; diagnostics go to standard error.",
		Args: cobra.NoArgs,
	}
	server := serverFlag(cmd, deps)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		project, _ := deps.LookupEnv(ProjectEnv)
		return deps.ServeMCP(cmd.Context(), MCPConfig{ServerURL: server(), DefaultProject: project}, logger)
	}
	return cmd
}
