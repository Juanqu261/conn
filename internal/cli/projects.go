package cli

import (
	"fmt"
	"io"
	"uuid"

	"github.com/spf13/cobra"

	"github.com/Juanqu261/conn/internal/protocol"
)

func newProjectCommand(deps Dependencies, streams Streams) *cobra.Command {
	project := group("project", "Create projects and manage their members")

	create := &cobra.Command{
		Use:   "create",
		Short: "Create a project that you own",
		Long: "Create a project from a project-create JSON document:\n\n" +
			`  {"project_id": "<new UUIDv7>", "name": "Checkout reliability"}` + "\n\n" +
			"Repeating the same file returns the existing project.",
		Args: cobra.NoArgs,
	}
	r := newRemote(create, deps, streams)
	var file string
	requiredString(create, &file, "file", `project-create JSON document, or "-" for standard input`)
	create.RunE = func(cmd *cobra.Command, _ []string) error {
		input, err := readInput[protocol.ProjectCreate](streams.In, file)
		if err != nil {
			return err
		}
		client, err := r.client()
		if err != nil {
			return err
		}
		result, err := client.CreateProject(cmd.Context(), input)
		if err != nil {
			return err
		}
		return printResult(r, result, func(out io.Writer, p protocol.Project) {
			fmt.Fprintf(out, "Project %q (%s) is ready. You are its owner.\n", clean(p.Name), p.ProjectID)
		})
	}

	member := group("member", "Manage project members")
	add := &cobra.Command{
		Use:   "add",
		Short: "Add a signed-in human to a project you own",
		Long:  "Add a human by the Conn human ID that `conn whoami` shows them. They must have run `conn login` once.",
		Args:  cobra.NoArgs,
	}
	ra := newRemote(add, deps, streams)
	var projectID, humanID uuid.UUID
	requiredUUID(add, &projectID, "project", "project ID")
	requiredUUID(add, &humanID, "human", "Conn human ID of the new member")
	add.RunE = func(cmd *cobra.Command, _ []string) error {
		client, err := ra.client()
		if err != nil {
			return err
		}
		result, err := client.AddMember(cmd.Context(), projectID, humanID)
		if err != nil {
			return err
		}
		return printResult(ra, result, func(out io.Writer, m protocol.ProjectMembership) {
			fmt.Fprintf(out, "%s is a %s of project %s.\n", m.HumanID, m.Role, m.ProjectID)
		})
	}
	member.AddCommand(add)
	project.AddCommand(create, member)
	return project
}

func newStatusCommand(deps Dependencies, streams Streams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show a project's objectives, blueprints, and charter decisions",
		Long: "Show a project's objectives and, for each blueprint, the active revision and any newer\n" +
			"revision with each cell's decision. Only an active revision is approved.",
		Args: cobra.NoArgs,
	}
	r := newRemote(cmd, deps, streams)
	var projectID uuid.UUID
	requiredUUID(cmd, &projectID, "project", "project ID")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		client, err := r.client()
		if err != nil {
			return err
		}
		result, err := client.Status(cmd.Context(), projectID)
		if err != nil {
			return err
		}
		return printResult(r, result, func(out io.Writer, s protocol.ProjectStatus) {
			fmt.Fprintf(out, "Project %q (%s). Your role: %s.\n", clean(s.Project.Name), s.Project.ProjectID, s.Role)
			fmt.Fprintln(out, "\nObjectives:")
			if len(s.Objectives) == 0 {
				fmt.Fprintln(out, "  none")
			}
			for _, o := range s.Objectives {
				fmt.Fprintf(out, "  %s  revision %d\n", o.ObjectiveID, o.CurrentRevision)
			}
			fmt.Fprintln(out, "\nBlueprints:")
			if len(s.Blueprints) == 0 {
				fmt.Fprintln(out, "  none")
			}
			for _, b := range s.Blueprints {
				for _, rev := range b.Revisions {
					fmt.Fprintf(out, "  %s  revision %d: %s, for objective revision %d\n",
						b.BlueprintID, rev.Revision, approval(rev.Status), rev.ObjectiveRevision)
					for _, cell := range rev.Cells {
						fmt.Fprintf(out, "    [%-7s] %s  %s  (accountable: %s)\n",
							cell.Decision, cell.CellID, clean(cell.Title), cell.AccountableHumanID)
					}
				}
			}
			fmt.Fprintf(out, "\nAs of event %d.\n", result.AsOfEvent)
		})
	}
	return cmd
}

// approval names a status and states plainly whether work may rely on it.
func approval(status protocol.BlueprintStatus) string {
	if status == protocol.BlueprintActive {
		return "active (approved)"
	}
	return string(status) + " (unapproved)"
}

func newSyncCommand(deps Dependencies, streams Streams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "List project events after a cursor",
		Long: "List committed project events after --after, oldest first. Pass the last event\n" +
			"number you have seen; repeating a cursor returns the same events.",
		Args: cobra.NoArgs,
	}
	r := newRemote(cmd, deps, streams)
	var projectID uuid.UUID
	var after int64
	var limit int
	requiredUUID(cmd, &projectID, "project", "project ID")
	cmd.Flags().Int64Var(&after, "after", 0, "last event number already seen")
	cmd.Flags().IntVar(&limit, "limit", 100, "maximum events to list, from 1 to 500")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		if after < 0 {
			return usageError{fmt.Errorf("--after cannot be negative")}
		}
		if limit < 1 || limit > 500 {
			return usageError{fmt.Errorf("--limit must be from 1 to 500")}
		}
		client, err := r.client()
		if err != nil {
			return err
		}
		result, err := client.Events(cmd.Context(), projectID, after, limit)
		if err != nil {
			return err
		}
		if r.json {
			data := []byte{'['}
			for i, event := range result.Data {
				encoded, err := protocol.Encode(event)
				if err != nil {
					return err
				}
				if i > 0 {
					data = append(data, ',')
				}
				data = append(data, encoded...)
			}
			writeEnvelope(streams.Out, append(data, ']'), result.AsOfEvent)
			return nil
		}
		last := after
		for _, e := range result.Data {
			fmt.Fprintf(streams.Out, "%6d  %s  %-28s %s %s  by %s\n",
				e.EventSequence, e.OccurredAt, e.EventType(), e.EntityType(), e.EntityID, e.ActorHumanID)
			last = e.EventSequence
		}
		if last < result.AsOfEvent {
			fmt.Fprintf(streams.Out, "More events remain. Run conn sync --project %s --after %d.\n", projectID, last)
		} else {
			fmt.Fprintf(streams.Out, "Up to date as of event %d.\n", result.AsOfEvent)
		}
		return nil
	}
	return cmd
}
