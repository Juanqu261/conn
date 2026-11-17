// Command conn is Conn's single executable. It constructs adapters and hands
// them to the CLI; it holds no command or product behavior itself.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/Juanqu261/conn/internal/cli"
	"github.com/Juanqu261/conn/internal/httpapi"
	"github.com/Juanqu261/conn/internal/mcp"
)

var version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := cli.Execute(ctx, os.Args[1:], cli.Streams{
		In:  os.Stdin,
		Out: os.Stdout,
		Err: os.Stderr,
	}, dependencies())
	stop()
	os.Exit(code)
}

func dependencies() cli.Dependencies {
	return cli.Dependencies{
		Version: version,
		Serve: func(ctx context.Context, cfg cli.ServerConfig, log *slog.Logger) error {
			return httpapi.Serve(ctx, cfg.ListenAddress, httpapi.NewHandler(), log)
		},
		ServeMCP: func(ctx context.Context, log *slog.Logger) error {
			// Standard output belongs to MCP alone. Hand the real stream to the
			// transport and point os.Stdout at standard error, so an accidental
			// print elsewhere in the process cannot corrupt the protocol.
			protocolOut := os.Stdout
			os.Stdout = os.Stderr
			return mcp.Serve(ctx, os.Stdin, protocolOut, version, log)
		},
	}
}
