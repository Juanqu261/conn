package mcp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"uuid"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Juanqu261/conn/internal/httpclient"
	"github.com/Juanqu261/conn/internal/protocol"
	"github.com/Juanqu261/conn/internal/repositorygit"
)

const (
	maxProjectChoices   = 50
	maxRepositoryLabels = 20
)

type selectionSource string

const (
	selectionExplicit        selectionSource = "explicit"
	selectionEnv             selectionSource = "env"
	selectionSoleProject     selectionSource = "sole_project"
	selectionRepositoryMatch selectionSource = "repository_match"
)

type selectedProject struct {
	ProjectID       uuid.UUID
	Source          selectionSource
	RepositoryMatch string
	SelectedAt      time.Time
}

type projectSelection struct {
	mu       sync.RWMutex
	selected *selectedProject
	now      func() time.Time
}

func newProjectSelection() *projectSelection {
	return &projectSelection{now: time.Now}
}

func (s *projectSelection) set(projectID uuid.UUID, source selectionSource, repositoryMatch string) selectedProject {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.selected != nil && s.selected.ProjectID == projectID && s.selected.Source == source && s.selected.RepositoryMatch == repositoryMatch {
		return *s.selected
	}
	selection := selectedProject{ProjectID: projectID, Source: source, RepositoryMatch: repositoryMatch, SelectedAt: s.now()}
	s.selected = &selection
	return selection
}

func (s *projectSelection) get() (selectedProject, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.selected == nil {
		return selectedProject{}, false
	}
	return *s.selected, true
}

type projectChoice struct {
	ProjectID             string   `json:"project_id"`
	Name                  string   `json:"name"`
	Role                  string   `json:"role"`
	Repositories          []string `json:"repositories"`
	RepositoriesTruncated bool     `json:"repositories_truncated"`
}

type projectListOutput struct {
	Projects  []projectChoice `json:"projects"`
	Truncated bool            `json:"truncated"`
}

type projectSelectionOutput struct {
	Selected projectChoice `json:"selected"`
	Source   string        `json:"source"`
}

type selectProjectInput struct {
	ProjectID string `json:"project_id"`
}

func registerProjectTools(server *sdk.Server, t tools) {
	closed := false
	sdk.AddTool(server, &sdk.Tool{
		Name:        "list_projects",
		Description: "List Projects visible to the authenticated Human with bounded, credential-free repository labels.",
		InputSchema: objectSchema(nil, map[string]any{}),
		Annotations: &sdk.ToolAnnotations{Title: "List Conn Projects", ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: &closed},
	}, t.listProjects)
	sdk.AddTool(server, &sdk.Tool{
		Name:        "select_project",
		Description: "Validate membership and select one Project for this MCP process only. Selection grants no authority and is not persisted.",
		InputSchema: selectProjectToolSchema(),
		Annotations: &sdk.ToolAnnotations{Title: "Select Conn Project", ReadOnlyHint: false, DestructiveHint: &closed, IdempotentHint: true, OpenWorldHint: &closed},
	}, t.selectProject)
}

func (t tools) listProjects(ctx context.Context, _ *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, projectListOutput, error) {
	client, err := t.client()
	if err != nil {
		return nil, projectListOutput{}, t.toolError("list_projects", err)
	}
	projects, err := client.Projects(ctx)
	if err != nil {
		return nil, projectListOutput{}, t.toolError("list_projects", err)
	}
	choices, truncated, err := loadProjectChoices(ctx, client, projects.Projects)
	if err != nil {
		return nil, projectListOutput{}, t.toolError("list_projects", err)
	}
	output := projectListOutput{Projects: choices, Truncated: truncated}
	return statusResult(fmt.Sprintf("%d visible Conn Project(s).", len(choices))), output, nil
}

func (t tools) selectProject(ctx context.Context, _ *sdk.CallToolRequest, in selectProjectInput) (*sdk.CallToolResult, projectSelectionOutput, error) {
	client, err := t.client()
	if err != nil {
		return nil, projectSelectionOutput{}, t.toolError("select_project", err)
	}
	projectID, err := parseID("project_id", in.ProjectID)
	if err != nil {
		return nil, projectSelectionOutput{}, t.toolError("select_project", err)
	}
	projects, err := client.Projects(ctx)
	if err != nil {
		return nil, projectSelectionOutput{}, t.toolError("select_project", err)
	}
	entry, ok := visibleProject(projects.Projects, projectID)
	if !ok {
		return nil, projectSelectionOutput{}, t.toolError("select_project", errors.New("not found, or the signed-in Human is not a member of this Project"))
	}
	choices, _, err := loadProjectChoices(ctx, client, []protocol.ProjectListEntry{entry})
	if err != nil {
		return nil, projectSelectionOutput{}, t.toolError("select_project", err)
	}
	t.selection.set(projectID, selectionExplicit, "")
	output := projectSelectionOutput{Selected: choices[0], Source: string(selectionExplicit)}
	return statusResult(fmt.Sprintf("Selected Project %s (%s) for this MCP process.", output.Selected.Name, output.Selected.ProjectID)), output, nil
}

func (t tools) client() (*httpclient.Client, error) {
	if t.cfg.Client == nil {
		return nil, errors.New("Conn backend client is unavailable")
	}
	client, err := t.cfg.Client()
	if err != nil {
		return nil, err
	}
	return client.WithAdapter("kiro"), nil
}

