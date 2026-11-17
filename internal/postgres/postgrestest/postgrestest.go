// Package postgrestest gives each PostgreSQL test its own empty database.
//
// Tests run only when CONN_TEST_DATABASE_URL names a server the test may
// create databases on, such as the development database. Without it they
// skip locally and fail in CI, so CI cannot silently skip persistence tests.
package postgrestest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// URLEnv names the server used for PostgreSQL tests.
const URLEnv = "CONN_TEST_DATABASE_URL"

// NewDatabase creates an empty database, returns a pool connected to it, and
// drops the database when the test ends.
func NewDatabase(t testing.TB) *pgxpool.Pool {
	t.Helper()
	adminURL := os.Getenv(URLEnv)
	if adminURL == "" {
		if os.Getenv("CI") != "" {
			t.Fatalf("%s must be set in CI", URLEnv)
		}
		t.Skipf("set %s to run PostgreSQL tests", URLEnv)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	suffix := make([]byte, 6)
	if _, err := rand.Read(suffix); err != nil {
		t.Fatal(err)
	}
	// Lowercase letters, digits, and underscores only, so the name is a
	// valid unquoted identifier.
	database := "conn_test_" + hex.EncodeToString(suffix)
	name := pgx.Identifier{database}.Sanitize()

	admin, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		t.Fatalf("connect to %s: %v", URLEnv, err)
	}
	defer admin.Close(ctx)
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatalf("create test database: %v", err)
	}

	config, err := pgxpool.ParseConfig(adminURL)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.Database = database
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		pool.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		admin, err := pgx.Connect(ctx, adminURL)
		if err != nil {
			t.Errorf("drop test database: %v", err)
			return
		}
		defer admin.Close(ctx)
		if _, err := admin.Exec(ctx, "DROP DATABASE "+name+" WITH (FORCE)"); err != nil {
			t.Errorf("drop test database: %v", err)
		}
	})
	return pool
}
