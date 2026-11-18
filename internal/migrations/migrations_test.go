package migrations_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5/stdlib"

	"github.com/Juanqu261/conn/internal/migrations"
	"github.com/Juanqu261/conn/internal/postgres/postgrestest"
)

// TestMigrationLifecycle protects requirement 9.2 and decision 0029: a fresh
// database is reported incompatible without being modified, the embedded
// migrations initialize it, reruns are no-ops, and the result is compatible.
func TestMigrationLifecycle(t *testing.T) {
	ctx := context.Background()
	pool := postgrestest.NewDatabase(t)
	db := stdlib.OpenDBFromPool(pool)
	t.Cleanup(func() { _ = db.Close() })
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))

	if err := migrations.Check(ctx, pool); !errors.Is(err, migrations.ErrIncompatibleSchema) {
		t.Fatalf("fresh database: got %v, want ErrIncompatibleSchema", err)
	}
	var versionTable bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass('goose_db_version') IS NOT NULL`).Scan(&versionTable); err != nil {
		t.Fatal(err)
	}
	if versionTable {
		t.Fatal("Check created the version table; it must only read")
	}

	applied, err := migrations.Up(ctx, db, quiet)
	if err != nil {
		t.Fatal(err)
	}
	want, err := migrations.EmbeddedVersions()
	if err != nil {
		t.Fatal(err)
	}
	var got []int64
	for _, migration := range applied {
		got = append(got, migration.Version)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("applied %v, want %v", got, want)
	}

	if again, err := migrations.Up(ctx, db, quiet); err != nil || len(again) != 0 {
		t.Fatalf("second run applied %d migrations, err %v", len(again), err)
	}
	if err := migrations.Check(ctx, pool); err != nil {
		t.Fatalf("migrated database: %v", err)
	}
}
