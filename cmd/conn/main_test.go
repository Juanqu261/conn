package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5/stdlib"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Juanqu261/conn/internal/application"
	"github.com/Juanqu261/conn/internal/httpapi"
	"github.com/Juanqu261/conn/internal/migrations"
	"github.com/Juanqu261/conn/internal/postgres"
	"github.com/Juanqu261/conn/internal/postgres/postgrestest"
	"github.com/Juanqu261/conn/internal/protocol"
	"github.com/Juanqu261/conn/internal/secret"
)

// The test binary re-executes itself as `conn` when this variable is set, so
// subprocess tests exercise the real entry point without a separate build.
const runMainEnv = "CONN_TEST_RUN_MAIN"

func TestMain(m *testing.M) {
	if os.Getenv(runMainEnv) == "1" {
		main()
		return
	}
	os.Exit(m.Run())
}

// TestMCPStdio protects the Kiro integration contract: Kiro starts `conn mcp`
// as a subprocess, all MCP traffic stays on standard output, existing reads
// remain available, mutations use the session-bound participant, and the
// process exits cleanly when Kiro disconnects.
func TestMCPStdio(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool := postgrestest.NewDatabase(t)
	db := stdlib.OpenDBFromPool(pool)
	t.Cleanup(func() { _ = db.Close() })
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	if _, err := migrations.Up(ctx, db, log); err != nil {
		t.Fatal(err)
	}
	auth := &application.Auth{
		Store: postgres.AuthStore{Pool: pool},
		VerifyToken: func(_ context.Context, token secret.String) (application.GitHubIdentity, error) {
			if token.Reveal() == "gh-alice" {
				return application.GitHubIdentity{UserID: 1, Login: "alice"}, nil
			}
			return application.GitHubIdentity{UserID: 2, Login: "bob"}, nil
		},
	}
	projects := &application.Projects{Store: postgres.ProjectStore{Pool: pool}}
	backend := httptest.NewServer(httpapi.NewHandler(httpapi.Config{
		Ready: func(context.Context) error { return nil }, Auth: auth, Projects: projects, Log: log,
	}))
	t.Cleanup(backend.Close)

	// Events 1 to 7: Alice creates the project, adds Bob, writes the
	// Objective, and proposes a two-cell blueprint that both accept.
	alice, bob := login(t, auth, "gh-alice"), login(t, auth, "gh-bob")
	projectID, objectiveID, blueprintID := uuid.NewV7(), uuid.NewV7(), uuid.NewV7()
	cellA, cellB := uuid.NewV7(), uuid.NewV7()
	proposal := func(bobOverview string) protocol.BlueprintProposal {
		return protocol.BlueprintProposal{
			BlueprintID: blueprintID, ObjectiveID: objectiveID, ObjectiveRevision: 1,
			CellCharters: []protocol.CellCharter{
				{CellID: cellA, AccountableHumanID: alice.HumanID, Title: "Gateway", OverviewMarkdown: "Own failover."},
				{CellID: cellB, AccountableHumanID: bob.HumanID, Title: "Orders", OverviewMarkdown: bobOverview},
			},
		}
	}
	seed := []func() error{
		func() error {
			_, err := projects.CreateProject(ctx, alice.HumanID, protocol.ProjectCreate{ProjectID: projectID, Name: "Checkout"})
			return err
		},
		func() error { _, err := projects.AddMember(ctx, alice.HumanID, projectID, bob.HumanID); return err },
		func() error {
			_, err := projects.CreateObjectiveRevision(ctx, alice.HumanID, projectID, protocol.ObjectiveRevisionInput{
				ObjectiveID: objectiveID, Title: "Reliable checkout", OverviewMarkdown: "Survive outages.", AccountableHumanID: alice.HumanID,
			}, 0)
			return err
		},
		func() error {
			_, err := projects.ProposeBlueprintRevision(ctx, alice.HumanID, projectID, proposal("Own order state."), 0)
			return err
		},
		func() error {
			_, err := projects.AcceptCharter(ctx, alice.HumanID, projectID, blueprintID, 1, cellA)
			return err
		},
		func() error {
			_, err := projects.AcceptCharter(ctx, bob.HumanID, projectID, blueprintID, 1, cellB)
			return err
		},
	}
	for _, step := range seed {
		if err := step(); err != nil {
			t.Fatal(err)
		}
	}

	// Bob's Kiro session. The session token arrives the way a headless
	// setup would provide it; the project comes from CONN_PROJECT.
	cmd := exec.Command(os.Args[0], "mcp")
	cmd.Env = append(os.Environ(), runMainEnv+"=1",
		"CONN_SERVER_URL="+backend.URL, "CONN_TOKEN="+bob.Token.Reveal(), "CONN_PROJECT="+projectID.String())
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	client := sdk.NewClient(&sdk.Implementation{Name: "conn-test", Version: "0"}, nil)
	session, err := client.Connect(ctx, &sdk.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatalf("initialize: %v\nstderr:\n%s", err, stderr.String())
	}
	if name := session.InitializeResult().ServerInfo.Name; name != "conn" {
		t.Fatalf("server name = %q, want conn", name)
	}

	// Existing read tools remain read-only. Mutating tools are explicitly
	// marked non-destructive and cannot make binding decisions.
	listed, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range listed.Tools {
		if tool.Annotations == nil {
			t.Fatalf("tool %s has no annotations", tool.Name)
		}
		if tool.Annotations.ReadOnlyHint && tool.Annotations.DestructiveHint != nil {
			t.Fatalf("read-only tool %s declares a destructive hint", tool.Name)
		}
		names = append(names, tool.Name)
	}
	slices.Sort(names)
	if want := []string{"attach_evidence", "check_collisions", "check_updates", "propose_resolution", "publish_intent", "read_blueprint", "read_conflict_context", "read_my_charter", "read_objective", "read_radar", "release_intent", "revise_intent"}; !slices.Equal(names, want) {
		t.Fatalf("tools %v, want %v", names, want)
	}

	call := func(name string, args map[string]any) (summary string, data json.RawMessage, asOf int64) {
		t.Helper()
		result, err := session.CallTool(ctx, &sdk.CallToolParams{Name: name, Arguments: args})
		if err != nil || result.IsError {
			t.Fatalf("%s(%v): %v %s\nstderr:\n%s", name, args, err, text(result), stderr.String())
		}
		raw, err := json.Marshal(result.StructuredContent)
		if err != nil {
			t.Fatal(err)
		}
		var envelope struct {
			Data      json.RawMessage `json:"data"`
			AsOfEvent int64           `json:"as_of_event"`
		}
		if err := json.Unmarshal(raw, &envelope); err != nil {
			t.Fatal(err)
		}
		return text(result), envelope.Data, envelope.AsOfEvent
	}

	// Bob reads his own charter in the active revision, with no input.
	_, data, asOf := call("read_my_charter", map[string]any{})
	view, err := protocol.Decode[protocol.CharterView](data)
	if err != nil {
		t.Fatal(err)
	}
	if view.HumanID != bob.HumanID || view.Status != protocol.BlueprintActive || view.Revision != 1 ||
		len(view.Charters) != 1 || view.Charters[0].Charter.CellID != cellB || view.Charters[0].Decision != protocol.CellAccepted || asOf != 7 {
		t.Fatalf("read_my_charter: %+v as of %d", view, asOf)
	}

	// No input can choose whose charter is read: the input schema rejects
	// unknown properties such as human_id.
	impersonation, err := session.CallTool(ctx, &sdk.CallToolParams{
		Name: "read_my_charter", Arguments: map[string]any{"human_id": alice.HumanID.String()},
	})
	if err == nil && (!impersonation.IsError || !strings.Contains(text(impersonation), "human_id")) {
		t.Fatalf("read_my_charter did not reject a human_id: %s", text(impersonation))
	}

	// Nothing changed after the cursor; then Alice proposes revision 2
	// elsewhere, and the cursor reports it.
	if _, data, asOf = call("check_updates", map[string]any{"after": 7}); string(data) != "[]" || asOf != 7 {
		t.Fatalf("check_updates before the change: %s as of %d", data, asOf)
	}
	if _, err := projects.ProposeBlueprintRevision(ctx, alice.HumanID, projectID, proposal("Own orders and refunds."), 1); err != nil {
		t.Fatal(err)
	}
	_, data, asOf = call("check_updates", map[string]any{"after": 7})
	var events []json.RawMessage
	if err := json.Unmarshal(data, &events); err != nil || len(events) != 1 || asOf != 8 {
		t.Fatalf("check_updates after the change: %s as of %d", data, asOf)
	}
	if event, err := protocol.Decode[protocol.EventEnvelope](events[0]); err != nil || event.EventType() != protocol.EventBlueprintRevisionProposed {
		t.Fatalf("event: %s, %v", events[0], err)
	}

	// The active revision stays in force, and the agent learns of the
	// unapproved proposal.
	summary, data, _ := call("read_my_charter", map[string]any{})
	if view, err = protocol.Decode[protocol.CharterView](data); err != nil || view.Revision != 1 || !strings.Contains(summary, "Revision 2 is proposed and unapproved") {
		t.Fatalf("read_my_charter after the proposal: revision %d, %q, %v", view.Revision, summary, err)
	}

	// Alice publishes an Intent outside the MCP subprocess. Bob then publishes
	// an overlapping declared invariant through MCP, which must be attributed to
	// Bob's session-bound participant and open an advisory collision.
	aliceParticipant, err := projects.ResolveParticipant(ctx, alice.HumanID, projectID, "cli", "")
	if err != nil {
		t.Fatal(err)
	}
	aliceIntentID, bobIntentID := uuid.NewV7(), uuid.NewV7()
	claim := func() []protocol.SemanticClaimCandidate {
		return []protocol.SemanticClaimCandidate{{
			SubjectKind: "invariant", NormalizedSubject: "invariant:checkout-idempotency", DisplayName: "Checkout idempotency",
			Relation: "changes", ProvenanceKind: "declared",
		}}
	}
	if _, err := projects.PublishIntent(ctx, alice.HumanID, projectID, aliceParticipant.Data.ParticipantID, protocol.IntentInput{
		IntentID: aliceIntentID, ObjectiveID: objectiveID, ObjectiveRevision: 1, SummaryMarkdown: "Update gateway retries.", DeclaredClaims: claim(),
	}); err != nil {
		t.Fatal(err)
	}
	intentArgs := func(intentID uuid.UUID, summary string) map[string]any {
		return map[string]any{
			"intent_id": intentID.String(), "objective_id": objectiveID.String(), "objective_revision": 1, "summary_markdown": summary,
			"declared_claims": []any{map[string]any{
				"subject_kind": "invariant", "normalized_subject": "invariant:checkout-idempotency", "display_name": "Checkout idempotency", "relation": "changes", "provenance_kind": "declared",
			}},
		}
	}
	_, data, _ = call("publish_intent", map[string]any{"intent": intentArgs(bobIntentID, "Update order retries.")})
	bobIntent, err := protocol.Decode[protocol.IntentView](data)
	if err != nil || bobIntent.IntentRevision.IntentID != bobIntentID || bobIntent.IntentRevision.Revision != 1 {
		t.Fatalf("publish_intent: %s, %v", data, err)
	}

	_, data, _ = call("check_collisions", map[string]any{"intent_id": bobIntentID.String()})
	var collisions []json.RawMessage
	if err := json.Unmarshal(data, &collisions); err != nil || len(collisions) != 1 {
		t.Fatalf("check_collisions: %s, %v", data, err)
	}
	collision, err := protocol.Decode[protocol.CollisionAssessment](collisions[0])
	if err != nil || collision.State != "open" {
		t.Fatalf("collision: %s, %v", collisions[0], err)
	}

	// Context retrieval is a read through the authenticated HTTP client, and a
	// proposal remains participant-authored and explicitly non-binding.
	_, data, _ = call("read_conflict_context", map[string]any{"collision_id": collision.CollisionID.String()})
	contextPack, err := protocol.Decode[protocol.ConflictContextPack](data)
	if err != nil || contextPack.Collision.CollisionID != collision.CollisionID || len(contextPack.Intents) != 2 {
		t.Fatalf("read_conflict_context: %s, %v", data, err)
	}
	_, data, _ = call("propose_resolution", map[string]any{
		"collision_id": collision.CollisionID.String(), "proposal": map[string]any{"kind": "sequencing", "rationale_markdown": "Complete gateway retries before order retries."},
	})
	proposalResult, err := protocol.Decode[protocol.ResolutionProposal](data)
	if err != nil || proposalResult.ProposedByParticipantID == nil || proposalResult.ProposedByHumanID != nil {
		t.Fatalf("propose_resolution: %s, %v", data, err)
	}

	// Bounded evidence is owned by Bob's active Intent, then revisions and a
	// release work through the same subprocess transport.
	_, data, _ = call("attach_evidence", map[string]any{"intent_id": bobIntentID.String(), "evidence": map[string]any{
		"intent_id": bobIntentID.String(), "objective_id": objectiveID.String(), "objective_revision": 1, "kind": "test_case", "state": "passing", "locator": "ci:order-retries",
	}})
	evidence, err := protocol.Decode[protocol.EvidenceReference](data)
	if err != nil || evidence.ProducerParticipantID == nil || evidence.IntentID == nil || *evidence.IntentID != bobIntentID {
		t.Fatalf("attach_evidence: %s, %v", data, err)
	}
	_, data, _ = call("revise_intent", map[string]any{
		"intent_id": bobIntentID.String(), "expected_revision": 1, "intent": intentArgs(bobIntentID, "Update order retries after gateway coordination."),
	})
	if revised, err := protocol.Decode[protocol.IntentView](data); err != nil || revised.IntentRevision.Revision != 2 {
		t.Fatalf("revise_intent: %s, %v", data, err)
	}
	_, data, _ = call("release_intent", map[string]any{"intent_id": bobIntentID.String()})
	if released, err := protocol.Decode[protocol.IntentView](data); err != nil || released.Status != "released" {
		t.Fatalf("release_intent: %s, %v", data, err)
	}
	_, data, _ = call("read_radar", map[string]any{})
	if radar, err := protocol.Decode[protocol.RadarView](data); err != nil || len(radar.ActiveIntents) != 1 {
		t.Fatalf("read_radar: %s, %v", data, err)
	}

	// Authority comes only from the authenticated session. An authority field
	// is not part of any MCP tool input and is rejected before an HTTP call.
	impersonation, err = session.CallTool(ctx, &sdk.CallToolParams{
		Name: "publish_intent", Arguments: map[string]any{"human_id": alice.HumanID.String(), "intent": intentArgs(uuid.NewV7(), "Impersonate Alice.")},
	})
	if err == nil && (!impersonation.IsError || !strings.Contains(text(impersonation), "human_id")) {
		t.Fatalf("publish_intent did not reject a human_id: %s", text(impersonation))
	}

	if err := session.Close(); err != nil {
		t.Fatalf("close: %v\nstderr:\n%s", err, stderr.String())
	}
	if cmd.ProcessState == nil || cmd.ProcessState.ExitCode() != 0 {
		t.Fatalf("conn mcp did not exit cleanly: %v\nstderr:\n%s", cmd.ProcessState, stderr.String())
	}
	for _, token := range []string{alice.Token.Reveal(), bob.Token.Reveal()} {
		if strings.Contains(stderr.String(), token) {
			t.Fatal("a session token appeared in conn mcp diagnostics")
		}
	}
}

func login(t *testing.T, auth *application.Auth, githubToken string) struct {
	HumanID uuid.UUID
	Token   secret.String
} {
	t.Helper()
	grant, err := auth.Exchange(context.Background(), secret.New(githubToken))
	if err != nil {
		t.Fatal(err)
	}
	return struct {
		HumanID uuid.UUID
		Token   secret.String
	}{grant.Human.HumanID, grant.Token}
}

// text joins a tool result's text content.
func text(result *sdk.CallToolResult) string {
	if result == nil {
		return ""
	}
	var parts []string
	for _, content := range result.Content {
		if t, ok := content.(*sdk.TextContent); ok {
			parts = append(parts, t.Text)
		}
	}
	return strings.Join(parts, "\n")
}
