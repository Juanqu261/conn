// Package migrations holds Conn's own PostgreSQL schema migrations
package migrations

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"

	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
)

//go:embed sql
var embedded embed.FS

// Source returns the embedded SQL migrations.
func Source() fs.FS {
	sub, err := fs.Sub(embedded, "sql")
	if err != nil {
		panic(err) // The embedded directory is fixed at build time.
	}
	return sub
}

// Applied describes one migration applied by Up.
type Applied struct {
	Version int64
	Name    string
}

// Up applies every pending migration in order. A PostgreSQL advisory lock
// serializes concurrent runners, for example two deployment jobs.
func Up(ctx context.Context, db *sql.DB, log *slog.Logger) ([]Applied, error) {
	provider, err := newProvider(db, log)
	if errors.Is(err, goose.ErrNoMigrations) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	results, err := provider.Up(ctx)
	if err != nil {
		return nil, fmt.Errorf("apply migrations: %w", err)
	}
	applied := make([]Applied, 0, len(results))
	for _, result := range results {
		applied = append(applied, Applied{Version: result.Source.Version, Name: result.Source.Path})
	}
	return applied, nil
}

func newProvider(db *sql.DB, log *slog.Logger) (*goose.Provider, error) {
	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return nil, err
	}
	return goose.NewProvider(goose.DialectPostgres, db, Source(),
		goose.WithSessionLocker(locker),
		goose.WithDisableGlobalRegistry(true),
		goose.WithSlog(log),
	)
}
