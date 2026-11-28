package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Juanqu261/conn/internal/kirointegration"
	"github.com/Juanqu261/conn/internal/kiropower"
)

func newKiroCommand(deps Dependencies, streams Streams) *cobra.Command {
	var rawScope, root string
	var commandOnPath bool
	cmd := &cobra.Command{
		Use:   "kiro",
		Short: "Manage optional, inspectable Kiro integration guidance",
		Long: "Preview, install, or remove Conn's optional Kiro steering and warn-only command hooks,\n" +
			"or export the Conn Kiro power into a folder you name.\n\n" +
			"These commands never edit MCP settings, agents, installed skills, or authoritative Conn state.\n" +
			"Installation refuses to overwrite an existing target file and preserves every unrelated entry.",
		Args: cobra.NoArgs,
	}
	integration := &cobra.Command{
		Use:   "integration",
		Short: "Manage optional Conn steering and command hooks",
		Long: "Manage optional Conn steering and four warn-only command hooks: SessionStart,\n" +
			"UserPromptSubmit, PreToolUse on writes, and PostToolUse on writes. The hooks run this conn\n" +
			"executable by absolute path, or `conn` from PATH with --command-on-path. An unchanged\n" +
			"version 1 agent-prompt hook file is removed during installation; a modified one is\n" +
			"preserved and reported as a conflict.",
		Args: cobra.NoArgs,
	}
	integration.PersistentFlags().StringVar(&rawScope, "scope", string(kirointegration.ScopeWorkspace), "configuration scope: workspace or user")
	integration.PersistentFlags().StringVar(&root, "root", "", "explicit workspace root or user home override")

	plan := func(mode kirointegration.Mode) (kirointegration.Plan, error) {
		scope, err := parseKiroScope(rawScope)
		if err != nil {
			return kirointegration.Plan{}, usageError{err}
		}
		command := kirointegration.PathCommand
		if mode == kirointegration.ModeInstall && !commandOnPath {
			if command, err = executableCommand(deps, streams.Err); err != nil {
				return kirointegration.Plan{}, err
			}
		}
		return kirointegration.Preview(scope, root, mode, command)
	}
	preview := &cobra.Command{
		Use:   "preview",
		Short: "Inspect existing files and preview installation without writing",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			proposed, err := plan(kirointegration.ModeInstall)
			if err != nil {
				return err
			}
			renderKiroPlan(streams.Out, proposed)
			fmt.Fprintln(streams.Out, "No files changed.")
			return nil
		},
	}
	install := &cobra.Command{
		Use:   "install",
		Short: "Explicitly install the optional bundle after showing its plan",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			proposed, err := plan(kirointegration.ModeInstall)
			if err != nil {
				return err
			}
			renderKiroPlan(streams.Out, proposed)
			if proposed.HasConflicts() {
				return errors.New("installation stopped: a Conn target already contains different content, or a modified version 1 hook file needs manual review; inspect it and choose a different root or remove it manually")
			}
			if err := kirointegration.Apply(proposed); err != nil {
				return err
			}
			fmt.Fprintln(streams.Out, "Installed the optional Conn Kiro integration. Remove it with `conn kiro integration remove`.")
			return nil
		},
	}
	for _, command := range []*cobra.Command{preview, install} {
		command.Flags().BoolVar(&commandOnPath, "command-on-path", false, "render hooks as `conn` from PATH instead of this executable's absolute path")
	}
	remove := &cobra.Command{
		Use:   "remove",
		Short: "Remove only unchanged Conn-installed integration files",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			proposed, err := plan(kirointegration.ModeRemove)
			if err != nil {
				return err
			}
			renderKiroPlan(streams.Out, proposed)
			if proposed.HasConflicts() {
				return errors.New("removal stopped: a Conn target was modified; it was preserved for manual review")
			}
			if err := kirointegration.Apply(proposed); err != nil {
				return err
			}
			fmt.Fprintln(streams.Out, "Removed unchanged Conn Kiro integration files. Unrelated Kiro configuration was preserved.")
			return nil
		},
	}
	integration.AddCommand(preview, install, remove)
	cmd.AddCommand(integration, newKiroPowerCommand(streams))
	return cmd
}

