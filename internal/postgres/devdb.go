package postgres

import (
	"fmt"
	"os"
)

// DevelopmentPortEnv overrides the host port of the Docker Compose database.
const DevelopmentPortEnv = "CONN_DEV_DB_PORT"

// DevelopmentURL returns the URL of the local Docker Compose database
// defined in compose.yaml. Its credentials are development values, not
// secrets, and the port is bound to loopback.
func DevelopmentURL() string {
	port := os.Getenv(DevelopmentPortEnv)
	if port == "" {
		port = "55432"
	}
	return fmt.Sprintf("postgres://conn:conn-development-only@127.0.0.1:%s/conn?sslmode=disable", port)
}
