// Package postgrestest gives each PostgreSQL test its own empty database.
//
// Tests use CONN_TEST_DATABASE_URL when it is set. Otherwise they use the
// local Docker Compose database when it is running, and skip with a hint
// when it is not. In CI the variable is required, so CI cannot silently skip
// persistence tests.
package postgrestest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Juanqu261/conn/internal/postgres"
)

// URLEnv names a server on which tests may create and drop databases.
const URLEnv = "CONN_TEST_DATABASE_URL"

// NewDatabase creates an empty database, returns a pool connected to it, and
// drops the database when the test ends.
func NewDatabase(t testing.TB) *pgxpool.Pool {
	t.Helper()
	adminURL := serverURL(t)

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
		t.Fatalf("connect to test server: %v", err)
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

func serverURL(t testing.TB) string {
	t.Helper()
	if url := os.Getenv(URLEnv); url != "" {
		return url
	}
	if os.Getenv("CI") != "" {
		t.Fatalf("%s must be set in CI", URLEnv)
	}
	if !developmentDatabaseReachable() {
		t.Skipf("PostgreSQL tests skipped: start the development database with `pnpm db:start`, or set %s", URLEnv)
	}
	return postgres.DevelopmentURL()
}

// developmentDatabaseReachable probes the Compose database once per test
// binary, with a short timeout so a stopped database costs about a second.
var developmentDatabaseReachable = sync.OnceValue(func() bool {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, postgres.DevelopmentURL())
	if err != nil {
		return false
	}
	_ = conn.Close(ctx)
	return true
})
