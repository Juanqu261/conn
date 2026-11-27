package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"uuid"

	"github.com/spf13/cobra"

	"github.com/Juanqu261/conn/internal/agentguide"
	"github.com/Juanqu261/conn/internal/protocol"
)

func newAgentsCommand(deps Dependencies, streams Streams) *cobra.Command {
	agents := group("agents", "Manage the Conn section of AGENTS.md for coding agents other than Kiro")
	agents.Long = "Manage one marker-delimited Conn section in AGENTS.md, or another Markdown file in the\n" +
		"repository root, for MCP coding agents other than Kiro. The section comes from the same source\n" +
		"as the Kiro steering. Content outside the markers is never changed, and a modified section is\n" +
		"preserved and reported as a conflict. Preview writes nothing."
	var root, file string
	plan := func(mode agentguide.Mode) (agentguide.Plan, error) {
		directory := root
		if directory == "" {
			directory = "."
		}
		absolute, err := filepath.Abs(directory)
		if err != nil {
			return agentguide.Plan{}, usageError{err}
		}
		proposed, err := agentguide.Preview(absolute, file, mode)
		if err != nil {
			return agentguide.Plan{}, usageError{err}
		}
		return proposed, nil
	}
	render := func(out io.Writer, plan agentguide.Plan) {
		fmt.Fprintf(out, "Conn agent guidance %s plan:\n  %-9s %s\n", plan.Mode, plan.Action, plan.Path)
		if plan.Reason != "" {
			fmt.Fprintf(out, "  (%s)\n", plan.Reason)
		}
		if plan.Mode == agentguide.ModeInstall && (plan.Action == agentguide.ActionCreateFile || plan.Action == agentguide.ActionAppend || plan.Action == agentguide.ActionReplace || plan.Action == agentguide.ActionConflict) {
			fmt.Fprintf(out, "--- proposed Conn section ---\n%s--- end proposed Conn section ---\n", agentguide.Section())
		}
	}
	preview := &cobra.Command{Use: "preview", Short: "Show the Conn section plan without writing", Args: cobra.NoArgs, RunE: func(*cobra.Command, []string) error {
		proposed, err := plan(agentguide.ModeInstall)
		if err != nil {
			return err
		}
		render(streams.Out, proposed)
		fmt.Fprintln(streams.Out, "No files changed. Configure the MCP server with `conn mcp config --client generic`.")
		return nil
	}}
	install := &cobra.Command{Use: "install", Short: "Add or update the Conn section after showing it", Args: cobra.NoArgs, RunE: func(*cobra.Command, []string) error {
		proposed, err := plan(agentguide.ModeInstall)
		if err != nil {
			return err
		}
		render(streams.Out, proposed)
		if proposed.Action == agentguide.ActionConflict {
			return errors.New("installation stopped: the Conn section was modified and was preserved for manual review")
		}
		if err := agentguide.Apply(proposed); err != nil {
			return err
		}
		fmt.Fprintln(streams.Out, "Installed the Conn section. Remove it with `conn agents remove`.")
		return nil
	}}
	remove := &cobra.Command{Use: "remove", Short: "Remove the Conn section only when it is unchanged", Args: cobra.NoArgs, RunE: func(*cobra.Command, []string) error {
		proposed, err := plan(agentguide.ModeRemove)
		if err != nil {
			return err
		}
		render(streams.Out, proposed)
		if proposed.Action == agentguide.ActionConflict {
			return errors.New("removal stopped: the Conn section was modified and was preserved for manual review")
		}
		if err := agentguide.Apply(proposed); err != nil {
			return err
		}
		if proposed.Action != agentguide.ActionAbsent {
			fmt.Fprintln(streams.Out, "Removed the Conn section. Content outside the markers was preserved.")
		}
		return nil
	}}
	agents.PersistentFlags().StringVar(&file, "file", agentguide.DefaultFile, "Markdown instruction file in the root")
	agents.PersistentFlags().StringVar(&root, "root", "", "repository root (default: the current directory)")
	agents.AddCommand(preview, install, remove)
	return agents
}

func newMCPConfigCommand(deps Dependencies, streams Streams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Print the MCP configuration for a coding agent without writing it",
		Long: "Print a stdio MCP server configuration for conn. --client generic prints an mcpServers JSON\n" +
			"snippet that most MCP clients accept; --client kiro prints the kiro-cli mcp add command.\n" +
			"Conn never writes MCP client configuration.",
		Args: cobra.NoArgs,
	}
	server := serverFlag(cmd, deps)
	var client string
	cmd.Flags().StringVar(&client, "client", "generic", "client: generic or kiro")
	cmd.RunE = func(*cobra.Command, []string) error {
		resolve := deps.Executable
		if resolve == nil {
			resolve = os.Executable
		}
		executable, err := resolve()
		if err != nil || executable == "" {
			return errors.New("the conn executable path is unavailable")
		}
		if resolved, err := filepath.EvalSymlinks(executable); err == nil {
			executable = resolved
		}
		switch client {
		case "generic":
			document := map[string]any{"mcpServers": map[string]any{"conn": map[string]any{
				"command": executable, "args": []string{"mcp"}, "env": map[string]string{ServerURLEnv: server()},
			}}}
			encoder := json.NewEncoder(streams.Out)
			encoder.SetEscapeHTML(false)
			encoder.SetIndent("", "  ")
			return encoder.Encode(document)
		case "kiro":
			fmt.Fprintf(streams.Out, "kiro-cli mcp add --name conn --scope workspace --command \"%s\" --env \"%s=%s\" --args mcp\n", executable, ServerURLEnv, server())
			return nil
		}
		return usageError{errors.New("--client must be generic or kiro")}
	}
	return cmd
}

