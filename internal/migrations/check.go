package migrations

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"slices"

	"github.com/jackc/pgx/v5"
	"github.com/pressly/goose/v3"
)

// ErrIncompatibleSchema means the database's applied migrations differ from
// the migrations embedded in this build.
var ErrIncompatibleSchema = errors.New("database schema is incompatible with this build")

// Querier is the read-only subset of pgx used by Check.
type Querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Check reports whether the database has exactly the migrations embedded in
// this build: none pending, none missing, none newer. It only reads. Goose's
// own version queries create the version table when it is missing, so Check
// reads that table directly instead.
func Check(ctx context.Context, db Querier) error {
	want, err := EmbeddedVersions()
	if err != nil {
		return err
	}

	var tableExists bool
	if err := db.QueryRow(ctx, `SELECT to_regclass('goose_db_version') IS NOT NULL`).Scan(&tableExists); err != nil {
		return fmt.Errorf("check schema version: %w", err)
	}
	if !tableExists {
		return fmt.Errorf("%w: no Conn migrations are applied; this build expects versions %v", ErrIncompatibleSchema, want)
	}

	// A version is applied when its most recent row says so.
	rows, err := db.Query(ctx, `
		SELECT version_id FROM (
			SELECT DISTINCT ON (version_id) version_id, is_applied
			FROM goose_db_version
			WHERE version_id > 0
			ORDER BY version_id, id DESC
		) latest
		WHERE is_applied
		ORDER BY version_id`)
	if err != nil {
		return fmt.Errorf("check schema version: %w", err)
	}
	have, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return fmt.Errorf("check schema version: %w", err)
	}

	if !slices.Equal(have, want) {
		return fmt.Errorf("%w: database has versions %v; this build expects %v", ErrIncompatibleSchema, have, want)
	}
	return nil
}

// EmbeddedVersions lists the versions of the embedded migrations in order.
func EmbeddedVersions() ([]int64, error) {
	files, err := fs.Glob(Source(), "*.sql")
	if err != nil {
		return nil, err
	}
	versions := make([]int64, 0, len(files))
	for _, file := range files {
		version, err := goose.NumericComponent(file)
		if err != nil {
			return nil, fmt.Errorf("migration %s: %w", file, err)
		}
		versions = append(versions, version)
	}
	slices.Sort(versions)
	return versions, nil
}
