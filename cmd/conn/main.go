// Command conn is Conn's single executable. It constructs adapters and hands
// them to the CLI; it holds no command or product behavior itself.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/Juanqu261/conn/internal/account"
	"github.com/Juanqu261/conn/internal/application"
	"github.com/Juanqu261/conn/internal/auth/github"
	"github.com/Juanqu261/conn/internal/cli"
	"github.com/Juanqu261/conn/internal/credentials"
	"github.com/Juanqu261/conn/internal/devenv"
	"github.com/Juanqu261/conn/internal/httpapi"
	"github.com/Juanqu261/conn/internal/httpclient"
	"github.com/Juanqu261/conn/internal/mcp"
	"github.com/Juanqu261/conn/internal/migrations"
	"github.com/Juanqu261/conn/internal/postgres"
)

var version = "dev"

func main() {
	devSettings := loadDevSettings()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := cli.Execute(ctx, os.Args[1:], cli.Streams{
		In:  os.Stdin,
		Out: os.Stdout,
		Err: os.Stderr,
	}, dependencies(devSettings))
	stop()
	os.Exit(code)
}

func dependencies(devSettings *devenv.Result) cli.Dependencies {
	return cli.Dependencies{
		Version:     version,
		DevSettings: devSettings,
		LookupEnv:   os.LookupEnv,
		Serve:       serve,
		ServeMCP: func(ctx context.Context, cfg cli.MCPConfig, log *slog.Logger) error {
			service, err := accountService(cfg.ServerURL)
			if err != nil {
				return err
			}
			// Standard output belongs to MCP alone. Hand the real stream to the
			// transport and point os.Stdout at standard error, so an accidental
			// print elsewhere in the process cannot corrupt the protocol.
			protocolOut := os.Stdout
			os.Stdout = os.Stderr
			return mcp.Serve(ctx, os.Stdin, protocolOut, mcp.Config{
				Version:        version,
				Client:         service.Client,
				DefaultProject: cfg.DefaultProject,
				Log:            log,
			})
		},
		Account: accountService,
	}
}

// accountService returns local account operations against serverURL.
func accountService(serverURL string) (*account.Service, error) {
	httpClient := httpclient.NewHTTPClient()
	server, err := httpclient.New(serverURL, httpClient)
	if err != nil {
		return nil, err
	}
	return &account.Service{
		Server:    server,
		Store:     credentials.NewKeyring(server.URL()),
		LookupEnv: os.LookupEnv,
		GitHub:    github.Production,
		HTTP:      httpClient,
	}, nil
}

func loadDevSettings() *devenv.Result {
	if version != "dev" {
		return nil
	}
	result, err := devenv.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "conn: warning: %v\n", err)
		return nil
	}
	if result != nil {
		for _, warning := range result.Warnings {
			fmt.Fprintf(os.Stderr, "conn: warning: %s\n", warning)
		}
	}
	return result
}

func serve(ctx context.Context, cfg cli.ServerConfig, log *slog.Logger) error {
	if cfg.DatabaseURL == "" {
		return errors.New("CONN_DATABASE_URL is required")
	}
	if cfg.GitHubClientID == "" {
		log.Warn("CONN_GITHUB_CLIENT_ID is not set; conn login is unavailable")
	}
	pool, err := postgres.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	// conn server never changes the schema. It reports an incompatible schema
	// through readiness until deployment automation migrates the database.
	ready := func(ctx context.Context) error { return migrations.Check(ctx, pool) }
	if err := ready(ctx); err != nil {
		log.Warn("server is not ready; apply Conn migrations (pnpm db:migrate in development)", "error", err)
	} else {
		log.Info("database schema is compatible")
	}

	githubAPI := &github.API{HTTP: httpclient.NewHTTPClient(), Endpoints: github.Production}
	handler := httpapi.NewHandler(httpapi.Config{
		Ready:          ready,
		Auth:           &application.Auth{Store: postgres.AuthStore{Pool: pool}, VerifyToken: githubAPI.Verify},
		Projects:       &application.Projects{Store: postgres.ProjectStore{Pool: pool}},
		GitHubClientID: cfg.GitHubClientID,
		Log:            log,
	})
	return httpapi.Serve(ctx, cfg.ListenAddress, handler, log)
}