func newInsightsCommand(deps Dependencies, streams Streams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "insights",
		Short: "Show Project-level numbers on warnings and evidence",
		Long: "Show Project-level numbers for a window of 1 to 365 days: collisions opened per rule,\n" +
			"decisions per kind and rule, the false-positive share per rule, the median time to a first\n" +
			"decision, decisions before either Intent was released, undeclared observed changes, acceptance\n" +
			"criteria by evidence provenance, and combination checks by outcome. Insights never include a\n" +
			"person, and ratios with fewer than 5 samples report insufficient data.",
		Args: cobra.NoArgs,
	}
	r := newRemote(cmd, deps, streams)
	var projectID uuid.UUID
	var days int
	cmd.Flags().Var(uuidValue{&projectID}, "project", "project ID (default: CONN_PROJECT or the matching Project)")
	cmd.Flags().IntVar(&days, "since-days", protocol.DefaultInsightDays, "window in days, from 1 to 365")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		if days < 1 || days > protocol.MaxInsightDays {
			return usageError{errors.New("--since-days must be from 1 to 365")}
		}
		client, err := r.client()
		if err != nil {
			return err
		}
		if projectID, err = resolveCLIProject(cmd.Context(), deps, client, projectID, "."); err != nil {
			return err
		}
		result, err := client.Insights(cmd.Context(), projectID, days)
		if err != nil {
			return err
		}
		return printResult(r, result, printInsights)
	}
	return cmd
}

func printInsights(out io.Writer, insights protocol.ProjectInsights) {
	ratio := func(value protocol.InsightRatio) string {
		if value.Status != protocol.InsightValue || value.Value == nil {
			return fmt.Sprintf("insufficient data (%d of %d)", value.Numerator, value.Denominator)
		}
		return fmt.Sprintf("%.0f%% (%d of %d)", *value.Value*100, value.Numerator, value.Denominator)
	}
	fmt.Fprintf(out, "Project insights for the last %d days (%s to %s). Ratios need at least %d samples.\n", insights.SinceDays, insights.WindowStart, insights.WindowEnd, insights.MinimumDenominator)
	fmt.Fprintln(out, "\nCollisions opened:")
	if len(insights.CollisionsOpened) == 0 {
		fmt.Fprintln(out, "  none")
	}
	for _, item := range insights.CollisionsOpened {
		fmt.Fprintf(out, "  %-28s %d\n", item.RuleID, item.Count)
	}
	fmt.Fprintln(out, "\nDecisions:")
	if len(insights.Decisions) == 0 {
		fmt.Fprintln(out, "  none")
	}
	for _, item := range insights.Decisions {
		fmt.Fprintf(out, "  %-28s %-20s %d\n", item.RuleID, item.Kind, item.Count)
	}
	for _, item := range insights.FalsePositiveShare {
		fmt.Fprintf(out, "  false-positive share, %s: %s\n", item.RuleID, ratio(item.Share))
	}
	median := insights.MedianHoursToFirstDecision
	if median.Status == protocol.InsightValue && median.Value != nil {
		fmt.Fprintf(out, "\nMedian time to first decision: %.1f hours (%d collisions)\n", *median.Value, median.SampleSize)
	} else {
		fmt.Fprintf(out, "\nMedian time to first decision: insufficient data (%d collisions)\n", median.SampleSize)
	}
	fmt.Fprintf(out, "Decided before either Intent was released: %s\n", ratio(insights.DecidedBeforeRelease))
	fmt.Fprintf(out, "Analyzed revisions with undeclared observed changes: %s\n", ratio(insights.UndeclaredObservedChanges))
	acceptance := insights.AcceptanceByProvenance
	fmt.Fprintf(out, "Acceptance criteria on current Objectives: %d observed run, %d reported, %d missing\n", acceptance.ObservedRun, acceptance.Reported, acceptance.Missing)
	outcomes := make([]string, 0, len(insights.CombinationChecks))
	for _, item := range insights.CombinationChecks {
		outcomes = append(outcomes, fmt.Sprintf("%s %d", strings.ReplaceAll(item.Outcome, "_", " "), item.Count))
	}
	if len(outcomes) == 0 {
		outcomes = []string{"none"}
	}
	fmt.Fprintf(out, "Combination checks: %s\n", strings.Join(outcomes, ", "))
}
