package mcp

import (
	"context"
	"errors"
	"fmt"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Juanqu261/conn/internal/analyzer"
	"github.com/Juanqu261/conn/internal/auth/github"
	"github.com/Juanqu261/conn/internal/credentials"
	"github.com/Juanqu261/conn/internal/httpclient"
	"github.com/Juanqu261/conn/internal/protocol"
)

const (
	loginInputRequestID  = "authorize_github"
	logoutInputRequestID = "confirm_logout"
)

type sessionStatusOutput struct {
	Server           sessionServerStatus     `json:"server"`
	Credential       sessionCredentialStatus `json:"credential"`
	Human            *sessionHumanStatus     `json:"human,omitempty"`
	ProjectSelection projectSelectionStatus  `json:"project_selection"`
	Analyzer         analyzerStatus          `json:"analyzer"`
	Capabilities     clientCapabilityStatus  `json:"capabilities"`
	Problems         []string                `json:"problems"`
}

type sessionHumanStatus struct {
	HumanID      string `json:"human_id"`
	GitHubUserID int64  `json:"github_user_id"`
	GitHubLogin  string `json:"github_login"`
}

type sessionServerStatus struct {
	URL       string `json:"url"`
	Reachable bool   `json:"reachable"`
	Ready     bool   `json:"ready"`
}

type sessionCredentialStatus struct {
	StoreAvailable bool   `json:"store_available"`
	Stored         bool   `json:"stored"`
	Source         string `json:"source,omitempty"`
	Valid          bool   `json:"valid"`
}

type projectSelectionStatus struct {
	State           string `json:"state"`
	Source          string `json:"source,omitempty"`
	ProjectID       string `json:"project_id,omitempty"`
	RepositoryMatch string `json:"repository_match,omitempty"`
}

type analyzerStatus struct {
	Available   bool   `json:"available"`
	Source      string `json:"source,omitempty"`
	NodeSource  string `json:"node_source,omitempty"`
	NodeVersion string `json:"node_version,omitempty"`
}

type clientCapabilityStatus struct {
	ProtocolVersion string `json:"protocol_version,omitempty"`
	ClientName      string `json:"client_name,omitempty"`
	// ParticipantAdapter is the attribution of this client's Participant:
	// kiro for Kiro, mcp for other MCP clients.
	ParticipantAdapter string `json:"participant_adapter"`
	FormInput          bool   `json:"form_input"`
	URLInput           bool   `json:"url_input"`
	StructuredResult   bool   `json:"structured_result"`
	Prompts            bool   `json:"prompts"`
	ResourceTemplates  bool   `json:"resource_templates"`
	IdlePush           bool   `json:"idle_push"`
	InteractiveLogin   bool   `json:"interactive_login"`
	ConfirmedLogout    bool   `json:"confirmed_logout"`
}

type loginOutput struct {
	LoggedIn    bool                `json:"logged_in"`
	Human       *sessionHumanStatus `json:"human,omitempty"`
	ExpiresAt   string              `json:"expires_at,omitempty"`
	EnvOverride bool                `json:"env_override"`
	Message     string              `json:"message"`
}

type logoutOutput struct {
	LoggedOut          bool   `json:"logged_out"`
	Server             string `json:"server"`
	GitHubLogin        string `json:"github_login,omitempty"`
	ServerRevoked      bool   `json:"server_revoked"`
	EnvOverrideRemains bool   `json:"env_override_remains"`
	Message            string `json:"message"`
}

func registerAccountTools(server *sdk.Server, t tools) {
	closed, destructive := false, true
	sdk.AddTool(server, &sdk.Tool{
		Name:        "session_status",
		Description: "Report safe Conn backend, credential, identity, Project-selection, analyzer, and MCP client-capability diagnostics. Never returns a token.",
		InputSchema: objectSchema(nil, map[string]any{}),
		Annotations: &sdk.ToolAnnotations{Title: "Conn session status", ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: &closed},
	}, t.sessionStatus)
	sdk.AddTool(server, &sdk.Tool{
		Name:        "login",
		Description: "Sign in through direct GitHub Device Flow interaction and store only the Conn session in the operating-system credential store.",
		InputSchema: objectSchema(nil, map[string]any{}),
		Annotations: &sdk.ToolAnnotations{Title: "Log in to Conn", ReadOnlyHint: false, DestructiveHint: &closed, IdempotentHint: false, OpenWorldHint: &destructive},
	}, t.login)
	sdk.AddTool(server, &sdk.Tool{
		Name:        "logout",
		Description: "Directly confirm, revoke, and remove the stored Conn session. This does not unset CONN_TOKEN.",
		InputSchema: objectSchema(nil, map[string]any{}),
		Annotations: &sdk.ToolAnnotations{Title: "Log out of Conn", ReadOnlyHint: false, DestructiveHint: &destructive, IdempotentHint: true, OpenWorldHint: &closed},
	}, t.logout)
}

