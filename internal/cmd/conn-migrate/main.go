// Command conn-migrate applies Conn's own schema migrations. It is
// development, CI, and deployment tooling (decision 0029) and is not part of
// the end-user conn CLI. It only applies pending migrations; it never rolls
// back or drops anything.
//
// Usage:
//
//	conn-migrate up          target CONN_DATABASE_URL
//	conn-migrate --dev up    target the local Docker Compose database
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"

	"github.com/Juanqu261/conn/internal/devenv"
	"github.com/Juanqu261/conn/internal/migrations"
	"github.com/Juanqu261/conn/internal/postgres"
)

const databaseURLEnv = "CONN_DATABASE_URL"

var errUsage = errors.New("usage: conn-migrate [--dev] up")

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx, os.Args[1:], os.Stderr)
	stop()
	switch {
	case errors.Is(err, errUsage):
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	case err != nil:
		fmt.Fprintf(os.Stderr, "conn-migrate: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stderr io.Writer) error {
	flags := flag.NewFlagSet("conn-migrate", flag.ContinueOnError)
	flags.SetOutput(stderr)
	dev := flags.Bool("dev", false, "target the local development database from compose.yaml")
	if err := flags.Parse(args); err != nil {
		return errUsage
	}
	if flags.NArg() != 1 || flags.Arg(0) != "up" {
		return errUsage
	}

	databaseURL, err := targetURL(*dev, stderr)
	if err != nil {
		return err
	}
	config, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		return fmt.Errorf("parse database URL: %w", err)
	}

	log := slog.New(slog.NewTextHandler(stderr, nil))
	// Name the target before changing it, without the password.
	log.Info("applying Conn migrations",
		"host", config.Host, "port", config.Port, "database", config.Database, "user", config.User)

	db := stdlib.OpenDB(*config)
	defer db.Close()

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		return fmt.Errorf("connect to %s:%d/%s: %w", config.Host, config.Port, config.Database, err)
	}

	applied, err := migrations.Up(ctx, db, log)
	if err != nil {
		return err
	}
	if len(applied) == 0 {
		log.Info("schema is up to date")
		return nil
	}
	for _, migration := range applied {
		log.Info("applied migration", "version", migration.Version, "file", migration.Name)
	}
	return nil
}

// targetURL requires exactly one explicit target so a missing variable never
// falls back to some other database.
func targetURL(dev bool, stderr io.Writer) (string, error) {
	fromEnv := os.Getenv(databaseURLEnv)
	switch {
	case dev && fromEnv != "":
		return "", fmt.Errorf("--dev and %s are both set; choose one target", databaseURLEnv)
	case dev:
		result, err := devenv.Load()
		if err != nil {
			return "", err
		}
		if result != nil {
			for _, warning := range result.Warnings {
				fmt.Fprintf(stderr, "conn-migrate: warning: %s\n", warning)
			}
		}
		return postgres.DevelopmentURL(), nil
	case fromEnv != "":
		return fromEnv, nil
	default:
		return "", fmt.Errorf("set %s or pass --dev", databaseURLEnv)
	}
}
