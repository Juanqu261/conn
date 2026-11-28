package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/Juanqu261/conn/internal/account"
	"github.com/Juanqu261/conn/internal/analyzer"
	"github.com/Juanqu261/conn/internal/auth/github"
	"github.com/Juanqu261/conn/internal/credentials"
	"github.com/Juanqu261/conn/internal/envvalue"
	"github.com/Juanqu261/conn/internal/hookstate"
	"github.com/Juanqu261/conn/internal/httpclient"
)

// ServerURLEnv selects the Conn server when --server is not given.
const ServerURLEnv = "CONN_SERVER_URL"

// errReported means the command already explained the failure.
var errReported = errors.New("reported")

// serverFlag adds --server and returns a function resolving the server URL:
// the flag, then CONN_SERVER_URL, then the local development server.
func serverFlag(cmd *cobra.Command, deps Dependencies) func() string {
	var flag string
	cmd.Flags().StringVar(&flag, "server", "", "Conn server URL (default $"+ServerURLEnv+" or "+httpclient.DefaultServerURL+")")
	return func() string {
		if flag != "" {
			return flag
		}
		if value, ok := envvalue.Lookup(deps.LookupEnv, ServerURLEnv); ok {
			return value
		}
		return httpclient.DefaultServerURL
	}
}

func newLoginCommand(deps Dependencies, streams Streams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Sign in with GitHub and store a Conn session",
		Long: "Sign in with GitHub device authorization.\n\n" +
			"Conn shows a one-time code to enter at github.com, then stores a 30-day\n" +
			"Conn session in the operating-system credential store. Conn keeps no\n" +
			"GitHub token and requests no GitHub permissions.",
		Args: cobra.NoArgs,
	}
	server := serverFlag(cmd, deps)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		service, err := deps.Account(server())
		if err != nil {
			return err
		}
		result, err := service.Login(cmd.Context(), func(a *github.DeviceAuthorization) {
			fmt.Fprintf(streams.Err, "Copy your one-time code: %s\n", a.UserCode)
			fmt.Fprintf(streams.Err, "Then open %s and enter it.\n", a.VerificationURI)
			fmt.Fprintln(streams.Err, "Waiting for authorization...")
		})
		if err != nil {
			return explain(err)
		}
		fmt.Fprintf(streams.Out, "Logged in to %s as @%s.\n", service.Server.URL(), result.Human.GitHubLogin)
		fmt.Fprintf(streams.Out, "Your Conn human ID is %s. Share it with a project owner to join a project.\n", result.Human.HumanID)
		fmt.Fprintf(streams.Out, "The session expires on %s.\n", result.ExpiresAt.Format("2006-01-02"))
		if result.EnvOverride {
			fmt.Fprintf(streams.Err, "Note: %s is set and takes precedence over this login until you unset it.\n", credentials.TokenEnv)
		}
		return nil
	}
	return cmd
}

func newLogoutCommand(deps Dependencies, streams Streams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "logout",
		Short: "Revoke the stored Conn session and remove it from this machine",
		Args:  cobra.NoArgs,
	}
	server := serverFlag(cmd, deps)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		service, err := deps.Account(server())
		if err != nil {
			return err
		}
		result, err := service.Logout(cmd.Context())
		if err != nil {
			return explain(err)
		}
		if result.RevokeErr != nil {
			return fmt.Errorf("removed the session from this machine, but the server could not revoke it (%v). It stops working when it expires", explain(result.RevokeErr))
		}
		fmt.Fprintf(streams.Out, "Logged out of %s.\n", service.Server.URL())
		if value, ok := deps.LookupEnv(credentials.TokenEnv); ok && value != "" {
			fmt.Fprintf(streams.Err, "Note: %s is still set; unset it to stop using that session.\n", credentials.TokenEnv)
		}
		return nil
	}
	return cmd
}

func newWhoAmICommand(deps Dependencies, streams Streams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "whoami",
		Short: "Show the signed-in human and their Conn human ID",
		Args:  cobra.NoArgs,
	}
	server := serverFlag(cmd, deps)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		service, err := deps.Account(server())
		if err != nil {
			return err
		}
		human, source, err := service.WhoAmI(cmd.Context())
		if source == credentials.SourceEnv && httpclient.IsCode(err, "unauthenticated") {
			return hint(err, "the session in "+credentials.TokenEnv+" has expired or was revoked. Unset it or replace it with a current session")
		}
		if err != nil {
			return explain(err)
		}
		fmt.Fprintf(streams.Out, "@%s\n", human.GitHubLogin)
		fmt.Fprintf(streams.Out, "Human ID:   %s\n", human.HumanID)
		fmt.Fprintf(streams.Out, "Server:     %s\n", service.Server.URL())
		fmt.Fprintf(streams.Out, "Credential: %s\n", source)
		return nil
	}
	return cmd
}

