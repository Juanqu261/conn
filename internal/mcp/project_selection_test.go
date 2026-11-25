package mcp

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"uuid"

	"github.com/Juanqu261/conn/internal/httpclient"
	"github.com/Juanqu261/conn/internal/protocol"
	"github.com/Juanqu261/conn/internal/repositorygit"
)

func TestProjectSelectionPrecedence(t *testing.T) {
	ctx := context.Background()
	projectA := selectionProject(t, "019a8f00-0101-7000-8000-000000000001", "Checkout")
	projectB := selectionProject(t, "019a8f00-0101-7000-8000-000000000002", "Payments")
	projectC := selectionProject(t, "019a8f00-0101-7000-8000-000000000003", "Settlement")
	entry := func(project protocol.Project) protocol.ProjectListEntry {
		return protocol.ProjectListEntry{Project: project, Role: protocol.RoleMember}
	}
	remoteA := "github.com/acme/checkout"
	remoteB := "github.com/acme/payments"

	tests := []struct {
		name             string
		projects         []protocol.ProjectListEntry
		repositories     map[uuid.UUID][]protocol.RepositoryConnection
		explicit         string
		sessionProject   *protocol.Project
		defaultProject   string
		discoveredRemote string
		wantProject      uuid.UUID
		wantSource       selectionSource
	}{
		{
			name:         "explicit request wins over session environment and repository",
			projects:     []protocol.ProjectListEntry{entry(projectA), entry(projectB), entry(projectC)},
			repositories: map[uuid.UUID][]protocol.RepositoryConnection{projectA.ProjectID: {selectionRepository(t, projectA.ProjectID, remoteA)}},
			explicit:     projectA.ProjectID.String(), sessionProject: &projectB, defaultProject: projectC.ProjectID.String(), discoveredRemote: remoteA,
			wantProject: projectA.ProjectID, wantSource: selectionExplicit,
		},
		{
			name:           "session selection overrides environment",
			projects:       []protocol.ProjectListEntry{entry(projectA), entry(projectB)},
			sessionProject: &projectB, defaultProject: projectA.ProjectID.String(), discoveredRemote: remoteA,
			wantProject: projectB.ProjectID, wantSource: selectionExplicit,
		},
		{
			name:           "environment precedes sole and repository discovery",
			projects:       []protocol.ProjectListEntry{entry(projectA), entry(projectB)},
			defaultProject: projectA.ProjectID.String(), discoveredRemote: remoteB,
			wantProject: projectA.ProjectID, wantSource: selectionEnv,
		},
		{
			name:             "sole visible Project becomes process selection",
			projects:         []protocol.ProjectListEntry{entry(projectC)},
			discoveredRemote: remoteA,
			wantProject:      projectC.ProjectID, wantSource: selectionSoleProject,
		},
		{
			name:     "exact normalized repository match becomes process selection",
			projects: []protocol.ProjectListEntry{entry(projectA), entry(projectB)},
			repositories: map[uuid.UUID][]protocol.RepositoryConnection{
				projectA.ProjectID: {selectionRepository(t, projectA.ProjectID, remoteA)},
				projectB.ProjectID: {selectionRepository(t, projectB.ProjectID, remoteB)},
			},
			discoveredRemote: remoteB,
			wantProject:      projectB.ProjectID, wantSource: selectionRepositoryMatch,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			backend := newSelectionBackend(t, tc.projects, tc.repositories)
			selection := newProjectSelection()
			if tc.sessionProject != nil {
				selection.set(tc.sessionProject.ProjectID, selectionExplicit, "")
			}
			adapter := tools{
				cfg: Config{
					Client:         func() (*httpclient.Client, error) { return backend.client, nil },
					DefaultProject: tc.defaultProject,
					DiscoverRepository: func(context.Context) (repositorygit.Metadata, error) {
						if tc.discoveredRemote == "" {
							return repositorygit.Metadata{}, repositorygit.ErrNoOrigin
						}
						return repositorygit.Metadata{HostKind: "github", CanonicalRemote: tc.discoveredRemote}, nil
					},
				},
				selection: selection,
			}
			_, projectID, err := adapter.session(ctx, tc.explicit)
			if err != nil {
				t.Fatal(err)
			}
			if projectID != tc.wantProject {
				t.Fatalf("project = %s, want %s", projectID, tc.wantProject)
			}
			resolved, err := adapter.resolveProject(ctx, backend.client, tc.explicit)
			if err != nil || resolved.Source != tc.wantSource {
				t.Fatalf("selection = %+v, %v; want source %s", resolved, err, tc.wantSource)
			}
			if tc.wantSource == selectionRepositoryMatch && resolved.RepositoryMatch != tc.discoveredRemote {
				t.Fatalf("repository match = %q, want %q", resolved.RepositoryMatch, tc.discoveredRemote)
			}
		})
	}

	t.Run("ambiguous repository match returns safe bounded choices", func(t *testing.T) {
		credentialRemote := "https://build-user:secret-value@github.com/acme/shared.git"
		metadata, err := repositorygit.NormalizeRemote(credentialRemote)
		if err != nil {
			t.Fatal(err)
		}
		projects := []protocol.ProjectListEntry{entry(projectA), entry(projectB)}
		repositories := map[uuid.UUID][]protocol.RepositoryConnection{
			projectA.ProjectID: {selectionRepository(t, projectA.ProjectID, metadata.CanonicalRemote)},
			projectB.ProjectID: {selectionRepository(t, projectB.ProjectID, metadata.CanonicalRemote)},
		}
		backend := newSelectionBackend(t, projects, repositories)
		adapter := tools{
			cfg: Config{
				Client: func() (*httpclient.Client, error) { return backend.client, nil },
				DiscoverRepository: func(context.Context) (repositorygit.Metadata, error) {
					return metadata, nil
				},
			},
			selection: newProjectSelection(),
		}
		_, err = adapter.resolveProject(ctx, backend.client, "")
		if err == nil || !strings.Contains(err.Error(), "project selection is ambiguous") || !strings.Contains(err.Error(), projectA.Name) ||
			!strings.Contains(err.Error(), metadata.CanonicalRemote) || strings.Contains(err.Error(), "build-user") || strings.Contains(err.Error(), "secret-value") {
			t.Fatalf("ambiguous error = %v", err)
		}
	})

	t.Run("unauthorized configured and explicit selection do not persist", func(t *testing.T) {
		backend := newSelectionBackend(t, []protocol.ProjectListEntry{entry(projectA)}, nil)
		configured := tools{
			cfg:       Config{Client: func() (*httpclient.Client, error) { return backend.client, nil }, DefaultProject: projectC.ProjectID.String()},
			selection: newProjectSelection(),
		}
		if _, err := configured.resolveProject(ctx, backend.client, ""); err == nil || !strings.Contains(err.Error(), "not found") {
			t.Fatalf("unauthorized CONN_PROJECT error = %v", err)
		}
		if _, ok := configured.selection.get(); ok {
			t.Fatal("unauthorized CONN_PROJECT changed process selection")
		}

		_, _, err := configured.selectProject(ctx, nil, selectProjectInput{ProjectID: projectC.ProjectID.String()})
		if err == nil || !strings.Contains(err.Error(), "not found") {
			t.Fatalf("unauthorized select_project error = %v", err)
		}
		if _, ok := configured.selection.get(); ok {
			t.Fatal("unauthorized select_project changed process selection")
		}
	})

	t.Run("list and explicit process selection expose only safe labels", func(t *testing.T) {
		backend := newSelectionBackend(t, []protocol.ProjectListEntry{entry(projectA), entry(projectB)}, map[uuid.UUID][]protocol.RepositoryConnection{
			projectA.ProjectID: {selectionRepository(t, projectA.ProjectID, remoteA)},
			projectB.ProjectID: {selectionRepository(t, projectB.ProjectID, remoteB)},
		})
		adapter := tools{cfg: Config{Client: func() (*httpclient.Client, error) { return backend.client, nil }}, selection: newProjectSelection()}
		_, listed, err := adapter.listProjects(ctx, nil, struct{}{})
		if err != nil || len(listed.Projects) != 2 || listed.Projects[0].Repositories[0] != remoteA {
			t.Fatalf("list_projects = %+v, %v", listed, err)
		}
		_, selected, err := adapter.selectProject(ctx, nil, selectProjectInput{ProjectID: projectB.ProjectID.String()})
		if err != nil || selected.Selected.ProjectID != projectB.ProjectID.String() || selected.Source != string(selectionExplicit) {
			t.Fatalf("select_project = %+v, %v", selected, err)
		}
		firstSelection, _ := adapter.selection.get()
		if _, _, err := adapter.selectProject(ctx, nil, selectProjectInput{ProjectID: projectB.ProjectID.String()}); err != nil {
			t.Fatal(err)
		}
		secondSelection, _ := adapter.selection.get()
		if !secondSelection.SelectedAt.Equal(firstSelection.SelectedAt) {
			t.Fatal("idempotent select_project changed selection timestamp")
		}
		resolved, err := adapter.resolveProject(ctx, backend.client, "")
		if err != nil || resolved.ProjectID != projectB.ProjectID || resolved.Source != selectionExplicit {
			t.Fatalf("selected resolution = %+v, %v", resolved, err)
		}
	})

	t.Run("process restart discards session selection", func(t *testing.T) {
		backend := newSelectionBackend(t, []protocol.ProjectListEntry{entry(projectA), entry(projectB)}, map[uuid.UUID][]protocol.RepositoryConnection{
			projectA.ProjectID: {}, projectB.ProjectID: {},
		})
		first := tools{cfg: Config{Client: func() (*httpclient.Client, error) { return backend.client, nil }}, selection: newProjectSelection()}
		first.selection.set(projectB.ProjectID, selectionExplicit, "")
		resolved, err := first.resolveProject(ctx, backend.client, "")
		if err != nil || resolved.ProjectID != projectB.ProjectID {
			t.Fatalf("first process selection = %+v, %v", resolved, err)
		}

		restarted := tools{
			cfg: Config{
				Client: func() (*httpclient.Client, error) { return backend.client, nil },
				DiscoverRepository: func(context.Context) (repositorygit.Metadata, error) {
					return repositorygit.Metadata{}, repositorygit.ErrNoOrigin
				},
			},
			selection: newProjectSelection(),
		}
		if _, err := restarted.resolveProject(ctx, backend.client, ""); err == nil || !strings.Contains(err.Error(), "ambiguous") {
			t.Fatalf("restarted process retained selection: %v", err)
		}
	})
}

