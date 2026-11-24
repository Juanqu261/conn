package cli

import (
	"fmt"
	"io"
	"path/filepath"
	"time"

	"uuid"

	"github.com/spf13/cobra"

	localanalyzer "github.com/Juanqu261/conn/internal/analyzer"
	"github.com/Juanqu261/conn/internal/protocol"
)

func newIntentCommand(deps Dependencies, streams Streams) *cobra.Command {
	intent := group("intent", "Publish and coordinate your own change intents")

	publish := &cobra.Command{Use: "publish", Short: "Publish your initial Intent from JSON", Args: cobra.NoArgs}
	rp := newRemote(publish, deps, streams)
	var publishProject uuid.UUID
	var publishFile string
	requiredUUID(publish, &publishProject, "project", "project ID")
	requiredString(publish, &publishFile, "file", "intent-input JSON document, or - for standard input")
	publish.RunE = func(cmd *cobra.Command, _ []string) error {
		input, err := readInput[protocol.IntentInput](streams.In, publishFile)
		if err != nil {
			return err
		}
		client, err := rp.client()
		if err != nil {
			return err
		}
		result, err := client.PublishIntent(cmd.Context(), publishProject, input)
		if err != nil {
			return err
		}
		return printResult(rp, result, printIntent)
	}

	revise := &cobra.Command{Use: "revise", Short: "Append a revision to your active Intent", Args: cobra.NoArgs}
	rr := newRemote(revise, deps, streams)
	var reviseProject, reviseID uuid.UUID
	var reviseExpected int
	var reviseFile string
	requiredUUID(revise, &reviseProject, "project", "project ID")
	requiredUUID(revise, &reviseID, "intent", "intent ID")
	requiredString(revise, &reviseFile, "file", "intent-input JSON document, or - for standard input")
	revise.Flags().IntVar(&reviseExpected, "expected-revision", 0, "current revision expected before appending")
	revise.RunE = func(cmd *cobra.Command, _ []string) error {
		if reviseExpected < 1 {
			return usageError{fmt.Errorf("--expected-revision must be positive")}
		}
		input, err := readInput[protocol.IntentInput](streams.In, reviseFile)
		if err != nil {
			return err
		}
		if input.IntentID != reviseID {
			return usageError{fmt.Errorf("--intent must match intent_id in the input document")}
		}
		client, err := rr.client()
		if err != nil {
			return err
		}
		result, err := client.ReviseIntent(cmd.Context(), reviseProject, reviseID, reviseExpected, input)
		if err != nil {
			return err
		}
		return printResult(rr, result, printIntent)
	}

	release := &cobra.Command{Use: "release", Short: "Release your active Intent", Args: cobra.NoArgs}
	rl := newRemote(release, deps, streams)
	var releaseProject, releaseID uuid.UUID
	requiredUUID(release, &releaseProject, "project", "project ID")
	requiredUUID(release, &releaseID, "intent", "intent ID")
	release.RunE = func(cmd *cobra.Command, _ []string) error {
		client, err := rl.client()
		if err != nil {
			return err
		}
		result, err := client.ReleaseIntent(cmd.Context(), releaseProject, releaseID)
		if err != nil {
			return err
		}
		return printResult(rl, result, printIntent)
	}

	analyze := &cobra.Command{Use: "analyze", Short: "Analyze a local TypeScript worktree and attach source-free claims", Args: cobra.NoArgs}
	ra := newRemote(analyze, deps, streams)
	var analyzeProject, analyzeIntent uuid.UUID
	var worktree, remote, nodePath, analyzerPath, tsconfigPath string
	var paths []string
	var timeout time.Duration
	requiredUUID(analyze, &analyzeProject, "project", "project ID")
	requiredUUID(analyze, &analyzeIntent, "intent", "intent ID")
	requiredString(analyze, &worktree, "worktree", "local repository worktree")
	requiredString(analyze, &remote, "remote", "credential-free canonical repository remote")
	analyze.Flags().StringVar(&nodePath, "node", "node", "Node.js executable")
	analyze.Flags().StringVar(&analyzerPath, "analyzer-script", "packages/typescript-analyzer/dist/cli.js", "built local TypeScript analyzer script")
	analyze.Flags().StringVar(&tsconfigPath, "tsconfig", "", "optional repository-relative tsconfig path")
	analyze.Flags().StringSliceVar(&paths, "path", nil, "repository-relative TypeScript path to analyze (repeatable)")
	analyze.Flags().DurationVar(&timeout, "timeout", localanalyzer.DefaultTimeout, "maximum local analysis duration")
	analyze.RunE = func(cmd *cobra.Command, _ []string) error {
		if timeout <= 0 {
			return usageError{fmt.Errorf("--timeout must be positive")}
		}
		absoluteScript, err := filepath.Abs(analyzerPath)
		if err != nil {
			return fmt.Errorf("resolve analyzer script: %w", err)
		}
		report, err := localanalyzer.Run(cmd.Context(), localanalyzer.Config{NodePath: nodePath, AnalyzerPath: absoluteScript, Timeout: timeout}, localanalyzer.Request{
			Worktree: worktree, CanonicalRemote: remote, TSConfigPath: tsconfigPath, Paths: paths,
		})
		if err != nil {
			return err
		}
		client, err := ra.client()
		if err != nil {
			return err
		}
		result, err := client.AttachStaticAnalysis(cmd.Context(), analyzeProject, analyzeIntent, report)
		if err != nil {
			return err
		}
		return printResult(ra, result, func(out io.Writer, value protocol.IntentView) {
			fmt.Fprintf(out, "Attached %d static-analysis claims to Intent %s revision %d.\n", len(report.Claims), value.IntentRevision.IntentID, value.IntentRevision.Revision)
			if len(report.Diagnostics) > 0 {
				fmt.Fprintf(out, "Analyzer reported %d bounded diagnostics; Conn retained them as incomplete-analysis context.\n", len(report.Diagnostics))
			}
		})
	}

	list := &cobra.Command{Use: "list", Short: "List active and released Intents", Args: cobra.NoArgs}
	rlist := newRemote(list, deps, streams)
	var listProject uuid.UUID
	requiredUUID(list, &listProject, "project", "project ID")
	list.RunE = func(cmd *cobra.Command, _ []string) error {
		client, err := rlist.client()
		if err != nil {
			return err
		}
		result, err := client.Intents(cmd.Context(), listProject)
		if err != nil {
			return err
		}
		return printResult(rlist, result, func(out io.Writer, value protocol.IntentList) {
			if len(value.Intents) == 0 {
				fmt.Fprintln(out, "No Intents are published.")
				return
			}
			for _, item := range value.Intents {
				fmt.Fprintf(out, "%s  revision %d  %s  %s\n", item.IntentRevision.IntentID, item.IntentRevision.Revision, item.Status, clean(item.IntentRevision.SummaryMarkdown))
			}
			fmt.Fprintf(out, "As of event %d.\n", result.AsOfEvent)
		})
	}

	read := &cobra.Command{Use: "read", Short: "Read an Intent's current revision", Args: cobra.NoArgs}
	rread := newRemote(read, deps, streams)
	var readProject, readID uuid.UUID
	requiredUUID(read, &readProject, "project", "project ID")
	requiredUUID(read, &readID, "intent", "intent ID")
	read.RunE = func(cmd *cobra.Command, _ []string) error {
		client, err := rread.client()
		if err != nil {
			return err
		}
		result, err := client.Intent(cmd.Context(), readProject, readID)
		if err != nil {
			return err
		}
		return printResult(rread, result, printIntent)
	}
	intent.AddCommand(publish, revise, release, analyze, list, read)
	return intent
}

func printIntent(out io.Writer, value protocol.IntentView) {
	r := value.IntentRevision
	fmt.Fprintf(out, "Intent %s revision %d is %s.\n%s\n", r.IntentID, r.Revision, value.Status, clean(r.SummaryMarkdown))
	for _, observation := range value.AdvisoryObservations {
		fmt.Fprintf(out, "Advisory: %s\n", clean(observation))
	}
}