func newDoctorCommand(deps Dependencies, streams Streams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check the server, credential store, and session",
		Long: "Check the server, credential store, and session. No credential is printed.\n\n" +
			"Optional checks report local analysis, hook diagnostics, the conn version against the\n" +
			"Conn Kiro power's minimum, manual Kiro MCP registrations of conn mcp (read-only), the\n" +
			"Kiro steering and hook bundle, and the commit-trailer Git hook.",
		Args: cobra.NoArgs,
	}
	server := serverFlag(cmd, deps)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		service, err := deps.Account(server())
		if err != nil {
			return err
		}
		checks := service.Doctor(cmd.Context())
		if settings := deps.DevSettings; settings != nil {
			detail := fmt.Sprintf("%s: %d loaded, %d already set in the environment", settings.Path, len(settings.Loaded), len(settings.Overridden))
			checks = append([]account.Check{{Name: "Dev settings", OK: len(settings.Warnings) == 0, Detail: detail}}, checks...)
		}
		checks = append(checks, analyzerChecks(cmd.Context(), deps)...)
		checks = append(checks, hookDiagnosticsCheck(cmd.Context()))
		checks = append(checks, kiroChecks(cmd.Context(), deps)...)
		if !renderChecks(streams.Out, checks) {
			return errReported
		}
		return nil
	}
	return cmd
}

// analyzerChecks report local analysis availability. Analysis is optional:
// declared claims and collision warnings work without it.
func analyzerChecks(ctx context.Context, deps Dependencies) []account.Check {
	location := locateAnalyzer(deps)
	analyzerCheck := account.Check{Name: "Analyzer", Optional: true, OK: location.AnalyzerAvailable}
	if location.AnalyzerAvailable {
		analyzerCheck.Detail = "from " + location.AnalyzerSource
	} else {
		analyzerCheck.Detail = "not found; set " + analyzer.AnalyzerPathEnv + " or run pnpm build (local analysis is optional)"
	}
	nodeCheck := account.Check{Name: "Node.js", Optional: true, OK: location.NodeAvailable}
	if location.NodeAvailable {
		version := analyzer.NodeVersion(ctx, location.Config.NodePath)
		if version == "" {
			nodeCheck.OK, nodeCheck.Detail = false, "found on "+location.NodeSource+" but `node --version` failed"
		} else {
			nodeCheck.Detail = version + " from " + location.NodeSource
		}
	} else {
		nodeCheck.Detail = "not found; set " + analyzer.NodePathEnv + " or add node to PATH"
	}
	return []account.Check{analyzerCheck, nodeCheck}
}

// hookDiagnosticsCheck reports silent hook failures recorded in the current
// worktree's local hook state.
func hookDiagnosticsCheck(ctx context.Context) account.Check {
	check := account.Check{Name: "Hook diagnostics", Optional: true, OK: true, Detail: "none recorded"}
	store, err := hookstate.Open(ctx, ".")
	if err != nil {
		check.Detail = "not in a Git worktree"
		return check
	}
	diagnostics := store.Diagnostics()
	if len(diagnostics) == 0 {
		return check
	}
	last := diagnostics[len(diagnostics)-1]
	check.OK = false
	check.Detail = fmt.Sprintf("%d recent silent failure(s); last %s in %s at %s", len(diagnostics), last.Code, last.Hook, last.At.Format(time.RFC3339))
	return check
}

func renderChecks(out io.Writer, checks []account.Check) bool {
	healthy := true
	for _, check := range checks {
		status := "ok"
		switch {
		case !check.OK && check.Optional:
			status = "warn"
		case !check.OK:
			status, healthy = "fail", false
		}
		fmt.Fprintf(out, "[%-4s] %-17s %s\n", status, check.Name, check.Detail)
	}
	return healthy
}

// explain adds the next step to errors a human can fix. The original error
// stays in the chain, so exit classification still sees it.
// explained marks an error that already carries its next-step hint, so the
// root command does not repeat a hint that a subcommand added.
type explained struct{ error }

func (e explained) Unwrap() error { return e.error }

func explain(err error) error {
	if err == nil || errors.As(err, new(explained)) {
		return err
	}
	if hinted, ok := withHint(err); ok {
		return explained{hinted}
	}
	return err
}

// withHint adds the next step for errors that have one.
func withHint(err error) (error, bool) {
	hinted := hintFor(err)
	return hinted, hinted != nil
}

// hintFor returns err with its next step, or nil when it has none.
func hintFor(err error) error {
	switch {
	case errors.Is(err, credentials.ErrNotLoggedIn):
		return hint(err, "not logged in. Run `conn login`")
	case errors.Is(err, credentials.ErrUnavailable):
		return fmt.Errorf("%w.\nConn keeps its session only in the operating-system credential store and never in a plaintext file. "+
			"On Linux, install and unlock a Secret Service provider such as GNOME Keyring. For headless use, set %s", err, credentials.TokenEnv)
	case errors.Is(err, credentials.ErrInvalid):
		return fmt.Errorf("%w. Run `conn login` for a new session", err)
	case errors.Is(err, httpclient.ErrUnreachable):
		return fmt.Errorf("%w. Check that the server is running, or set --server or %s", err, ServerURLEnv)
	case httpclient.IsCode(err, "unauthenticated"):
		return hint(err, "the Conn session has expired or was revoked. Run `conn login`")
	case httpclient.IsCode(err, "login_not_configured"):
		return fmt.Errorf("%w Ask the server operator to set CONN_GITHUB_CLIENT_ID", err)
	case errors.Is(err, github.ErrAccessDenied), errors.Is(err, github.ErrExpired):
		return fmt.Errorf("%w. Run `conn login` to try again", err)
	case errors.Is(err, github.ErrDeviceFlowDisabled):
		return fmt.Errorf("%w. Enable Device Flow in the GitHub OAuth app settings", err)
	}
	return nil
}
