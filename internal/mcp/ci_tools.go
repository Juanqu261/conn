package mcp

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Juanqu261/conn/internal/ciaddon"
)

type ciCheckInput struct {
	ConnRef string `json:"conn_ref,omitempty"`
	Comment bool   `json:"comment,omitempty"`
}

type ciCheckOutput struct {
	Action        string                 `json:"action"`
	TargetPath    string                 `json:"target_path"`
	Workflow      string                 `json:"workflow,omitempty"`
	Difference    []string               `json:"difference,omitempty"`
	Prerequisites []ciaddon.Prerequisite `json:"prerequisites"`
}

// registerCITools adds the CI add-on tools. They write only the workflow file
// under the trusted workspace root and never create credentials, set secrets
// or variables, commit, push, or call GitHub.
func registerCITools(server *sdk.Server, t tools) {
	closed, destructive := false, true
	sdk.AddTool(server, &sdk.Tool{
		Name: "preview_ci_check",
		Description: "Show the Conn pull-request check workflow, its target path under the trusted workspace, and the prerequisites with exact commands. Writes nothing. " +
			"The check reports Conn results on pull requests and never fails them for findings. A Project owner creates its CI credential in the terminal; never create credentials or set secrets yourself.",
		InputSchema: ciCheckToolSchema(),
		Annotations: &sdk.ToolAnnotations{Title: "Preview Conn CI check", ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: &closed},
	}, t.previewCICheck)
	sdk.AddTool(server, &sdk.Tool{
		Name:        "install_ci_check",
		Description: "Write .github/workflows/conn-check.yml under the trusted workspace root after preview_ci_check. Refuses to overwrite different content. It never commits, pushes, creates credentials, or sets secrets or variables.",
		InputSchema: ciCheckToolSchema(),
		Annotations: &sdk.ToolAnnotations{Title: "Install Conn CI check", ReadOnlyHint: false, DestructiveHint: &closed, IdempotentHint: true, OpenWorldHint: &closed},
	}, t.installCICheck)
	sdk.AddTool(server, &sdk.Tool{
		Name:        "remove_ci_check",
		Description: "Remove .github/workflows/conn-check.yml only when it is byte-identical Conn content; a modified file is preserved for manual review.",
		InputSchema: objectSchema(nil, map[string]any{}),
		Annotations: &sdk.ToolAnnotations{Title: "Remove Conn CI check", ReadOnlyHint: false, DestructiveHint: &destructive, IdempotentHint: true, OpenWorldHint: &closed},
	}, t.removeCICheck)
}

func (t tools) ciPlan(mode ciaddon.Mode, in ciCheckInput) (ciaddon.Plan, error) {
	root, err := resolvedDirectory(workspaceRoot(t.cfg.WorkspaceRoot))
	if err != nil {
		return ciaddon.Plan{}, errors.New("the trusted workspace could not be resolved")
	}
	options := ciaddon.Options{ConnRef: in.ConnRef, Comment: in.Comment}
	if mode == ciaddon.ModeInstall && options.ConnRef == "" {
		if options.ConnRef, err = ciaddon.DefaultRef(); err != nil {
			return ciaddon.Plan{}, errors.New("this conn build has no clean source revision; pass conn_ref with a Conn commit SHA")
		}
	}
	return ciaddon.Preview(root, mode, options)
}

func (t tools) ciOutput(ctx context.Context, plan ciaddon.Plan) ciCheckOutput {
	output := ciCheckOutput{Action: string(plan.Action), TargetPath: filepath.ToSlash(ciaddon.TargetPath), Prerequisites: ciaddon.Prerequisites(t.ciPrerequisiteInfo(ctx, plan.Root))}
	if plan.Mode == ciaddon.ModeInstall {
		output.Workflow = string(plan.Content)
	}
	if plan.Action == ciaddon.ActionConflict && plan.Existing != nil && plan.Content != nil {
		output.Difference = ciaddon.Difference(plan.Existing, plan.Content, 40)
	}
	return output
}

// ciPrerequisiteInfo fills the commands from the trusted worktree and the
// session's matching RepositoryConnection when it can be read.
func (t tools) ciPrerequisiteInfo(ctx context.Context, root string) ciaddon.PrerequisiteInfo {
	info := ciaddon.PrerequisiteInfo{}
	metadata, err := t.discoverAnalysisRepository(ctx, root)
	if err != nil {
		return info
	}
	if repo, found := strings.CutPrefix(metadata.CanonicalRemote, "github.com/"); found {
		info.GitHubRepo = repo
	}
	client, projectID, err := t.session(ctx, "")
	if err != nil {
		return info
	}
	info.ServerURL = client.URL()
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

func (t tools) previewCICheck(ctx context.Context, _ *sdk.CallToolRequest, in ciCheckInput) (*sdk.CallToolResult, ciCheckOutput, error) {
	plan, err := t.ciPlan(ciaddon.ModeInstall, in)
	if err != nil {
		return nil, ciCheckOutput{}, t.toolError("preview_ci_check", err)
	}
	output := t.ciOutput(ctx, plan)
	summary := fmt.Sprintf("Conn check workflow %s at %s. Nothing was written. Prerequisites: an HTTPS Conn backend reachable from GitHub runners, a CI credential the Project owner creates with conn ci credential create, and the CONN_SERVER_URL repository variable.", plan.Action, output.TargetPath)
	if plan.Action == ciaddon.ActionConflict {
		summary = fmt.Sprintf("A different workflow exists at %s; install_ci_check would refuse to overwrite it. Nothing was written.", output.TargetPath)
	}
	return statusResult(summary), output, nil
}

func (t tools) installCICheck(ctx context.Context, _ *sdk.CallToolRequest, in ciCheckInput) (*sdk.CallToolResult, ciCheckOutput, error) {
	plan, err := t.ciPlan(ciaddon.ModeInstall, in)
	if err != nil {
		return nil, ciCheckOutput{}, t.toolError("install_ci_check", err)
	}
	if plan.Action == ciaddon.ActionConflict {
		return nil, ciCheckOutput{}, t.toolError("install_ci_check", errors.New("a different workflow exists at .github/workflows/conn-check.yml; it was preserved"))
	}
	if err := ciaddon.Apply(plan); err != nil {
		return nil, ciCheckOutput{}, t.toolError("install_ci_check", err)
	}
	output := t.ciOutput(ctx, plan)
	return statusResult(fmt.Sprintf("Wrote %s (%s). Ask the human to complete the prerequisites, then commit and push it; Conn did not commit, push, create credentials, or set secrets.", output.TargetPath, plan.Action)), output, nil
}

func (t tools) removeCICheck(ctx context.Context, _ *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, ciCheckOutput, error) {
	plan, err := t.ciPlan(ciaddon.ModeRemove, ciCheckInput{})
	if err != nil {
		return nil, ciCheckOutput{}, t.toolError("remove_ci_check", err)
	}
	if plan.Action == ciaddon.ActionConflict {
		return nil, ciCheckOutput{}, t.toolError("remove_ci_check", errors.New("the workflow is not unchanged Conn content; it was preserved for manual review"))
	}
	if err := ciaddon.Apply(plan); err != nil {
		return nil, ciCheckOutput{}, t.toolError("remove_ci_check", err)
	}
	output := ciCheckOutput{Action: string(plan.Action), TargetPath: filepath.ToSlash(ciaddon.TargetPath), Prerequisites: []ciaddon.Prerequisite{}}
	return statusResult(fmt.Sprintf("Conn check workflow: %s.", plan.Action)), output, nil
}
