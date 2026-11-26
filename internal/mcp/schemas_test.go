package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Juanqu261/conn/internal/httpclient"
)

func TestCanonicalToolInputSchemas(t *testing.T) {
	ctx := context.Background()
	var backendCalls atomic.Int64
	marker := errors.New("validated input reached the tool handler")
	server := sdk.NewServer(&sdk.Implementation{Name: "conn-schema-test", Version: "0"}, nil)
	registerTools(server, Config{
		DefaultProject: "019a8f00-0001-7000-8000-000000000001",
		Client: func() (*httpclient.Client, error) {
			backendCalls.Add(1)
			return nil, marker
		},
	})
	serverTransport, clientTransport := sdk.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()
	client := sdk.NewClient(&sdk.Implementation{Name: "conn-schema-client", Version: "0"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer clientSession.Close()

	listed, err := clientSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	tools := make(map[string]*sdk.Tool, len(listed.Tools))
	for _, tool := range listed.Tools {
		tools[tool.Name] = tool
		if tool.Description == "" {
			t.Errorf("tool %s has no description", tool.Name)
		}
		schema := toolSchemaObject(t, tool)
		if schema["type"] != "object" || schema["additionalProperties"] != false {
			t.Errorf("tool %s root schema is not a closed object: %#v", tool.Name, schema)
		}
		encoded, err := json.Marshal(schema)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(encoded), `"$ref"`) {
			t.Errorf("tool %s exposes an unresolved schema reference", tool.Name)
		}
	}

	wantAnnotations := map[string]struct {
		readOnly    bool
		idempotent  bool
		destructive bool
		openWorld   bool
	}{
		"read_objective":        {true, true, false, false},
		"read_blueprint":        {true, true, false, false},
		"read_my_charter":       {true, true, false, false},
		"check_updates":         {true, true, false, false},
		"publish_intent":        {false, false, false, false},
		"revise_intent":         {false, true, false, false},
		"release_intent":        {false, true, false, false},
		"analyze_intent":        {false, true, false, false},
		"check_collisions":      {true, true, false, false},
		"read_radar":            {true, true, false, false},
		"read_work_context":     {true, true, false, false},
		"read_conflict_context": {true, true, false, false},
		"propose_resolution":    {false, false, false, false},
		"attach_evidence":       {false, false, false, false},
		"session_status":        {true, true, false, false},
		"list_projects":         {true, true, false, false},
		"select_project":        {false, true, false, false},
		"login":                 {false, false, false, true},
		"logout":                {false, true, true, false},
	}
	for name, want := range wantAnnotations {
		tool := tools[name]
		if tool == nil || tool.Annotations == nil {
			t.Fatalf("tool %s has no annotations", name)
		}
		annotations := tool.Annotations
		if annotations.ReadOnlyHint != want.readOnly || annotations.IdempotentHint != want.idempotent {
			t.Errorf("tool %s annotations = readOnly %t, idempotent %t", name, annotations.ReadOnlyHint, annotations.IdempotentHint)
		}
		if annotations.OpenWorldHint == nil || *annotations.OpenWorldHint != want.openWorld {
			t.Errorf("tool %s openWorldHint = %v, want %t", name, annotations.OpenWorldHint, want.openWorld)
		}
		if want.readOnly {
			if annotations.DestructiveHint != nil {
				t.Errorf("read-only tool %s declares destructiveHint", name)
			}
		} else if annotations.DestructiveHint == nil || *annotations.DestructiveHint != want.destructive {
			t.Errorf("tool %s destructiveHint = %v, want %t", name, annotations.DestructiveHint, want.destructive)
		}
	}

	publishSchema := propertySchema(t, toolSchemaObject(t, tools["publish_intent"]), "intent")
	assertRequired(t, publishSchema, false, "intent_id")
	assertRequired(t, publishSchema, true, "objective_id", "objective_revision", "summary_markdown")
	for _, forbidden := range []string{"human_id", "participant_id", "token", "binding"} {
		if _, ok := schemaProperties(t, publishSchema)[forbidden]; ok {
			t.Errorf("publish_intent advertises forbidden intent.%s", forbidden)
		}
	}
	claims := propertySchema(t, publishSchema, "declared_claims")
	if claims["maxItems"] != float64(100) {
		t.Errorf("declared_claims maxItems = %#v, want 100", claims["maxItems"])
	}
	evidenceSchema := propertySchema(t, toolSchemaObject(t, tools["attach_evidence"]), "evidence")
	assertRequired(t, evidenceSchema, false, "intent_id")
	assertRequired(t, evidenceSchema, true, "objective_id", "objective_revision", "kind", "state", "locator")
	proposalSchema := propertySchema(t, toolSchemaObject(t, tools["propose_resolution"]), "proposal")
	assertRequired(t, proposalSchema, true, "kind", "rationale_markdown")

	objectiveID := "019a8f00-0002-7000-8000-000000000001"
	intentID := "019a8f00-0012-7000-8000-000000000012"
	collisionID := "019a8f00-0016-7000-8000-000000000016"
	validIntent := map[string]any{
		"objective_id": objectiveID, "objective_revision": 1, "summary_markdown": "Coordinate retry behavior.",
		"declared_claims": []any{map[string]any{
			"subject_kind": "invariant", "normalized_subject": "invariant:checkout:one-charge", "display_name": "Checkout idempotency",
			"relation": "changes", "provenance_kind": "declared",
		}},
	}
	validEvidence := map[string]any{
		"objective_id": objectiveID, "objective_revision": 1, "kind": "test_case", "state": "passing", "locator": "ci:retry-test",
	}

	accepted := []struct {
		name string
		tool string
		args map[string]any
	}{
		{"publish Intent without generated ID", "publish_intent", map[string]any{"intent": validIntent}},
		{"revise Intent with defaulted nested ID", "revise_intent", map[string]any{"intent_id": intentID, "expected_revision": 1, "intent": validIntent}},
		{"attach evidence with defaulted nested ID", "attach_evidence", map[string]any{"intent_id": intentID, "evidence": validEvidence}},
		{"analyze exact Intent revision", "analyze_intent", map[string]any{"intent_id": intentID, "expected_intent_revision": 1, "paths": []any{"src/payment.ts"}}},
		{"submit non-binding proposal", "propose_resolution", map[string]any{"collision_id": collisionID, "proposal": map[string]any{"kind": "sequencing", "rationale_markdown": "Land the producer first."}}},
		{"read focused work context", "read_work_context", map[string]any{"project_id": "019a8f00-0001-7000-8000-000000000001", "objective_id": objectiveID, "after": 0, "limit": 100}},
	}
	for _, tc := range accepted {
		t.Run("accepts "+tc.name, func(t *testing.T) {
			before := backendCalls.Load()
			result, err := clientSession.CallTool(ctx, &sdk.CallToolParams{Name: tc.tool, Arguments: tc.args})
			if err != nil {
				t.Fatal(err)
			}
			if result == nil || !result.IsError || !strings.Contains(toolResultText(result), marker.Error()) {
				t.Fatalf("schema rejected representative canonical input: %s", toolResultText(result))
			}
			if got := backendCalls.Load(); got != before+1 {
				t.Fatalf("backend calls = %d, want %d", got, before+1)
			}
		})
	}

	rejected := []struct {
		name string
		tool string
		args map[string]any
	}{
		{"participant authority", "publish_intent", map[string]any{"intent": mergeMap(validIntent, "participant_id", intentID)}},
		{"human authority", "publish_intent", map[string]any{"intent": mergeMap(validIntent, "human_id", intentID)}},
		{"credential", "publish_intent", map[string]any{"intent": mergeMap(validIntent, "token", "conn_sess_not-a-token")}},
		{"unknown Intent property", "publish_intent", map[string]any{"intent": mergeMap(validIntent, "extra", true)}},
		{"non-declared claim provenance", "publish_intent", map[string]any{"intent": mergeMap(validIntent, "declared_claims", []any{map[string]any{
			"subject_kind": "invariant", "normalized_subject": "invariant:checkout:one-charge", "display_name": "Checkout idempotency",
			"relation": "changes", "provenance_kind": "model_inference", "confidence": 0.9,
		}})}},
		{"invalid evidence state", "attach_evidence", map[string]any{"intent_id": intentID, "evidence": mergeMap(validEvidence, "state", "complete")}},
		{"raw evidence log", "attach_evidence", map[string]any{"intent_id": intentID, "evidence": mergeMap(validEvidence, "raw_log", "all output")}},
		{"binding proposal field", "propose_resolution", map[string]any{"collision_id": collisionID, "proposal": map[string]any{"kind": "sequencing", "rationale_markdown": "Land the producer first.", "binding": true}}},
		{"zero expected revision", "revise_intent", map[string]any{"intent_id": intentID, "expected_revision": 0, "intent": validIntent}},
		{"analysis authority selector", "analyze_intent", map[string]any{"intent_id": intentID, "expected_intent_revision": 1, "participant_id": intentID}},
		{"analysis unknown field", "analyze_intent", map[string]any{"intent_id": intentID, "expected_intent_revision": 1, "analyzer_script": "unsafe.mjs"}},
		{"analysis zero revision", "analyze_intent", map[string]any{"intent_id": intentID, "expected_intent_revision": 0}},
		{"work-context authority selector", "read_work_context", map[string]any{"human_id": intentID}},
		{"work-context unknown field", "read_work_context", map[string]any{"diagnostics": []any{}}},
	}
	for _, tc := range rejected {
		t.Run("rejects "+tc.name, func(t *testing.T) {
			before := backendCalls.Load()
			result, err := clientSession.CallTool(ctx, &sdk.CallToolParams{Name: tc.tool, Arguments: tc.args})
			if err != nil {
				t.Fatal(err)
			}
			if result == nil || !result.IsError || strings.Contains(toolResultText(result), marker.Error()) {
				t.Fatalf("schema did not reject input before the handler: %s", toolResultText(result))
			}
			if got := backendCalls.Load(); got != before {
				t.Fatalf("rejected input reached backend: calls = %d, want %d", got, before)
			}
		})
	}
}

func toolSchemaObject(t *testing.T, tool *sdk.Tool) map[string]any {
	t.Helper()
	if tool == nil {
		t.Fatal("tool is missing")
	}
	data, err := json.Marshal(tool.InputSchema)
	if err != nil {
		t.Fatal(err)
	}
	var schema map[string]any
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatal(err)
	}
	return schema
}

