package cli

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/Juanqu261/conn/internal/kirointegration"
)

func newKiroCommand(streams Streams) *cobra.Command {
	var rawScope, root string
	cmd := &cobra.Command{
		Use:   "kiro",
		Short: "Manage optional, inspectable Kiro integration guidance",
		Long: "Preview, install, or remove Conn's optional Kiro steering and lifecycle prompt hooks.\n\n" +
			"These commands never edit MCP settings, agents, skills, or authoritative Conn state.\n" +
			"Installation refuses to overwrite an existing target file and preserves every unrelated entry.",
		Args: cobra.NoArgs,
	}
	integration := &cobra.Command{
		Use:   "integration",
		Short: "Manage optional Conn steering and lifecycle hooks",
		Args:  cobra.NoArgs,
	}
	integration.PersistentFlags().StringVar(&rawScope, "scope", string(kirointegration.ScopeWorkspace), "configuration scope: workspace or user")
	integration.PersistentFlags().StringVar(&root, "root", "", "explicit workspace root or user home override")

	plan := func(mode kirointegration.Mode) (kirointegration.Plan, error) {
		scope, err := parseKiroScope(rawScope)
		if err != nil {
			return kirointegration.Plan{}, usageError{err}
		}
		return kirointegration.Preview(scope, root, mode)
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
			if err := renderKiroPlan(streams.Out, proposed); err != nil {
				return err
			}
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
			if err := renderKiroPlan(streams.Out, proposed); err != nil {
				return err
			}
			if proposed.HasConflicts() {
				return errors.New("installation stopped: a Conn target already contains different content; inspect it and choose a different root or remove it manually")
			}
			if err := kirointegration.Apply(proposed); err != nil {
				return err
			}
			fmt.Fprintln(streams.Out, "Installed the optional Conn Kiro integration. Remove it with `conn kiro integration remove`.")
			return nil
		},
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
			if err := renderKiroPlan(streams.Out, proposed); err != nil {
				return err
			}
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
	cmd.AddCommand(integration)
	return cmd
}

func parseKiroScope(value string) (kirointegration.Scope, error) {
	scope := kirointegration.Scope(value)
	if scope != kirointegration.ScopeWorkspace && scope != kirointegration.ScopeUser {
		return "", errors.New("--scope must be workspace or user")
	}
	return scope, nil
}

func renderKiroPlan(out io.Writer, plan kirointegration.Plan) error {
	fmt.Fprintf(out, "Conn Kiro integration %s plan for %s scope at %s:\n", plan.Mode, plan.Scope, plan.Root)
	artifacts, err := kirointegration.Artifacts()
	if err != nil {
		return err
	}
	content := make(map[string][]byte, len(artifacts))
	for _, artifact := range artifacts {
		content[filepath.Clean(artifact.RelativePath)] = artifact.Content
	}
	for _, change := range plan.Changes {
		fmt.Fprintf(out, "  %-9s %s\n", change.Action, filepath.ToSlash(change.RelativePath))
		if plan.Mode == kirointegration.ModeInstall && (change.Action == kirointegration.ActionCreate || change.Action == kirointegration.ActionConflict) {
			fmt.Fprintf(out, "--- proposed %s ---\n%s--- end proposed %s ---\n", filepath.ToSlash(change.RelativePath), content[filepath.Clean(change.RelativePath)], filepath.ToSlash(change.RelativePath))
		}
	}
	for _, preserved := range plan.Preserved {
		fmt.Fprintf(out, "  preserve  %s\n", filepath.ToSlash(preserved))
	}
	return nil
}