func (t tools) sessionStatus(ctx context.Context, req *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, sessionStatusOutput, error) {
	output := sessionStatusOutput{
		ProjectSelection: projectSelectionStatus{State: "none"},
		Analyzer:         t.analyzerStatus(ctx),
		Capabilities:     clientCapabilities(req),
		Problems:         []string{},
	}
	if t.cfg.Account == nil {
		output.Problems = append(output.Problems, "account_operations_unavailable")
		return statusResult("Conn account operations are unavailable in this adapter configuration."), output, nil
	}

	status := t.cfg.Account.SessionStatus(ctx)
	output.Server = sessionServerStatus{URL: status.ServerURL, Reachable: status.ServerReachable, Ready: status.ServerReady}
	output.Credential = sessionCredentialStatus{
		StoreAvailable: status.CredentialStoreAvailable,
		Stored:         status.StoredCredentialPresent,
		Source:         string(status.CredentialSource),
		Valid:          status.CredentialValid,
	}
	output.Human = sessionHuman(status.Human)
	output.Problems = append(output.Problems, status.Problems...)
	if selected, ok := t.selection.get(); ok {
		output.ProjectSelection = projectSelectionStatus{
			State:           "selected",
			Source:          string(selected.Source),
			ProjectID:       selected.ProjectID.String(),
			RepositoryMatch: selected.RepositoryMatch,
		}
	} else if t.cfg.DefaultProject != "" {
		output.ProjectSelection = projectSelectionStatus{State: "configured", Source: "env", ProjectID: t.cfg.DefaultProject}
		if _, err := parseID("CONN_PROJECT", t.cfg.DefaultProject); err != nil {
			output.ProjectSelection.State = "invalid"
			output.Problems = append(output.Problems, "invalid_project_selection")
		}
	}
	if !output.Analyzer.Available {
		output.Problems = append(output.Problems, "analyzer_unavailable")
	}

	summary := "Conn session is not authenticated."
	if output.Human != nil && output.Credential.Valid {
		summary = fmt.Sprintf("Authenticated to %s as @%s.", output.Server.URL, output.Human.GitHubLogin)
	} else if !output.Server.Reachable {
		summary = fmt.Sprintf("Conn backend %s is unreachable.", output.Server.URL)
	} else if !output.Server.Ready {
		summary = fmt.Sprintf("Conn backend %s is not ready.", output.Server.URL)
	}
	return statusResult(summary), output, nil
}

func (t tools) analyzerStatus(ctx context.Context) analyzerStatus {
	if t.cfg.AnalyzerLocation == nil {
		return analyzerStatus{Available: t.cfg.AnalyzerAvailable != nil && t.cfg.AnalyzerAvailable()}
	}
	location := t.cfg.AnalyzerLocation()
	status := analyzerStatus{Available: location.Available(), Source: location.AnalyzerSource, NodeSource: location.NodeSource}
	if location.NodeAvailable {
		status.NodeVersion = analyzer.NodeVersion(ctx, location.Config.NodePath)
	}
	return status
}

