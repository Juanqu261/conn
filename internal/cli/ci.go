package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"uuid"

	"github.com/spf13/cobra"

	"github.com/Juanqu261/conn/internal/ciaddon"
	"github.com/Juanqu261/conn/internal/cicheck"
	"github.com/Juanqu261/conn/internal/httpclient"
	"github.com/Juanqu261/conn/internal/protocol"
	"github.com/Juanqu261/conn/internal/repositorygit"
	"github.com/Juanqu261/conn/internal/secret"
)

var gitHubRepoPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,100}/[A-Za-z0-9._-]{1,100}$`)

// setGitHubSecret stores token as the CONN_CI_TOKEN secret through gh,
// writing it to gh's standard input so it never appears in arguments or
// output.
func setGitHubSecret(ctx context.Context, repo string, token secret.String) error {
	cmd := exec.CommandContext(ctx, "gh", "secret", "set", "CONN_CI_TOKEN", "--repo", repo)
	cmd.Stdin = strings.NewReader(token.Reveal())
	var stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = io.Discard, &stderr
	if err := cmd.Run(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return errors.New("the GitHub CLI (gh) is not installed or not on PATH")
		}
		message := strings.TrimSpace(stderr.String())
		if len(message) > 300 {
			message = message[:300]
		}
		return fmt.Errorf("gh secret set failed: %s", strings.ReplaceAll(message, token.Reveal(), "[redacted]"))
	}
	return nil
}

func newCICommand(deps Dependencies, streams Streams) *cobra.Command {
	ci := group("ci", "Manage the Conn pull-request check and its CI credential")
	ci.Long = "Manage the Conn pull-request check: a GitHub Actions workflow that reports Conn warnings,\n" +
		"unregistered work, and review packets on every pull request and never fails it for a finding.\n" +
		"CI writes nothing to Conn."
	ci.AddCommand(newCICredentialCommand(deps, streams), newCICheckCommand(deps, streams))
	ci.AddCommand(newCIAddonCommands(deps, streams)...)
	return ci
}

func newCICredentialCommand(deps Dependencies, streams Streams) *cobra.Command {
	credential := group("credential", "Create, list, and revoke CI credentials (Project owners only)")
	credential.Long = "CI credentials let the Conn pull-request check read one Project and preview collisions for\n" +
		"one repository. Only a Project owner creates them, from this terminal. Agents never create CI credentials."

	create := &cobra.Command{
		Use:   "create",
		Short: "Create a CI credential and deliver it to gh secret set or standard output",
		Long: "Create a CI credential bound to one RepositoryConnection. The token is delivered once:\n" +
			"--github-secret <owner/name> writes it to the standard input of `gh secret set CONN_CI_TOKEN`,\n" +
			"so it never appears on standard output; --print writes it to standard output for manual\n" +
			"handling. Without either option the command refuses. Conn stores only the token's hash.",
		Args: cobra.NoArgs,
	}
	rc := newRemote(create, deps, streams)
	var projectID, repositoryID uuid.UUID
	var label, githubSecret string
	var days int
	var print bool
	create.Flags().Var(uuidValue{&projectID}, "project", "project ID (default: CONN_PROJECT or the matching Project)")
	requiredUUID(create, &repositoryID, "repository", "RepositoryConnection ID the credential reads")
	create.Flags().StringVar(&label, "label", "", "optional label")
	create.Flags().IntVar(&days, "expires-in-days", protocol.DefaultCICredentialTTL, "expiry in days, from 1 to 365")
	create.Flags().StringVar(&githubSecret, "github-secret", "", "store the token as CONN_CI_TOKEN in this GitHub repository (owner/name) through gh")
	create.Flags().BoolVar(&print, "print", false, "print the token to standard output for manual handling")
	create.RunE = func(cmd *cobra.Command, _ []string) error {
		switch {
		case githubSecret == "" && !print:
			return usageError{errors.New("choose how to deliver the token: --github-secret <owner/name> or --print. Conn refuses to create a token nobody receives")}
		case githubSecret != "" && print:
			return usageError{errors.New("pass only one of --github-secret and --print")}
		case githubSecret != "" && !gitHubRepoPattern.MatchString(githubSecret):
			return usageError{errors.New("--github-secret must be owner/name")}
		case days < 1 || days > protocol.MaxCICredentialDays:
			return usageError{errors.New("--expires-in-days must be from 1 to 365")}
		}
		client, err := rc.client()
		if err != nil {
			return err
		}
		if projectID, err = resolveCLIProject(cmd.Context(), deps, client, projectID, ""); err != nil {
			return err
		}
		result, err := client.CreateCICredential(cmd.Context(), projectID, protocol.CICredentialCreate{RepositoryID: repositoryID, Label: label, ExpiresInDays: days})
		if err != nil {
			return err
		}
		grant := result.Data
		if githubSecret != "" {
			deliver := deps.SetGitHubSecret
			if deliver == nil {
				deliver = setGitHubSecret
			}
			if err := deliver(cmd.Context(), githubSecret, grant.Token); err != nil {
				// A token nobody received must not stay valid.
				if _, revokeErr := client.RevokeCICredential(context.WithoutCancel(cmd.Context()), projectID, grant.Credential.CredentialID); revokeErr != nil {
					return fmt.Errorf("%w; revoking credential %s also failed, so revoke it with conn ci credential revoke", err, grant.Credential.CredentialID)
				}
				return fmt.Errorf("%w; credential %s was revoked", err, grant.Credential.CredentialID)
			}
			fmt.Fprintf(streams.Out, "Created CI credential %s for repository %s, expiring %s, and stored it as the CONN_CI_TOKEN secret of %s.\n",
				grant.Credential.CredentialID, grant.Credential.RepositoryID, grant.Credential.ExpiresAt, githubSecret)
			return nil
		}
		fmt.Fprintf(streams.Err, "Created CI credential %s for repository %s, expiring %s. The token follows on standard output and is not shown again.\n",
			grant.Credential.CredentialID, grant.Credential.RepositoryID, grant.Credential.ExpiresAt)
		fmt.Fprintln(streams.Out, grant.Token.Reveal())
		return nil
	}

	list := &cobra.Command{Use: "list", Short: "List the Project's CI credentials without tokens", Args: cobra.NoArgs}
	rl := newRemote(list, deps, streams)
	var listProject uuid.UUID
	list.Flags().Var(uuidValue{&listProject}, "project", "project ID (default: CONN_PROJECT or the matching Project)")
	list.RunE = func(cmd *cobra.Command, _ []string) error {
		client, err := rl.client()
		if err != nil {
			return err
		}
		if listProject, err = resolveCLIProject(cmd.Context(), deps, client, listProject, ""); err != nil {
			return err
		}
		result, err := client.CICredentials(cmd.Context(), listProject)
		if err != nil {
			return err
		}
		return printResult(rl, result, func(out io.Writer, list protocol.CICredentialList) {
			if len(list.Credentials) == 0 {
				fmt.Fprintln(out, "No CI credentials exist for this Project.")
			}
			for _, item := range list.Credentials {
				state := "active"
				if item.RevokedAt != nil {
					state = "revoked " + item.RevokedAt.String()
				}
				lastUsed := "never used"
				if item.LastUsedAt != nil {
					lastUsed = "last used " + item.LastUsedAt.String()
				}
				label := item.Label
				if label == "" {
					label = "(no label)"
				}
				fmt.Fprintf(out, "%s  %s  repository %s  created by %s at %s  expires %s  %s  %s\n",
					item.CredentialID, clean(label), item.RepositoryID, item.CreatedByHumanID, item.CreatedAt, item.ExpiresAt, lastUsed, state)
			}
		})
	}

	revoke := &cobra.Command{Use: "revoke <credential-id>", Short: "Revoke a CI credential immediately", Args: cobra.ExactArgs(1)}
	rr := newRemote(revoke, deps, streams)
	var revokeProject uuid.UUID
	revoke.Flags().Var(uuidValue{&revokeProject}, "project", "project ID (default: CONN_PROJECT or the matching Project)")
	revoke.RunE = func(cmd *cobra.Command, args []string) error {
		credentialID, err := uuid.Parse(args[0])
		if err != nil {
			return usageError{errors.New("the credential ID is not a UUID")}
		}
		client, err := rr.client()
		if err != nil {
			return err
		}
		if revokeProject, err = resolveCLIProject(cmd.Context(), deps, client, revokeProject, ""); err != nil {
			return err
		}
		result, err := client.RevokeCICredential(cmd.Context(), revokeProject, credentialID)
		if err != nil {
			return err
		}
		return printResult(rr, result, func(out io.Writer, item protocol.CICredential) {
			fmt.Fprintf(out, "CI credential %s is revoked as of %s.\n", item.CredentialID, item.RevokedAt)
		})
	}
	credential.AddCommand(create, list, revoke)
	return credential
}

func newCICheckCommand(deps Dependencies, streams Streams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "check",
		Short: "Report Conn results for a pull request in CI",
		Long: "Report the Intents named by Conn-Intent trailers in the pull request, collisions previewed for\n" +
			"the changes observed between its merge base and head, and one review packet per Intent. The\n" +
			"report goes to the job summary and, with --comment, to one pull-request comment found by a\n" +
			"fixed marker.\n\n" +
			"It reads Conn with CONN_CI_TOKEN and writes nothing to Conn, runs no project code, and always\n" +
			"exits 0: findings and Conn, network, or configuration errors are described in the summary.\n" +
			"Without CONN_CI_TOKEN, as on fork pull requests, it reports that it was skipped.\n\n" +
			"Environment: CONN_SERVER_URL, CONN_CI_TOKEN, GITHUB_EVENT_PATH, GITHUB_STEP_SUMMARY, and\n" +
			"GITHUB_TOKEN with --comment.",
		Args: cobra.NoArgs,
	}
	var worktree, event, summary string
	var comment bool
	cmd.Flags().StringVar(&worktree, "worktree", ".", "checked-out pull request worktree")
	cmd.Flags().StringVar(&event, "event", "", "pull_request event file (default $GITHUB_EVENT_PATH)")
	cmd.Flags().StringVar(&summary, "summary", "", "job summary file (default $GITHUB_STEP_SUMMARY)")
	cmd.Flags().BoolVar(&comment, "comment", false, "create or update one pull-request comment")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		if event == "" {
			event, _ = deps.LookupEnv("GITHUB_EVENT_PATH")
		}
		if summary == "" {
			summary, _ = deps.LookupEnv("GITHUB_STEP_SUMMARY")
		}
		root := worktree
		if resolved, err := filepath.Abs(worktree); err == nil {
			root = resolved
		}
		location := locateAnalyzer(deps)
		cicheck.Run(cmd.Context(), cicheck.Config{
			Worktree: root, EventPath: event, SummaryPath: summary, Comment: comment, LookupEnv: deps.LookupEnv,
			GitHub: &cicheck.GitHubClient{BaseURL: deps.GitHubAPIURL}, Analyzer: location.Config, AnalyzerAvailable: location.Available(), Log: streams.Err,
		})
		return nil
	}
	return cmd
}

func newCIAddonCommands(deps Dependencies, streams Streams) []*cobra.Command {
	var root, connRef string
	var comment bool
	options := func(mode ciaddon.Mode) (ciaddon.Options, error) {
		if mode == ciaddon.ModeRemove {
			return ciaddon.Options{}, nil
		}
		ref := connRef
		if ref == "" {
			var err error
			if ref, err = ciaddon.DefaultRef(); err != nil {
				return ciaddon.Options{}, usageError{fmt.Errorf("%w (--conn-ref)", err)}
			}
		}
		return ciaddon.Options{ConnRef: ref, Comment: comment}, nil
	}
	plan := func(mode ciaddon.Mode) (ciaddon.Plan, error) {
		opts, err := options(mode)
		if err != nil {
			return ciaddon.Plan{}, err
		}
		return ciaddon.Preview(root, mode, opts)
	}
	addFlags := func(cmd *cobra.Command, install bool) {
		cmd.Flags().StringVar(&root, "root", "", "repository root (default: the current directory)")
		if install {
			cmd.Flags().StringVar(&connRef, "conn-ref", "", "Conn source revision the workflow builds (default: this build's commit)")
			cmd.Flags().BoolVar(&comment, "comment", false, "also create or update one pull-request comment (adds pull-requests: write)")
		}
	}
	preview := &cobra.Command{
		Use:   "preview",
		Short: "Show the Conn check workflow, its target, and its prerequisites without writing",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			proposed, err := plan(ciaddon.ModeInstall)
			if err != nil {
				return err
			}
			renderCIPlan(streams.Out, proposed)
			fmt.Fprintln(streams.Out, "\nPrerequisites:")
			for i, item := range ciaddon.Prerequisites(ciPrerequisiteInfo(cmd.Context(), deps, proposed.Root)) {
				fmt.Fprintf(streams.Out, "%d. %s\n", i+1, item.Text)
				if item.Command != "" {
					fmt.Fprintf(streams.Out, "   %s\n", item.Command)
				}
			}
			fmt.Fprintln(streams.Out, "No files changed.")
			return nil
		},
	}
	install := &cobra.Command{
		Use:   "install",
		Short: "Write .github/workflows/conn-check.yml after showing it",
		Long:  "Write the Conn check workflow. Installation refuses to overwrite different content. It never\ncreates credentials, sets secrets or variables, commits, pushes, or calls GitHub.",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			proposed, err := plan(ciaddon.ModeInstall)
			if err != nil {
				return err
			}
			renderCIPlan(streams.Out, proposed)
			if proposed.Action == ciaddon.ActionConflict {
				return errors.New("installation stopped: a different workflow exists at that path and was preserved")
			}
			if err := ciaddon.Apply(proposed); err != nil {
				return err
			}
			fmt.Fprintln(streams.Out, "Installed the Conn check workflow. Complete the prerequisites from `conn ci preview`, then commit and push it.")
			return nil
		},
	}
	remove := &cobra.Command{
		Use:   "remove",
		Short: "Remove the Conn check workflow only when it is unchanged Conn content",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			proposed, err := plan(ciaddon.ModeRemove)
			if err != nil {
				return err
			}
			renderCIPlan(streams.Out, proposed)
			if proposed.Action == ciaddon.ActionConflict {
				return errors.New("removal stopped: the workflow is not unchanged Conn content and was preserved for manual review")
			}
			if err := ciaddon.Apply(proposed); err != nil {
				return err
			}
			if proposed.Action == ciaddon.ActionRemove {
				fmt.Fprintln(streams.Out, "Removed the Conn check workflow.")
			}
			return nil
		},
	}
	addFlags(preview, true)
	addFlags(install, true)
	addFlags(remove, false)
	return []*cobra.Command{preview, install, remove}
}

func renderCIPlan(out io.Writer, plan ciaddon.Plan) {
	fmt.Fprintf(out, "Conn check %s plan for %s:\n  %-9s %s\n", plan.Mode, plan.Root, plan.Action, filepath.ToSlash(ciaddon.TargetPath))
	if plan.Mode == ciaddon.ModeInstall && (plan.Action == ciaddon.ActionCreate || plan.Action == ciaddon.ActionConflict) {
		fmt.Fprintf(out, "--- proposed %s ---\n%s--- end proposed ---\n", filepath.ToSlash(ciaddon.TargetPath), plan.Content)
	}
	if plan.Action == ciaddon.ActionConflict && plan.Existing != nil && plan.Content != nil {
		fmt.Fprintln(out, "The existing file differs from Conn's content:")
		for _, line := range ciaddon.Difference(plan.Existing, plan.Content, 40) {
			fmt.Fprintf(out, "  %s\n", clean(line))
		}
	}
}

// ciPrerequisiteInfo fills the prerequisite commands from local facts and,
// when the session can read it, the matching RepositoryConnection.
func ciPrerequisiteInfo(ctx context.Context, deps Dependencies, root string) ciaddon.PrerequisiteInfo {
	info := ciaddon.PrerequisiteInfo{}
	if value, ok := deps.LookupEnv(ServerURLEnv); ok {
		info.ServerURL = value
	}
	metadata, err := repositorygit.Discover(ctx, root)
	if err != nil {
		return info
	}
	if repo, found := strings.CutPrefix(metadata.CanonicalRemote, "github.com/"); found {
		info.GitHubRepo = repo
	}
	if deps.Account == nil {
		return info
	}
	server := info.ServerURL
	if server == "" {
		server = httpclient.DefaultServerURL
	}
	service, err := deps.Account(server)
	if err != nil {
		return info
	}
	client, err := service.Client()
	if err != nil {
		return info
	}
	projectID, err := resolveCLIProject(ctx, deps, client, uuid.UUID{}, root)
	if err != nil {
		return info
	}
	repositories, err := client.RepositoryConnections(ctx, projectID)
	if err != nil {
		return info
	}
	for _, repository := range repositories.Data.Repositories {
		if repository.CanonicalRemote == metadata.CanonicalRemote {
			info.RepositoryID = repository.RepositoryID.String()
		}
	}
	return info
}
