package mcp

import (
	"context"
	"errors"
	"io"
	"os"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

const kiroCapabilityServerEnv = "CONN_RUN_KIRO_CAPABILITY_SERVER"

type kiroCapabilityEmptyInput struct{}

type kiroCapabilityMarker struct {
	Marker string `json:"marker"`
}

type kiroCapabilityResourceInput struct {
	ProjectID string `json:"project_id" jsonschema:"test project identifier"`
}

type kiroCapabilityReport struct {
	ProtocolVersion string `json:"protocol_version"`
	ClientName      string `json:"client_name,omitempty"`
	FormElicitation bool   `json:"form_elicitation"`
	URLElicitation  bool   `json:"url_elicitation"`
}

type kiroCapabilityElicitation struct {
	Action  string         `json:"action"`
	Content map[string]any `json:"content,omitempty"`
}

// TestKiroCapabilitySpikeServer is a test-only stdio MCP server for manual
// validation in a separate Kiro V3 session. Normal test runs skip it. Build the
// package test binary with `go test -c`, set kiroCapabilityServerEnv to 1 in
// the temporary MCP entry, and run only this test through -test.run.
func TestKiroCapabilitySpikeServer(t *testing.T) {
	if os.Getenv(kiroCapabilityServerEnv) != "1" {
		t.Skip("manual Kiro capability server")
	}

	server := newKiroCapabilityServer()
	err := server.Run(context.Background(), &sdk.IOTransport{Reader: os.Stdin, Writer: os.Stdout})
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func newKiroCapabilityServer() *sdk.Server {
	server := sdk.NewServer(&sdk.Implementation{
		Name:        "conn-capability-spike",
		Title:       "Conn capability spike",
		Description: "Temporary compatibility server for Conn's Kiro integration.",
		Version:     "0",
	}, nil)

	server.AddPrompt(&sdk.Prompt{
		Name:        "conn-capability-prompt",
		Title:       "Conn capability prompt",
		Description: "Verify Kiro prompt discovery and argument expansion.",
		Arguments: []*sdk.PromptArgument{
			{Name: "focus", Title: "Focus", Description: "A short test focus.", Required: true},
		},
	}, func(_ context.Context, req *sdk.GetPromptRequest) (*sdk.GetPromptResult, error) {
		focus := req.Params.Arguments["focus"]
		return &sdk.GetPromptResult{
			Description: "Temporary Conn prompt compatibility result.",
			Messages: []*sdk.PromptMessage{{
				Role:    sdk.Role("user"),
				Content: &sdk.TextContent{Text: "Return exactly CONN_PROMPT_OK:" + focus + " and do not call tools."},
			}},
		}, nil
	})

	server.AddResourceTemplate(&sdk.ResourceTemplate{
		Name:        "conn-capability-resource",
		Title:       "Conn capability resource",
		Description: "Verify Kiro resource-template discovery and argument collection.",
		MIMEType:    "text/plain",
		URITemplate: "conn-spike://projects/{project_id}/context",
	}, func(_ context.Context, req *sdk.ReadResourceRequest) (*sdk.ReadResourceResult, error) {
		return &sdk.ReadResourceResult{Contents: []*sdk.ResourceContents{{
			URI:      req.Params.URI,
			MIMEType: "text/plain",
			Text:     "CONN_RESOURCE_OK:" + req.Params.URI,
		}}}, nil
	})

	sdk.AddTool(server, &sdk.Tool{
		Name:        "capability_report",
		Description: "Return the MCP protocol version and elicitation capabilities Kiro advertised.",
	}, func(_ context.Context, req *sdk.CallToolRequest, _ kiroCapabilityEmptyInput) (*sdk.CallToolResult, kiroCapabilityReport, error) {
		params := req.Session.InitializeParams()
		out := kiroCapabilityReport{}
		if params == nil {
			return nil, out, nil
		}
		out.ProtocolVersion = params.ProtocolVersion
		if params.ClientInfo != nil {
			out.ClientName = params.ClientInfo.Name
		}
		if params.Capabilities != nil && params.Capabilities.Elicitation != nil {
			caps := params.Capabilities.Elicitation
			out.FormElicitation = caps.Form != nil || caps.URL == nil
			out.URLElicitation = caps.URL != nil
		}
		return nil, out, nil
	})

	sdk.AddTool(server, &sdk.Tool{
		Name:        "structured_spike",
		Description: "Return the exact marker CONN_STRUCTURED_OK in structured and text-compatible output.",
	}, func(_ context.Context, _ *sdk.CallToolRequest, _ kiroCapabilityEmptyInput) (*sdk.CallToolResult, kiroCapabilityMarker, error) {
		return nil, kiroCapabilityMarker{Marker: "CONN_STRUCTURED_OK"}, nil
	})

	sdk.AddTool(server, &sdk.Tool{
		Name:        "resource_fallback_spike",
		Description: "Return the resource marker through a tool when the client has no resource-template selector.",
	}, func(_ context.Context, _ *sdk.CallToolRequest, in kiroCapabilityResourceInput) (*sdk.CallToolResult, kiroCapabilityMarker, error) {
		return nil, kiroCapabilityMarker{Marker: "CONN_RESOURCE_TOOL_OK:conn-spike://projects/" + in.ProjectID + "/context"}, nil
	})

	sdk.AddTool(server, &sdk.Tool{
		Name:        "form_elicitation_spike",
		Description: "Open a harmless confirmation form to verify interactive MCP form elicitation.",
	}, func(_ context.Context, req *sdk.CallToolRequest, _ kiroCapabilityEmptyInput) (*sdk.CallToolResult, kiroCapabilityElicitation, error) {
		pending, result, err := capabilityElicitationRoundTrip(req, "form", "form-step-1", &sdk.ElicitParams{
			Mode:    "form",
			Message: "Conn compatibility test only. Choose whether to confirm the form path.",
			RequestedSchema: map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"confirm": map[string]any{
						"type":        "boolean",
						"title":       "Confirm compatibility test",
						"description": "This changes no Conn or Kiro state.",
						"default":     false,
					},
				},
				"required": []string{"confirm"},
			},
		})
		if err != nil || pending != nil {
			return pending, kiroCapabilityElicitation{}, err
		}
		return nil, kiroCapabilityElicitation{Action: result.Action, Content: result.Content}, nil
	})

	sdk.AddTool(server, &sdk.Tool{
		Name:        "url_elicitation_spike",
		Description: "Open the public Kiro MCP usage documentation to verify interactive MCP URL elicitation.",
	}, func(_ context.Context, req *sdk.CallToolRequest, _ kiroCapabilityEmptyInput) (*sdk.CallToolResult, kiroCapabilityElicitation, error) {
		pending, result, err := capabilityElicitationRoundTrip(req, "url", "url-step-1", &sdk.ElicitParams{
			Mode:          "url",
			Message:       "Open the public Kiro MCP usage documentation. This changes no Conn or Kiro state.",
			URL:           "https://kiro.dev/docs/mcp/usage",
			ElicitationID: "conn-capability-url-1",
		})
		if err != nil || pending != nil {
			return pending, kiroCapabilityElicitation{}, err
		}
		return nil, kiroCapabilityElicitation{Action: result.Action, Content: result.Content}, nil
	})

	sdk.AddTool(server, &sdk.Tool{
		Name:        "cancellation_spike",
		Description: "Wait for up to 30 seconds so the human can press Escape and test request cancellation.",
	}, func(ctx context.Context, _ *sdk.CallToolRequest, _ kiroCapabilityEmptyInput) (*sdk.CallToolResult, kiroCapabilityMarker, error) {
		select {
		case <-ctx.Done():
			return nil, kiroCapabilityMarker{}, ctx.Err()
		case <-time.After(30 * time.Second):
			return nil, kiroCapabilityMarker{Marker: "CONN_CANCELLATION_NOT_OBSERVED"}, nil
		}
	})

	return server
}

func capabilityElicitationRoundTrip(req *sdk.CallToolRequest, id, state string, params *sdk.ElicitParams) (*sdk.CallToolResult, *sdk.ElicitResult, error) {
	if len(req.Params.InputResponses) == 0 {
		return &sdk.CallToolResult{
			InputRequests: sdk.InputRequestMap{id: params},
			RequestState:  state,
		}, nil, nil
	}
	if req.Params.RequestState != state {
		return nil, nil, errors.New("elicitation request state did not round-trip")
	}
	response, ok := req.Params.InputResponses[id]
	if !ok {
		return nil, nil, errors.New("elicitation response is missing")
	}
	result, ok := response.(*sdk.ElicitResult)
	if !ok {
		return nil, nil, errors.New("elicitation response has an unexpected type")
	}
	return nil, result, nil
}