func (t tools) login(ctx context.Context, req *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, loginOutput, error) {
	if t.cfg.Account == nil || t.interactions == nil {
		return nil, loginOutput{}, errors.New("interactive login is unavailable in this adapter configuration")
	}
	if !clientCapabilities(req).URLInput {
		return nil, loginOutput{}, fmt.Errorf("interactive login unavailable: this client cannot collect mid-session URL input; set %s explicitly for non-interactive use", credentials.TokenEnv)
	}

	if len(req.Params.InputResponses) == 0 {
		attempt, err := t.cfg.Account.BeginLogin(ctx)
		if err != nil {
			return nil, loginOutput{}, accountOperationError(err)
		}
		state, err := t.interactions.addLogin(req.Session, attempt)
		if err != nil {
			t.cfg.Account.CancelLogin(attempt)
			return nil, loginOutput{}, err
		}
		authorization := attempt.Authorization()
		message := fmt.Sprintf("Enter one-time code %s at GitHub to authorize this Conn login. The code expires at %s.", authorization.UserCode, authorization.ExpiresAt.UTC().Format(time.RFC3339))
		if authorization.EnvOverride {
			message += " CONN_TOKEN is set and will remain the active credential after this stored login."
		}
		return &sdk.CallToolResult{
			InputRequests: sdk.InputRequestMap{
				loginInputRequestID: &sdk.ElicitParams{
					Mode:          "url",
					Message:       message,
					URL:           authorization.VerificationURI,
					ElicitationID: "conn-login-" + state,
				},
			},
			RequestState: state,
		}, loginOutput{}, nil
	}

	pending, err := t.interactions.take(req.Params.RequestState, req.Session, loginInteraction)
	if err != nil {
		return nil, loginOutput{}, err
	}
	response, err := elicitationResponse(req, loginInputRequestID)
	if err != nil {
		t.cfg.Account.CancelLogin(pending.login)
		return nil, loginOutput{}, err
	}
	if response.Action != "accept" {
		t.cfg.Account.CancelLogin(pending.login)
		output := loginOutput{Message: "GitHub authorization was cancelled; no Conn credential was stored."}
		return statusResult(output.Message), output, nil
	}

	result, err := t.cfg.Account.CompleteLogin(ctx, pending.login)
	if err != nil {
		return nil, loginOutput{}, accountOperationError(err)
	}
	message := fmt.Sprintf("Logged in to %s as @%s.", t.cfg.Account.Server.URL(), result.Human.GitHubLogin)
	if result.EnvOverride {
		message += " CONN_TOKEN remains the active credential until it is unset."
	}
	output := loginOutput{
		LoggedIn:    true,
		Human:       sessionHuman(&result.Human),
		ExpiresAt:   result.ExpiresAt.UTC().Format(time.RFC3339),
		EnvOverride: result.EnvOverride,
		Message:     message,
	}
	return statusResult(message), output, nil
}

func (t tools) logout(ctx context.Context, req *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, logoutOutput, error) {
	if t.cfg.Account == nil || t.interactions == nil {
		return nil, logoutOutput{}, errors.New("interactive logout is unavailable in this adapter configuration")
	}
	if !clientCapabilities(req).FormInput {
		return nil, logoutOutput{}, errors.New("interactive logout unavailable: this client cannot collect direct confirmation; use `conn logout`")
	}

	if len(req.Params.InputResponses) == 0 {
		human, err := t.cfg.Account.StoredWhoAmI(ctx)
		if err != nil {
			return nil, logoutOutput{}, accountOperationError(err)
		}
		state, err := t.interactions.addLogout(req.Session, human)
		if err != nil {
			return nil, logoutOutput{}, err
		}
		serverURL := t.cfg.Account.Server.URL()
		return &sdk.CallToolResult{
			InputRequests: sdk.InputRequestMap{
				logoutInputRequestID: &sdk.ElicitParams{
					Mode:    "form",
					Message: fmt.Sprintf("Confirm revoking the stored Conn session on %s for @%s. CONN_TOKEN, if set, will remain unchanged.", serverURL, human.GitHubLogin),
					RequestedSchema: map[string]any{
						"type":                 "object",
						"additionalProperties": false,
						"properties": map[string]any{
							"confirm": map[string]any{
								"type":        "boolean",
								"title":       "Revoke stored Conn session",
								"description": fmt.Sprintf("Revoke the session for @%s on %s and remove it from this machine.", human.GitHubLogin, serverURL),
								"default":     false,
							},
						},
						"required": []string{"confirm"},
					},
				},
			},
			RequestState: state,
		}, logoutOutput{}, nil
	}

	pending, err := t.interactions.take(req.Params.RequestState, req.Session, logoutInteraction)
	if err != nil {
		return nil, logoutOutput{}, err
	}
	response, err := elicitationResponse(req, logoutInputRequestID)
	if err != nil {
		return nil, logoutOutput{}, err
	}
	confirm, _ := response.Content["confirm"].(bool)
	if response.Action != "accept" || !confirm {
		message := "Logout was cancelled; the stored Conn session remains active."
		return statusResult(message), logoutOutput{Server: t.cfg.Account.Server.URL(), GitHubLogin: pending.logoutHuman.GitHubLogin, Message: message}, nil
	}
	current, err := t.cfg.Account.StoredWhoAmI(ctx)
	if err != nil {
		return nil, logoutOutput{}, accountOperationError(err)
	}
	if current.HumanID != pending.logoutHuman.HumanID {
		return nil, logoutOutput{}, errors.New("the stored Conn session changed before confirmation; start logout again")
	}

	result, err := t.cfg.Account.Logout(ctx)
	if err != nil {
		return nil, logoutOutput{}, accountOperationError(err)
	}
	output := logoutOutput{
		LoggedOut:          true,
		Server:             t.cfg.Account.Server.URL(),
		GitHubLogin:        pending.logoutHuman.GitHubLogin,
		ServerRevoked:      result.RevokeErr == nil,
		EnvOverrideRemains: result.EnvOverride,
		Message:            "The stored Conn session was removed from this machine.",
	}
	if result.RevokeErr == nil {
		output.Message = "The stored Conn session was revoked and removed from this machine."
	} else {
		output.Message += " The server could not confirm revocation; the session will stop working when it expires."
	}
	if result.EnvOverride {
		output.Message += " CONN_TOKEN is still set and remains active."
	}
	return statusResult(output.Message), output, nil
}

