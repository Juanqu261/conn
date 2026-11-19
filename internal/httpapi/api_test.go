package httpapi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"uuid"

	"github.com/jackc/pgx/v5/stdlib"

	"github.com/Juanqu261/conn/internal/application"
	"github.com/Juanqu261/conn/internal/httpapi"
	"github.com/Juanqu261/conn/internal/migrations"
	"github.com/Juanqu261/conn/internal/postgres"
	"github.com/Juanqu261/conn/internal/postgres/postgrestest"
	"github.com/Juanqu261/conn/internal/protocol"
	"github.com/Juanqu261/conn/internal/secret"
)

// TestAPI protects the public /v1 contract (design: HTTP API): the
// {data, as_of_event} envelope, each stable error code and status, field
// pointers on invalid input, bounded JSON bodies, expected-revision
// conflicts on the revision routes, and event-cursor paging. Domain rules
// are covered by the persistence scenarios; each step here checks how one
// outcome reaches HTTP.
func TestAPI(t *testing.T) {
	ctx := context.Background()
	pool := postgrestest.NewDatabase(t)
	db := stdlib.OpenDBFromPool(pool)
	t.Cleanup(func() { _ = db.Close() })
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	if _, err := migrations.Up(ctx, db, log); err != nil {
		t.Fatal(err)
	}

	githubUsers := map[string]application.GitHubIdentity{
		"gh-alice": {UserID: 1, Login: "alice"},
		"gh-bob":   {UserID: 2, Login: "bob"},
		"gh-dave":  {UserID: 4, Login: "dave"},
	}
	verify := func(_ context.Context, token secret.String) (application.GitHubIdentity, error) {
		if identity, ok := githubUsers[token.Reveal()]; ok {
			return identity, nil
		}
		return application.GitHubIdentity{}, application.ErrGitHubTokenRejected
	}
	server := httptest.NewServer(httpapi.NewHandler(httpapi.Config{
		Ready:    func(context.Context) error { return nil },
		Auth:     &application.Auth{Store: postgres.AuthStore{Pool: pool}, VerifyToken: verify},
		Projects: &application.Projects{Store: postgres.ProjectStore{Pool: pool}},
		Log:      log,
	}))
	t.Cleanup(server.Close)

	call := func(method, path, token, contentType, body string) reply {
		t.Helper()
		req, err := http.NewRequest(method, server.URL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		if body != "" {
			req.Header.Set("Content-Type", contentType)
		}
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var r reply
		if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
			t.Fatalf("%s %s: decode response: %v", method, path, err)
		}
		r.Status = resp.StatusCode
		return r
	}
	login := func(githubToken string) (string, string) {
		r := call(http.MethodPost, "/v1/auth/github/exchange", githubToken, "", "")
		var grant struct {
			Token string         `json:"token"`
			Human protocol.Human `json:"human"`
		}
		if err := json.Unmarshal(r.Data, &grant); err != nil || r.Status != http.StatusCreated {
			t.Fatalf("login %s: %d %v", githubToken, r.Status, err)
		}
		return grant.Token, grant.Human.HumanID.String()
	}
	alice, aliceID := login("gh-alice")
	bob, bobID := login("gh-bob")
	dave, daveID := login("gh-dave")

	project, objective, blueprint := uuid.NewV7(), uuid.NewV7(), uuid.NewV7()
	cellA, cellB := uuid.NewV7(), uuid.NewV7()
	projectPath := "/v1/projects/" + project.String()
	objectiveBody := func(id uuid.UUID, title, keyPoints string) string {
		return fmt.Sprintf(`{"objective_id": %q, "title": %q, "overview_markdown": "Survive provider outages.",
			"accountable_human_id": %q%s}`, id, title, aliceID, keyPoints)
	}
	blueprintBody := func(bobOverview string) string {
		return fmt.Sprintf(`{"blueprint_id": %q, "objective_id": %q, "objective_revision": 1, "cell_charters": [
			{"cell_id": %q, "accountable_human_id": %q, "title": "Gateway", "overview_markdown": "Own failover."},
			{"cell_id": %q, "accountable_human_id": %q, "title": "Orders", "overview_markdown": %q}]}`,
			blueprint, objective, cellA, aliceID, cellB, bobID, bobOverview)
	}
	blueprintRevision := projectPath + "/blueprints/" + blueprint.String() + "/revisions/1"
	const jsonType = "application/json"
	duplicateKeys := `, "key_points": [
		{"key": "latency", "kind": "constraint", "statement": "p99 under 300 ms."},
		{"key": "latency", "kind": "outcome", "statement": "Fast checkout."}]`

	steps := []struct {
		name                  string
		method, path, token   string
		contentType, body     string
		wantStatus            int
		wantCode, wantPointer string
		wantEvent             int64 // 0 when the response carries no cursor
		check                 func(t *testing.T, data json.RawMessage)
	}{
		{name: "a missing session is unauthenticated", method: "GET", path: projectPath,
			wantStatus: 401, wantCode: "unauthenticated"},
		{name: "bodies must be JSON", method: "POST", path: "/v1/projects", token: alice,
			contentType: "text/plain", body: "{}", wantStatus: 415, wantCode: "unsupported_media_type"},
		{name: "malformed JSON is a bad request", method: "POST", path: "/v1/projects", token: alice,
			body: `{"project_id":`, wantStatus: 400, wantCode: "invalid_json"},
		{name: "schema failures name the field", method: "POST", path: "/v1/projects", token: alice,
			body: fmt.Sprintf(`{"project_id": %q, "name": " "}`, project), wantStatus: 422, wantCode: "invalid_input", wantPointer: "/name"},
		{name: "oversized bodies are refused", method: "POST", path: "/v1/projects", token: alice,
			body: fmt.Sprintf(`{"project_id": %q, "name": "%s"}`, project, strings.Repeat("x", 1<<20)), wantStatus: 413, wantCode: "request_too_large"},
		{name: "the owner creates the project", method: "POST", path: "/v1/projects", token: alice,
			body: fmt.Sprintf(`{"project_id": %q, "name": "Checkout reliability"}`, project), wantStatus: 201, wantEvent: 1,
			check: field("owner_human_id", aliceID)},
		{name: "the owner lists visible projects", method: "GET", path: "/v1/projects", token: alice,
			wantStatus: 200, check: func(t *testing.T, data json.RawMessage) {
				projects, err := protocol.Decode[protocol.ProjectList](data)
				if err != nil {
					t.Fatal(err)
				}
				if len(projects.Projects) != 1 || projects.Projects[0].Project.ProjectID != project || projects.Projects[0].Role != protocol.RoleOwner {
					t.Fatalf("projects = %#v, want owner project %s", projects, project)
				}
			}},
		{name: "a non-member gets not found", method: "GET", path: projectPath, token: bob,
			wantStatus: 404, wantCode: "not_found"},
		{name: "a non-member cannot list members", method: "GET", path: projectPath + "/members", token: dave,
			wantStatus: 404, wantCode: "not_found"},
		{name: "a malformed ID gets the same answer", method: "GET", path: "/v1/projects/not-a-uuid", token: bob,
			wantStatus: 404, wantCode: "not_found"},
		{name: "the owner adds a member", method: "POST", path: projectPath + "/members", token: alice,
			body: fmt.Sprintf(`{"human_id": %q}`, bobID), wantStatus: 201, wantEvent: 2, check: field("role", "member")},
		{name: "a project member lists named teammates", method: "GET", path: projectPath + "/members", token: bob,
			wantStatus: 200, wantEvent: 2, check: func(t *testing.T, data json.RawMessage) {
				members, err := protocol.Decode[protocol.ProjectMemberList](data)
				if err != nil {
					t.Fatal(err)
				}
				if members.ProjectID != project || len(members.Members) != 2 || members.Members[1].Human.HumanID.String() != bobID || members.Members[1].Membership.Role != protocol.RoleMember {
					t.Fatalf("members = %#v, want owner and member %s", members, bobID)
				}
			}},
		{name: "a member cannot add members", method: "POST", path: projectPath + "/members", token: bob,
			body: fmt.Sprintf(`{"human_id": %q}`, daveID), wantStatus: 403, wantCode: "forbidden"},
		{name: "application rules name the field", method: "POST", path: projectPath + "/objectives", token: alice,
			body: objectiveBody(objective, "Reliable checkout", duplicateKeys), wantStatus: 422, wantCode: "invalid_input", wantPointer: "/key_points/1/key"},
		{name: "the owner creates the objective", method: "POST", path: projectPath + "/objectives", token: alice,
			body: objectiveBody(objective, "Reliable checkout", ""), wantStatus: 201, wantEvent: 3, check: field("revision", 1)},
		{name: "reusing an ID for other content conflicts", method: "POST", path: projectPath + "/objectives", token: alice,
			body: objectiveBody(objective, "Something else", ""), wantStatus: 409, wantCode: "id_conflict"},
		{name: "a revision requires an expected revision", method: "POST", path: projectPath + "/objectives/" + objective.String() + "/revisions", token: alice,
			body: objectiveBody(objective, "Reliable checkout v2", ""), wantStatus: 422, wantCode: "invalid_input"},
		{name: "the body must name the path's objective", method: "POST", path: projectPath + "/objectives/" + objective.String() + "/revisions?expected_revision=1", token: alice,
			body: objectiveBody(uuid.NewV7(), "Reliable checkout v2", ""), wantStatus: 422, wantCode: "invalid_input", wantPointer: "/objective_id"},
		{name: "the owner appends revision 2", method: "POST", path: projectPath + "/objectives/" + objective.String() + "/revisions?expected_revision=1", token: alice,
			body: objectiveBody(objective, "Reliable checkout v2", ""), wantStatus: 201, wantEvent: 4, check: field("revision", 2)},
		{name: "a stale expected revision conflicts", method: "POST", path: projectPath + "/objectives/" + objective.String() + "/revisions?expected_revision=1", token: alice,
			body: objectiveBody(objective, "A concurrent edit", ""), wantStatus: 409, wantCode: "stale_revision"},
		{name: "a member reads a revision", method: "GET", path: projectPath + "/objectives/" + objective.String() + "/revisions/1", token: bob,
			wantStatus: 200, wantEvent: 4, check: field("title", "Reliable checkout")},
		{name: "the owner proposes a blueprint", method: "POST", path: projectPath + "/blueprints", token: alice,
			body: blueprintBody("Own order state."), wantStatus: 201, wantEvent: 5, check: field("status", "proposed")},
		{name: "no one decides another human's charter", method: "POST", path: blueprintRevision + "/cells/" + cellB.String() + "/accept", token: alice,
			wantStatus: 403, wantCode: "forbidden"},
		{name: "Alice accepts her charter", method: "POST", path: blueprintRevision + "/cells/" + cellA.String() + "/accept", token: alice,
			wantStatus: 201, wantEvent: 6, check: field("outcome", "accept")},
		{name: "the final acceptance activates the revision", method: "POST", path: blueprintRevision + "/cells/" + cellB.String() + "/accept", token: bob,
			wantStatus: 201, wantEvent: 8},
		{name: "a decided charter cannot change", method: "POST", path: blueprintRevision + "/cells/" + cellB.String() + "/reject", token: bob,
			body: `{"rationale": "Changed my mind."}`, wantStatus: 409, wantCode: "charter_decided"},
		{name: "the owner proposes blueprint revision 2", method: "POST", path: projectPath + "/blueprints/" + blueprint.String() + "/revisions?expected_revision=1", token: alice,
			body: blueprintBody("Own order state and refunds."), wantStatus: 201, wantEvent: 9},
		{name: "status shows the revisions in force", method: "GET", path: projectPath + "/status", token: bob,
			wantStatus: 200, wantEvent: 9, check: func(t *testing.T, data json.RawMessage) {
				status, err := protocol.Decode[protocol.ProjectStatus](data)
				if err != nil {
					t.Fatal(err)
				}
				var got []string
				for _, r := range status.Blueprints[0].Revisions {
					got = append(got, fmt.Sprintf("%d %s %s", r.Revision, r.Status, r.Cells[1].Decision))
				}
				if want := []string{"1 active accept", "2 proposed pending"}; status.Role != protocol.RoleMember || !slices.Equal(got, want) {
					t.Fatalf("status %s %v, want member %v", status.Role, got, want)
				}
			}},
		{name: "events page from a cursor", method: "GET", path: projectPath + "/events?after=6&limit=2", token: bob,
			wantStatus: 200, wantEvent: 9, check: func(t *testing.T, data json.RawMessage) {
				var events []json.RawMessage
				if err := json.Unmarshal(data, &events); err != nil {
					t.Fatal(err)
				}
				var got []string
				for _, raw := range events {
					event, err := protocol.Decode[protocol.EventEnvelope](raw)
					if err != nil {
						t.Fatal(err)
					}
					got = append(got, fmt.Sprintf("%d %s", event.EventSequence, event.EventType()))
				}
				if want := []string{"7 charter.accepted", "8 blueprint.revision_activated"}; !slices.Equal(got, want) {
					t.Fatalf("events %v, want %v", got, want)
				}
			}},
		{name: "event pages are bounded", method: "GET", path: projectPath + "/events?limit=501", token: bob,
			wantStatus: 422, wantCode: "invalid_input"},
		{name: "a non-member cannot page events", method: "GET", path: projectPath + "/events", token: dave,
			wantStatus: 404, wantCode: "not_found"},
	}
	for _, step := range steps {
		contentType := step.contentType
		if contentType == "" {
			contentType = jsonType
		}
		r := call(step.method, step.path, step.token, contentType, step.body)
		var code, pointer string
		if r.Error != nil {
			code, pointer = r.Error.Code, r.Error.Pointer
		}
		var asOf int64
		if r.AsOfEvent != nil {
			asOf = *r.AsOfEvent
		}
		if r.Status != step.wantStatus || code != step.wantCode || pointer != step.wantPointer || asOf != step.wantEvent {
			t.Fatalf("%s: got %d %q at %q as of %d, want %d %q at %q as of %d",
				step.name, r.Status, code, pointer, asOf, step.wantStatus, step.wantCode, step.wantPointer, step.wantEvent)
		}
		if step.check != nil {
			step.check(t, r.Data)
		}
	}
}

type reply struct {
	Status    int                   `json:"-"`
	Data      json.RawMessage       `json:"data"`
	AsOfEvent *int64                `json:"as_of_event"`
	Error     *protocol.ErrorDetail `json:"error"`
}

// field checks one top-level property of the response data.
func field(name string, want any) func(*testing.T, json.RawMessage) {
	return func(t *testing.T, data json.RawMessage) {
		t.Helper()
		var object map[string]any
		if err := json.Unmarshal(data, &object); err != nil {
			t.Fatal(err)
		}
		got, ok := object[name]
		if !ok {
			t.Fatalf("response data has no %q: %s", name, data)
		}
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("%s = %v, want %v", name, got, want)
		}
	}
}
