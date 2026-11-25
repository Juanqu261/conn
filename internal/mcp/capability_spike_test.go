package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestPinnedSDKKiroCapabilities proves that the pinned official SDK can carry
// the Kiro-native surfaces over the same newline-delimited IO transport used by
// stdio MCP. Kiro UI behavior is validated separately because it is a client
// responsibility, not an SDK guarantee.
func TestPinnedSDKKiroCapabilities(t *testing.T) {
	server := sdk.NewServer(&sdk.Implementation{Name: "conn-capability-spike", Version: "0"}, nil)

	server.AddPrompt(&sdk.Prompt{
		Name:        "conn-start-spike",
		Description: "Load a focused Conn work context.",
		Arguments: []*sdk.PromptArgument{
			{Name: "focus", Description: "Optional objective or Intent focus."},
		},
	}, func(_ context.Context, req *sdk.GetPromptRequest) (*sdk.GetPromptResult, error) {
		focus := req.Params.Arguments["focus"]
		return &sdk.GetPromptResult{
			Description: "Start a bounded Conn work session.",
			Messages: []*sdk.PromptMessage{{
				Role:    sdk.Role("user"),
				Content: &sdk.TextContent{Text: "Read Conn work context for " + focus + "."},
			}},
		}, nil
	})

	server.AddResourceTemplate(&sdk.ResourceTemplate{
		Name:        "conn-work-context-spike",
		Title:       "Conn work context",
		Description: "Read a bounded project work context.",
		MIMEType:    "application/json",
		URITemplate: "conn://projects/{project_id}/work-context",
	}, func(_ context.Context, req *sdk.ReadResourceRequest) (*sdk.ReadResourceResult, error) {
		return &sdk.ReadResourceResult{Contents: []*sdk.ResourceContents{{
			URI:      req.Params.URI,
			MIMEType: "application/json",
			Text:     `{"project":"alpha","as_of_event":7}`,
		}}}, nil
	})

	type structuredInput struct {
		Value string `json:"value" jsonschema:"value to return"`
	}
	type structuredOutput struct {
		Value string `json:"value"`
	}
	sdk.AddTool(server, &sdk.Tool{
		Name:        "structured_spike",
		Description: "Return a structured compatibility result.",
	}, func(_ context.Context, _ *sdk.CallToolRequest, in structuredInput) (*sdk.CallToolResult, structuredOutput, error) {
		return nil, structuredOutput{Value: in.Value}, nil
	})

	type elicitInput struct {
		Mode string `json:"mode" jsonschema:"elicitation mode: form or url"`
	}
	type elicitOutput struct {
		Action  string         `json:"action"`
		Content map[string]any `json:"content,omitempty"`
	}
	sdk.AddTool(server, &sdk.Tool{
		Name:        "elicit_spike",
		Description: "Request bounded form or URL elicitation.",
	}, func(_ context.Context, req *sdk.CallToolRequest, in elicitInput) (*sdk.CallToolResult, elicitOutput, error) {
		params := &sdk.ElicitParams{Mode: in.Mode, Message: "Confirm the Conn compatibility spike."}
		switch in.Mode {
		case "form":
			params.RequestedSchema = map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"confirm": map[string]any{"type": "boolean"},
				},
				"required": []string{"confirm"},
			}
		case "url":
			params.URL = "https://example.com/conn-compatibility"
			params.ElicitationID = "conn-compatibility-1"
		default:
			return nil, elicitOutput{}, errors.New("unsupported elicitation mode")
		}
		pending, result, err := capabilityElicitationRoundTrip(req, in.Mode, in.Mode+"-step-1", params)
		if err != nil || pending != nil {
			return pending, elicitOutput{}, err
		}
		return nil, elicitOutput{Action: result.Action, Content: result.Content}, nil
	})

	cancelStarted := make(chan struct{}, 1)
	cancelObserved := make(chan struct{}, 1)
	sdk.AddTool(server, &sdk.Tool{
		Name:        "cancel_spike",
		Description: "Wait until the client cancels this request.",
	}, func(ctx context.Context, _ *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, struct{}, error) {
		cancelStarted <- struct{}{}
		<-ctx.Done()
		cancelObserved <- struct{}{}
		return nil, struct{}{}, ctx.Err()
	})

	var elicitationModes []string
	client := sdk.NewClient(&sdk.Implementation{Name: "conn-capability-client", Version: "0"}, &sdk.ClientOptions{
		Capabilities: &sdk.ClientCapabilities{Elicitation: &sdk.ElicitationCapabilities{
			Form: &sdk.FormElicitationCapabilities{},
			URL:  &sdk.URLElicitationCapabilities{},
		}},
		ElicitationHandler: func(_ context.Context, req *sdk.ElicitRequest) (*sdk.ElicitResult, error) {
			elicitationModes = append(elicitationModes, req.Params.Mode)
			switch req.Params.Mode {
			case "form":
				return &sdk.ElicitResult{Action: "accept", Content: map[string]any{"confirm": true}}, nil
			case "url":
				if req.Params.URL == "" || req.Params.ElicitationID == "" {
					return nil, errors.New("URL elicitation omitted its URL or ID")
				}
				return &sdk.ElicitResult{Action: "accept"}, nil
			default:
				return nil, errors.New("unexpected elicitation mode")
			}
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, closeSession := connectIOSpike(t, ctx, server, client)
	defer closeSession()

	caps := session.InitializeResult().Capabilities
	if caps == nil || caps.Prompts == nil || caps.Resources == nil || caps.Tools == nil {
		t.Fatalf("server capabilities = %+v, want prompts, resources, and tools", caps)
	}

	prompts, err := session.ListPrompts(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(prompts.Prompts) != 1 || prompts.Prompts[0].Name != "conn-start-spike" || len(prompts.Prompts[0].Arguments) != 1 {
		t.Fatalf("prompts = %+v", prompts.Prompts)
	}
	prompt, err := session.GetPrompt(ctx, &sdk.GetPromptParams{
		Name: "conn-start-spike", Arguments: map[string]string{"focus": "checkout"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(prompt.Messages) != 1 || prompt.Messages[0].Content.(*sdk.TextContent).Text != "Read Conn work context for checkout." {
		t.Fatalf("prompt result = %+v", prompt)
	}

	templates, err := session.ListResourceTemplates(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(templates.ResourceTemplates) != 1 || templates.ResourceTemplates[0].Name != "conn-work-context-spike" {
		t.Fatalf("resource templates = %+v", templates.ResourceTemplates)
	}
	resource, err := session.ReadResource(ctx, &sdk.ReadResourceParams{URI: "conn://projects/alpha/work-context"})
	if err != nil {
		t.Fatal(err)
	}
	if len(resource.Contents) != 1 || !strings.Contains(resource.Contents[0].Text, `"as_of_event":7`) {
		t.Fatalf("resource = %+v", resource)
	}

	structured, err := session.CallTool(ctx, &sdk.CallToolParams{
		Name: "structured_spike", Arguments: map[string]any{"value": "ok"},
	})
	if err != nil || structured.IsError {
		t.Fatalf("structured tool: %v, %+v", err, structured)
	}
	var gotStructured structuredOutput
	data, err := json.Marshal(structured.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &gotStructured); err != nil || gotStructured.Value != "ok" {
		t.Fatalf("structured content = %s, %v", data, err)
	}
	if len(structured.Content) == 0 {
		t.Fatal("structured tool omitted its text compatibility fallback")
	}

	for _, mode := range []string{"form", "url"} {
		result, err := session.CallTool(ctx, &sdk.CallToolParams{
			Name: "elicit_spike", Arguments: map[string]any{"mode": mode},
		})
		if err != nil || result.IsError {
			t.Fatalf("%s elicitation: %v, %+v", mode, err, result)
		}
		var got elicitOutput
		data, err := json.Marshal(result.StructuredContent)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, &got); err != nil || got.Action != "accept" {
			t.Fatalf("%s elicitation output = %s, %v", mode, data, err)
		}
		if mode == "form" && got.Content["confirm"] != true {
			t.Fatalf("form content = %+v", got.Content)
		}
	}
	if strings.Join(elicitationModes, ",") != "form,url" {
		t.Fatalf("elicitation modes = %v", elicitationModes)
	}

	callCtx, cancelCall := context.WithCancel(ctx)
	callDone := make(chan error, 1)
	go func() {
		_, err := session.CallTool(callCtx, &sdk.CallToolParams{Name: "cancel_spike"})
		callDone <- err
	}()
	select {
	case <-cancelStarted:
		cancelCall()
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	select {
	case <-cancelObserved:
	case <-ctx.Done():
		t.Fatal("server handler did not observe client cancellation")
	}
	if err := <-callDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled call error = %v, want context canceled", err)
	}
}

func TestPinnedSDKRejectsUnsupportedElicitation(t *testing.T) {
	server := sdk.NewServer(&sdk.Implementation{Name: "conn-capability-spike", Version: "0"}, nil)
	sdk.AddTool(server, &sdk.Tool{
		Name:        "elicit_without_capability",
		Description: "Verify client capability detection.",
	}, func(_ context.Context, req *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, struct{}, error) {
		pending, _, err := capabilityElicitationRoundTrip(req, "form", "unsupported-step-1", &sdk.ElicitParams{
			Mode:    "form",
			Message: "This client did not advertise elicitation.",
			RequestedSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		})
		return pending, struct{}{}, err
	})
	client := sdk.NewClient(&sdk.Implementation{Name: "no-elicitation-client", Version: "0"}, &sdk.ClientOptions{
		Capabilities: &sdk.ClientCapabilities{},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	session, closeSession := connectIOSpike(t, ctx, server, client)
	defer closeSession()

	result, err := session.CallTool(ctx, &sdk.CallToolParams{Name: "elicit_without_capability"})
	message := ""
	if err != nil {
		message = err.Error()
	} else if result != nil {
		message = spikeText(result)
	}
	if !strings.Contains(message, "does not support elicitation") {
		t.Fatalf("result = %+v, error = %v, want unsupported elicitation error", result, err)
	}
}

func connectIOSpike(t *testing.T, ctx context.Context, server *sdk.Server, client *sdk.Client) (*sdk.ClientSession, func()) {
	t.Helper()
	serverReader, clientWriter := io.Pipe()
	clientReader, serverWriter := io.Pipe()
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- server.Run(ctx, &sdk.IOTransport{Reader: serverReader, Writer: serverWriter})
	}()

	session, err := client.Connect(ctx, &sdk.IOTransport{Reader: clientReader, Writer: clientWriter}, &sdk.ClientSessionOptions{
		ProtocolVersion: "2026-07-28",
	})
	if err != nil {
		_ = clientWriter.Close()
		_ = clientReader.Close()
		_ = serverWriter.Close()
		_ = serverReader.Close()
		t.Fatal(err)
	}
	return session, func() {
		_ = session.Close()
		select {
		case <-serverDone:
		case <-time.After(2 * time.Second):
			t.Error("MCP IO server did not stop after client close")
		}
	}
}

func spikeText(result *sdk.CallToolResult) string {
	var parts []string
	for _, content := range result.Content {
		if text, ok := content.(*sdk.TextContent); ok {
			parts = append(parts, text.Text)
		}
	}
	return strings.Join(parts, "\n")
}
