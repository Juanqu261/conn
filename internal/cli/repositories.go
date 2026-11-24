package cli

import (
	"fmt"
	"io"

	"uuid"

	"github.com/spf13/cobra"

	"github.com/Juanqu261/conn/internal/protocol"
	"github.com/Juanqu261/conn/internal/repositorygit"
)

func newRepositoryCommand(deps Dependencies, streams Streams) *cobra.Command {
	repository := group("repository", "Connect and inspect project repository scopes")

	connect := &cobra.Command{
		Use:   "connect",
		Short: "Connect a repository to a project you own",
		Long: "Connect a credential-free repository identity from a JSON document, or derive one from\n" +
			"the local origin remote with --from-git. Conn never sends the raw origin URL.",
		Args: cobra.NoArgs,
	}
	rc := newRemote(connect, deps, streams)
	var projectID uuid.UUID
	var file, directory string
	var fromGit bool
	requiredUUID(connect, &projectID, "project", "project ID")
	connect.Flags().StringVar(&file, "file", "", "repository-connect-input JSON document")
	connect.Flags().BoolVar(&fromGit, "from-git", false, "derive a credential-free identity from origin")
	connect.Flags().StringVar(&directory, "directory", ".", "local worktree used with --from-git")
	connect.RunE = func(cmd *cobra.Command, _ []string) error {
		if (file == "") == !fromGit {
			return usageError{fmt.Errorf("provide exactly one of --file or --from-git")}
		}
		if !fromGit && directory != "." {
			return usageError{fmt.Errorf("--directory requires --from-git")}
		}

		var input protocol.RepositoryConnectInput
		if fromGit {
			metadata, err := repositorygit.Discover(cmd.Context(), directory)
			if err != nil {
				return fmt.Errorf("derive repository identity: %w", err)
			}
			input = protocol.RepositoryConnectInput{
				RepositoryID:    uuid.NewV7(),
				HostKind:        metadata.HostKind,
				CanonicalRemote: metadata.CanonicalRemote,
				AnalysisEnabled: true,
			}
			if _, err := protocol.Encode(input); err != nil {
				return err
			}
		} else {
			var err error
			input, err = readInput[protocol.RepositoryConnectInput](streams.In, file)
			if err != nil {
				return err
			}
		}
		client, err := rc.client()
		if err != nil {
			return err
		}
		result, err := client.ConnectRepository(cmd.Context(), projectID, input)
		if err != nil {
			return err
		}
		return printResult(rc, result, func(out io.Writer, value protocol.RepositoryConnection) {
			fmt.Fprintf(out, "Connected %s repository %s to project %s. Local analysis: %t.\n",
				value.HostKind, clean(value.CanonicalRemote), value.ProjectID, value.AnalysisEnabled)
		})
	}

	list := &cobra.Command{
		Use:   "list",
		Short: "List repository scopes visible in a project",
		Args:  cobra.NoArgs,
	}
	rl := newRemote(list, deps, streams)
	var listProjectID uuid.UUID
	requiredUUID(list, &listProjectID, "project", "project ID")
	list.RunE = func(cmd *cobra.Command, _ []string) error {
		client, err := rl.client()
		if err != nil {
			return err
		}
		result, err := client.RepositoryConnections(cmd.Context(), listProjectID)
		if err != nil {
			return err
		}
		return printResult(rl, result, func(out io.Writer, value protocol.RepositoryConnectionList) {
			if len(value.Repositories) == 0 {
				fmt.Fprintln(out, "No repositories are connected.")
				return
			}
			for _, item := range value.Repositories {
				fmt.Fprintf(out, "%s  %s  local analysis: %t\n", item.RepositoryID, clean(item.CanonicalRemote), item.AnalysisEnabled)
			}
			fmt.Fprintf(out, "As of event %d.\n", result.AsOfEvent)
		})
	}

	repository.AddCommand(connect, list)
	return repository
}