type selectionBackend struct {
	server *httptest.Server
	client *httpclient.Client
}

func newSelectionBackend(t *testing.T, projects []protocol.ProjectListEntry, repositories map[uuid.UUID][]protocol.RepositoryConnection) *selectionBackend {
	t.Helper()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/v1/projects" {
			writeSelectionDocument(t, w, protocol.ProjectList{Projects: projects}, false)
			return
		}
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if r.Method == http.MethodGet && len(parts) == 4 && parts[0] == "v1" && parts[1] == "projects" && parts[3] == "repositories" {
			projectID, err := uuid.Parse(parts[2])
			if err != nil {
				http.NotFound(w, r)
				return
			}
			writeSelectionDocument(t, w, protocol.RepositoryConnectionList{ProjectID: projectID, Repositories: repositories[projectID]}, true)
			return
		}
		http.NotFound(w, r)
	})
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := httpclient.New(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	return &selectionBackend{server: server, client: client}
}

func writeSelectionDocument(t *testing.T, w http.ResponseWriter, document protocol.Document, cursor bool) {
	t.Helper()
	data, err := protocol.Encode(document)
	if err != nil {
		t.Fatal(err)
	}
	w.Header().Set("Content-Type", "application/json")
	if cursor {
		_, _ = fmt.Fprintf(w, `{"data":%s,"as_of_event":1}`, data)
		return
	}
	_, _ = fmt.Fprintf(w, `{"data":%s}`, data)
}

func selectionProject(t *testing.T, rawID, name string) protocol.Project {
	t.Helper()
	return protocol.Project{
		ProjectID:    selectionUUID(t, rawID),
		Name:         name,
		OwnerHumanID: selectionUUID(t, "019a8f00-0100-7000-8000-000000000001"),
		CreatedAt:    protocol.NewTimestamp(time.Date(2026, 11, 22, 9, 0, 0, 0, time.UTC)),
	}
}

func selectionRepository(t *testing.T, projectID uuid.UUID, remote string) protocol.RepositoryConnection {
	t.Helper()
	repositoryID := projectID
	repositoryID[15] += 20
	return protocol.RepositoryConnection{
		RepositoryID:     repositoryID,
		ProjectID:        projectID,
		HostKind:         "github",
		CanonicalRemote:  remote,
		AnalysisEnabled:  true,
		CreatedByHumanID: selectionUUID(t, "019a8f00-0100-7000-8000-000000000001"),
		CreatedAt:        protocol.NewTimestamp(time.Date(2026, 11, 22, 9, 0, 0, 0, time.UTC)),
	}
}

func selectionUUID(t *testing.T, raw string) uuid.UUID {
	t.Helper()
	id, err := uuid.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