func (t tools) resolveProject(ctx context.Context, client *httpclient.Client, explicit string) (selectedProject, error) {
	if explicit != "" {
		projectID, err := parseID("project_id", explicit)
		if err != nil {
			return selectedProject{}, err
		}
		return selectedProject{ProjectID: projectID, Source: selectionExplicit}, nil
	}
	if selected, ok := t.selection.get(); ok {
		return selected, nil
	}

	projects, err := client.Projects(ctx)
	if err != nil {
		return selectedProject{}, err
	}
	if t.cfg.DefaultProject != "" {
		projectID, err := parseID("CONN_PROJECT", t.cfg.DefaultProject)
		if err != nil {
			return selectedProject{}, err
		}
		if _, ok := visibleProject(projects.Projects, projectID); !ok {
			return selectedProject{}, errors.New("not found, or the signed-in Human is not a member of the configured Project")
		}
		return selectedProject{ProjectID: projectID, Source: selectionEnv}, nil
	}

	switch len(projects.Projects) {
	case 0:
		return selectedProject{}, errors.New("the signed-in Human has no Conn Projects")
	case 1:
		entry := projects.Projects[0]
		return t.selection.set(entry.Project.ProjectID, selectionSoleProject, ""), nil
	}

	metadata, discoverErr := t.discoverRepository(ctx)
	if errors.Is(discoverErr, context.Canceled) || errors.Is(discoverErr, context.DeadlineExceeded) {
		return selectedProject{}, discoverErr
	}
	if discoverErr == nil {
		matches, err := matchingProjects(ctx, client, projects.Projects, metadata.CanonicalRemote)
		if err != nil {
			return selectedProject{}, err
		}
		if len(matches) == 1 {
			return t.selection.set(matches[0].Project.ProjectID, selectionRepositoryMatch, metadata.CanonicalRemote), nil
		}
		if len(matches) > 1 {
			choices, truncated, err := loadProjectChoices(ctx, client, matches)
			if err != nil {
				return selectedProject{}, err
			}
			return selectedProject{}, ambiguousProjects(choices, truncated)
		}
	}

	choices, truncated, err := loadProjectChoices(ctx, client, projects.Projects)
	if err != nil {
		return selectedProject{}, err
	}
	return selectedProject{}, ambiguousProjects(choices, truncated)
}

func (t tools) discoverRepository(ctx context.Context) (repositorygit.Metadata, error) {
	if t.cfg.DiscoverRepository != nil {
		return t.cfg.DiscoverRepository(ctx)
	}
	return repositorygit.Discover(ctx, ".")
}

func matchingProjects(ctx context.Context, client *httpclient.Client, projects []protocol.ProjectListEntry, canonicalRemote string) ([]protocol.ProjectListEntry, error) {
	matches := make([]protocol.ProjectListEntry, 0, len(projects))
	for _, entry := range projects {
		repositories, err := client.RepositoryConnections(ctx, entry.Project.ProjectID)
		if err != nil {
			return nil, err
		}
		for _, repository := range repositories.Data.Repositories {
			if repository.CanonicalRemote == canonicalRemote {
				matches = append(matches, entry)
				break
			}
		}
	}
	return matches, nil
}

func loadProjectChoices(ctx context.Context, client *httpclient.Client, projects []protocol.ProjectListEntry) ([]projectChoice, bool, error) {
	limit := min(len(projects), maxProjectChoices)
	choices := make([]projectChoice, 0, limit)
	for _, entry := range projects[:limit] {
		repositories, err := client.RepositoryConnections(ctx, entry.Project.ProjectID)
		if err != nil {
			return nil, false, err
		}
		repositoryLimit := min(len(repositories.Data.Repositories), maxRepositoryLabels)
		labels := make([]string, 0, repositoryLimit)
		for _, repository := range repositories.Data.Repositories[:repositoryLimit] {
			labels = append(labels, repository.CanonicalRemote)
		}
		choices = append(choices, projectChoice{
			ProjectID:             entry.Project.ProjectID.String(),
			Name:                  entry.Project.Name,
			Role:                  string(entry.Role),
			Repositories:          labels,
			RepositoriesTruncated: len(repositories.Data.Repositories) > repositoryLimit,
		})
	}
	return choices, len(projects) > limit, nil
}

func visibleProject(projects []protocol.ProjectListEntry, projectID uuid.UUID) (protocol.ProjectListEntry, bool) {
	for _, entry := range projects {
		if entry.Project.ProjectID == projectID {
			return entry, true
		}
	}
	return protocol.ProjectListEntry{}, false
}

func ambiguousProjects(choices []projectChoice, truncated bool) error {
	labels := make([]string, 0, len(choices))
	for _, choice := range choices {
		label := fmt.Sprintf("%s (%s)", choice.Name, choice.ProjectID)
		if len(choice.Repositories) > 0 {
			label += " repositories: " + strings.Join(choice.Repositories, ", ")
		}
		if choice.RepositoriesTruncated {
			label += " (additional repositories omitted)"
		}
		labels = append(labels, label)
	}
	message := fmt.Sprintf("project selection is ambiguous; call list_projects, then select_project. Choices: %s", strings.Join(labels, "; "))
	if truncated {
		message += "; additional Projects were omitted from this bounded result"
	}
	return errors.New(message)
}
