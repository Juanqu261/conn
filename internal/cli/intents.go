package cli

import (
	"fmt"
	"io"

	"uuid"

	"github.com/spf13/cobra"

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
	intent.AddCommand(publish, revise, release, list, read)
	return intent
}

func printIntent(out io.Writer, value protocol.IntentView) {
	r := value.IntentRevision
	fmt.Fprintf(out, "Intent %s revision %d is %s.\n%s\n", r.IntentID, r.Revision, value.Status, clean(r.SummaryMarkdown))
	for _, observation := range value.AdvisoryObservations {
		fmt.Fprintf(out, "Advisory: %s\n", clean(observation))
	}
}