func newKiroPowerCommand(streams Streams) *cobra.Command {
	power := &cobra.Command{
		Use:   "power",
		Short: "Render the Conn Kiro power",
		Long: "Render the Conn Kiro power in the Agent Plugins format: plugin.json, mcp.json, and the\n" +
			"conn-setup, conn-work-loop, and conn-review-proof skills. Kiro installs the power from a\n" +
			"repository URL or a local folder and manages its MCP server; Conn never writes MCP\n" +
			"configuration. The always-on steering and warn-only hooks stay workspace files that\n" +
			"`conn kiro integration install` writes.",
		Args: cobra.NoArgs,
	}
	var replace bool
	export := &cobra.Command{
		Use:   "export <dir>",
		Short: "Write the rendered power into a folder you name",
		Long: "Write the rendered power and its .digest into <dir>. Without --replace, a file with\n" +
			"different content is a conflict and nothing is written. Export never deletes a file.",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			changes, err := kiropower.Export(args[0], replace)
			fmt.Fprintf(streams.Out, "Conn Kiro power %s export to %s:\n", kiropower.Version, filepath.ToSlash(args[0]))
			for _, change := range changes {
				fmt.Fprintf(streams.Out, "  %-9s %s\n", change.Action, change.RelativePath)
			}
			if err != nil {
				return err
			}
			fmt.Fprintln(streams.Out, "Install it in Kiro with Add Custom Power, from this folder or its repository URL.")
			return nil
		},
	}
	export.Flags().BoolVar(&replace, "replace", false, "overwrite Conn's power files that hold different content")
	power.AddCommand(export)
	return power
}

// executableCommand renders the absolute path of the running conn
// executable. A temporary `go run` binary would disappear, so it warns.
func executableCommand(deps Dependencies, diagnostics io.Writer) (string, error) {
	resolve := deps.Executable
	if resolve == nil {
		resolve = os.Executable
	}
	executable, err := resolve()
	if err != nil || executable == "" {
		return "", errors.New("the conn executable path is unavailable; pass --command-on-path to run conn from PATH")
	}
	if resolved, err := filepath.EvalSymlinks(executable); err == nil {
		executable = resolved
	}
	if temp, err := filepath.EvalSymlinks(os.TempDir()); err == nil {
		if relative, err := filepath.Rel(temp, executable); err == nil && !strings.HasPrefix(relative, "..") {
			fmt.Fprintln(diagnostics, "Note: this conn executable is in a temporary directory, which go run deletes. Build conn first or pass --command-on-path.")
		}
	}
	return kirointegration.RenderCommand(executable), nil
}

func parseKiroScope(value string) (kirointegration.Scope, error) {
	scope := kirointegration.Scope(value)
	if scope != kirointegration.ScopeWorkspace && scope != kirointegration.ScopeUser {
		return "", errors.New("--scope must be workspace or user")
	}
	return scope, nil
}

func renderKiroPlan(out io.Writer, plan kirointegration.Plan) {
	fmt.Fprintf(out, "Conn Kiro integration %s plan for %s scope at %s:\n", plan.Mode, plan.Scope, plan.Root)
	if plan.Mode == kirointegration.ModeInstall {
		fmt.Fprintf(out, "  hook command: %s\n", plan.Command)
	}
	for _, change := range plan.Changes {
		fmt.Fprintf(out, "  %-9s %s\n", change.Action, filepath.ToSlash(change.RelativePath))
		if plan.Mode == kirointegration.ModeInstall && (change.Action == kirointegration.ActionCreate || change.Action == kirointegration.ActionReplace || change.Action == kirointegration.ActionConflict) {
			if content := plan.Content(change.RelativePath); content != nil {
				fmt.Fprintf(out, "--- proposed %s ---\n%s--- end proposed %s ---\n", filepath.ToSlash(change.RelativePath), content, filepath.ToSlash(change.RelativePath))
			}
		}
		if filepath.Clean(change.RelativePath) == filepath.Clean(kirointegration.LegacyHooksTarget) {
			switch change.Action {
			case kirointegration.ActionRemove:
				fmt.Fprintln(out, "            (unchanged version 1 agent-prompt hooks; replaced by command hooks)")
			case kirointegration.ActionConflict:
				fmt.Fprintln(out, "            (modified version 1 agent-prompt hooks; preserved for manual review)")
			}
		}
	}
	for _, preserved := range plan.Preserved {
		fmt.Fprintf(out, "  preserve  %s\n", filepath.ToSlash(preserved))
	}
}