func accountOperationError(err error) error {
	switch {
	case errors.Is(err, credentials.ErrUnavailable):
		return fmt.Errorf("the operating-system credential store is unavailable; Conn will not use a plaintext fallback. Unlock or install the credential store, or set %s explicitly for non-interactive use", credentials.TokenEnv)
	case errors.Is(err, credentials.ErrNotLoggedIn):
		return errors.New("no stored Conn session is available")
	case errors.Is(err, credentials.ErrInvalid):
		return errors.New("the Conn credential is invalid; start login again")
	case errors.Is(err, github.ErrAccessDenied):
		return errors.New("GitHub authorization was denied; no Conn credential was stored")
	case errors.Is(err, github.ErrExpired):
		return errors.New("the GitHub device code expired; start login again")
	case errors.Is(err, github.ErrDeviceFlowDisabled):
		return errors.New("GitHub Device Flow is disabled for this Conn server")
	case errors.Is(err, context.Canceled):
		return context.Canceled
	case errors.Is(err, httpclient.ErrUnreachable):
		return errors.New("the Conn backend is unreachable; check session_status and retry when it is available")
	case httpclient.IsCode(err, "unauthenticated"):
		return errors.New("the Conn session expired or was revoked; start login again")
	case httpclient.IsCode(err, "login_not_configured"):
		return errors.New("this Conn backend has no GitHub OAuth client configured")
	default:
		return err
	}
}

func sessionHuman(human *protocol.Human) *sessionHumanStatus {
	if human == nil {
		return nil
	}
	return &sessionHumanStatus{
		HumanID:      human.HumanID.String(),
		GitHubUserID: human.GitHubUserID,
		GitHubLogin:  human.GitHubLogin,
	}
}

func statusResult(summary string) *sdk.CallToolResult {
	return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: summary}}}
}

func elicitationResponse(req *sdk.CallToolRequest, id string) (*sdk.ElicitResult, error) {
	response, ok := req.Params.InputResponses[id]
	if !ok {
		return nil, errors.New("direct user response is missing")
	}
	result, ok := response.(*sdk.ElicitResult)
	if !ok {
		return nil, errors.New("direct user response has an unexpected type")
	}
	return result, nil
}

func clientCapabilities(req *sdk.CallToolRequest) clientCapabilityStatus {
	status := clientCapabilityStatus{StructuredResult: true, Prompts: true, ResourceTemplates: true}
	status.ParticipantAdapter = adapterKiro
	if req == nil || req.Session == nil {
		return status
	}
	if info := req.ClientInfo(); info != nil {
		status.ClientName = info.Name
	}
	status.ParticipantAdapter = adapterKind(status.ClientName)
	params := req.Session.InitializeParams()
	if params == nil {
		return status
	}
	status.ProtocolVersion = params.ProtocolVersion
	if params.ClientInfo != nil && status.ClientName == "" {
		status.ClientName = params.ClientInfo.Name
		status.ParticipantAdapter = adapterKind(status.ClientName)
	}
	if params.Capabilities != nil && params.Capabilities.Elicitation != nil {
		input := params.Capabilities.Elicitation
		status.FormInput = input.Form != nil || input.URL == nil
		status.URLInput = input.URL != nil
	}
	status.InteractiveLogin = status.URLInput
	status.ConfirmedLogout = status.FormInput
	return status
}
