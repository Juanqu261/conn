package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"uuid"

	"github.com/charmbracelet/x/term"

	"github.com/Juanqu261/conn/internal/hookstate"
	"github.com/Juanqu261/conn/internal/httpclient"
	"github.com/Juanqu261/conn/internal/protocol"
	"github.com/Juanqu261/conn/internal/repositorygit"
)

// interactive reports whether standard input is a terminal a human can
// answer from.
func interactive(in io.Reader) bool {
	file, ok := in.(*os.File)
	return ok && term.IsTerminal(file.Fd())
}

// prompter reads one answer per question from a shared reader.
type prompter struct {
	reader *bufio.Reader
	out    io.Writer
}

func newPrompter(streams Streams) *prompter {
	return &prompter{reader: bufio.NewReader(streams.In), out: streams.Err}
}

func (p *prompter) ask(question string) (string, error) {
	fmt.Fprint(p.out, question)
	line, err := p.reader.ReadString('\n')
	if err != nil && !(errors.Is(err, io.EOF) && line != "") {
		return "", fmt.Errorf("no answer was read: %w", err)
	}
	return strings.TrimSpace(line), nil
}

func (p *prompter) confirm(question string) (bool, error) {
	answer, err := p.ask(question + " [y/N] ")
	if err != nil {
		return false, err
	}
	answer = strings.ToLower(answer)
	return answer == "y" || answer == "yes", nil
}

// resolveCLIProject applies the CLI Project precedence: --project, the
// worktree's focus record, CONN_PROJECT, the sole visible Project, then an
// exact match of the worktree's origin with one Project's repositories.
// Membership is always checked by the backend.
func resolveCLIProject(ctx context.Context, deps Dependencies, client *httpclient.Client, explicit uuid.UUID, worktree string) (uuid.UUID, error) {
	if explicit != (uuid.UUID{}) {
		return explicit, nil
	}
	if worktree != "" {
		if store, err := hookstate.Open(ctx, worktree); err == nil {
			if focus := store.Focus(); focus.ProjectID != nil {
				return *focus.ProjectID, nil
			}
		}
	}
	if value, ok := deps.LookupEnv(ProjectEnv); ok && strings.TrimSpace(value) != "" {
		id, err := uuid.Parse(strings.TrimSpace(value))
		if err != nil {
			return uuid.UUID{}, usageError{fmt.Errorf("%s is not a UUID", ProjectEnv)}
		}
		return id, nil
	}
	projects, err := client.Projects(ctx)
	if err != nil {
		return uuid.UUID{}, err
	}
	switch len(projects.Projects) {
	case 0:
		return uuid.UUID{}, usageError{errors.New("you belong to no Conn Project; create one or ask an owner to add you")}
	case 1:
		return projects.Projects[0].Project.ProjectID, nil
	}
	if worktree != "" {
		if metadata, err := repositorygit.Discover(ctx, worktree); err == nil {
			var matches []protocol.ProjectListEntry
			for _, entry := range projects.Projects {
				repositories, err := client.RepositoryConnections(ctx, entry.Project.ProjectID)
				if err != nil {
					return uuid.UUID{}, err
				}
				for _, repository := range repositories.Data.Repositories {
					if repository.CanonicalRemote == metadata.CanonicalRemote {
						matches = append(matches, entry)
						break
					}
				}
			}
			if len(matches) == 1 {
				return matches[0].Project.ProjectID, nil
			}
		}
	}
	names := make([]string, 0, len(projects.Projects))
	for _, entry := range projects.Projects {
		names = append(names, fmt.Sprintf("%s (%s)", clean(entry.Project.Name), entry.Project.ProjectID))
	}
	return uuid.UUID{}, usageError{fmt.Errorf("the Project is ambiguous; pass --project or set %s. Projects: %s", ProjectEnv, strings.Join(names, "; "))}
}