func schemaProperties(t *testing.T, schema map[string]any) map[string]any {
	t.Helper()
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema has no properties: %#v", schema)
	}
	return properties
}

func propertySchema(t *testing.T, schema map[string]any, name string) map[string]any {
	t.Helper()
	property, ok := schemaProperties(t, schema)[name].(map[string]any)
	if !ok {
		t.Fatalf("schema property %s is missing", name)
	}
	return property
}

func assertRequired(t *testing.T, schema map[string]any, want bool, names ...string) {
	t.Helper()
	required := make(map[string]bool)
	for _, value := range schema["required"].([]any) {
		name, _ := value.(string)
		required[name] = true
	}
	for _, name := range names {
		if required[name] != want {
			t.Errorf("required[%s] = %t, want %t", name, required[name], want)
		}
	}
}

func mergeMap(source map[string]any, key string, value any) map[string]any {
	result := make(map[string]any, len(source)+1)
	for name, item := range source {
		result[name] = item
	}
	result[key] = value
	return result
}

func toolResultText(result *sdk.CallToolResult) string {
	if result == nil {
		return ""
	}
	var text []string
	for _, content := range result.Content {
		if item, ok := content.(*sdk.TextContent); ok {
			text = append(text, item.Text)
		}
	}
	return strings.Join(text, "\n")
}
