package cli

import (
	"errors"

	"github.com/spf13/cobra"

	"github.com/Juanqu261/conn/internal/hooks"
)

// hookCommand reports whether args invoke `conn hook`. Hook invocations
// never exit with a usage or failure code, because Kiro treats exit code 2
// from a pre-tool hook as a block.
func hookCommand(args []string) bool {
	return len(args) > 0 && args[0] == "hook"
}

func newHookCommand(deps Dependencies, streams Streams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hook",
		Short: "Run a warn-only Kiro command hook",
		Long: "Run one of Conn's optional Kiro command hooks. Kiro passes the hook event as JSON on\n" +
			"standard input. Hooks read from the Conn backend and never write to it. They never block,\n" +
			"deny, or ask for permission: on any failure they allow the event silently and record a\n" +
			"local diagnostic that conn doctor reports.\n\n" +
			"  session-start   summarize the focused Intent or recommend /conn-start\n" +
			"  prompt-submit   report new collisions and state changes since the last prompt\n" +
			"  pre-write       warn before a write that touches another Intent's claims\n" +
			"  post-write      record the written path, never its content\n\n" +
			"Install the hooks with `conn kiro integration install`.",
		Args: cobra.NoArgs,
	}
	for _, name := range hooks.Names {
		var warnExitCode int
		sub := &cobra.Command{
			Use:   name,
			Short: "Run the " + name + " hook",
			Args:  cobra.ArbitraryArgs,
			RunE: func(c *cobra.Command, _ []string) error {
				code := hooks.Run(c.Context(), name, hooks.Config{
					In: streams.In, Out: streams.Out, Err: streams.Err, LookupEnv: deps.LookupEnv,
					Store: deps.HookStore, WarnExitCode: warnExitCode,
				})
				if code != 0 {
					return exitWith{code: code}
				}
				return nil
			},
		}
		if name == hooks.PreWrite {
			sub.Flags().IntVar(&warnExitCode, "warn-exit-code", hooks.DefaultWarnExitCode,
				"exit code when a warning applies: 1 shows the warning without blocking; 0 only queues it")
		}
		sub.FParseErrWhitelist.UnknownFlags = true
		cmd.AddCommand(sub)
	}
	return cmd
}

// silenceHookFailure maps every hook error except its warning code to a
// silent success.
func silenceHookFailure(err error) int {
	var exit exitWith
	if errors.As(err, &exit) && exit.code == 1 {
		return 1
	}
	return ExitOK
}
