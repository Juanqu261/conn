package checkrun

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"

	"uuid"

	"github.com/Juanqu261/conn/internal/httpclient"
	"github.com/Juanqu261/conn/internal/protocol"
	"github.com/Juanqu261/conn/internal/repositorygit"
)

// Request runs one configured check for one Intent and attaches the result.
type Request struct {
	// Worktree is any directory inside the repository worktree.
	Worktree  string
	ProjectID uuid.UUID
	IntentID  uuid.UUID
	CheckKey  string
	// AcceptanceKeys override the check's configured keys when present.
	AcceptanceKeys []string
	Output         io.Writer
	Environ        []string
}

// Outcome reports a run and the evidence attached for it. When AttachErr is
// set, the run finished but nothing more was recorded.
type Outcome struct {
	Result    Result
	Evidence  []protocol.EvidenceReference
	AsOfEvent int64
	AttachErr error
}

// Prepared is a verified run that has not executed yet.
type Prepared struct {
	Execution      Execution
	Intent         protocol.IntentRevision
	AcceptanceKeys []string
}

// Prepare verifies everything that must hold before a check executes: the
// Intent belongs to the signed-in Human and is active, the worktree remote
// matches the Intent repository, the configuration is valid, the check exists,
// and every acceptance key exists on the Intent's Objective revision.
func Prepare(ctx context.Context, client *httpclient.Client, request Request) (Prepared, error) {
	root, err := repositorygit.TopLevel(ctx, request.Worktree)
	if err != nil {
		return Prepared{}, err
	}
	root = filepath.Clean(filepath.FromSlash(root))
	gitDir, err := repositorygit.GitDir(ctx, root)
	if err != nil {
		return Prepared{}, err
	}
	work, err := client.WorkContext(ctx, request.ProjectID, uuid.UUID{}, request.IntentID, nil, 1)
	if err != nil {
		return Prepared{}, err
	}
	if work.Data.Focus == nil || work.Data.Focus.SelectedIntent == nil || work.Data.Focus.SelectedIntent.IntentID != request.IntentID {
		return Prepared{}, errors.New("the Intent does not resolve to one exact focus for the signed-in Human")
	}
	intent := *work.Data.Focus.SelectedIntent
	view, err := client.Intent(ctx, request.ProjectID, request.IntentID)
	if err != nil {
		return Prepared{}, err
	}
	if view.Data.Status != "active" {
		return Prepared{}, &ConfigError{Message: fmt.Sprintf("Intent %s is %s; evidence attaches only to active Intents", request.IntentID, view.Data.Status)}
	}
	if intent.RepositoryID == nil {
		return Prepared{}, &ConfigError{Message: "the Intent names no repository, so Conn cannot confirm this worktree belongs to it"}
	}
	repositories, err := client.RepositoryConnections(ctx, request.ProjectID)
	if err != nil {
		return Prepared{}, err
	}
	var remote string
	for _, repository := range repositories.Data.Repositories {
		if repository.RepositoryID == *intent.RepositoryID {
			remote = repository.CanonicalRemote
		}
	}
	metadata, err := repositorygit.Discover(ctx, root)
	if remote == "" || err != nil || metadata.CanonicalRemote != remote {
		return Prepared{}, &ConfigError{Message: "this worktree's origin does not match the Intent's repository connection"}
	}

	config, err := LoadConfig(root)
	if err != nil {
		return Prepared{}, err
	}
	check, err := config.Check(request.CheckKey)
	if err != nil {
		return Prepared{}, err
	}
	keys := request.AcceptanceKeys
	if len(keys) == 0 {
		keys = check.AcceptanceKeys
	}
	available := []string{}
	for _, point := range work.Data.Focus.Objective.KeyPoints {
		if point.Kind == protocol.KeyPointAcceptanceCriterion {
			available = append(available, point.Key)
		}
	}
	for _, key := range keys {
		if !slices.Contains(available, key) {
			return Prepared{}, &ConfigError{Message: fmt.Sprintf("acceptance key %q does not exist on Objective %s revision %d", key, intent.ObjectiveID, intent.ObjectiveRevision)}
		}
	}
	if _, err := WorkingDirectory(root, check); err != nil {
		return Prepared{}, err
	}
	return Prepared{
		Execution: Execution{
			Root: root, GitDir: filepath.Clean(filepath.FromSlash(gitDir)), Key: request.CheckKey, Check: check,
			ConfigSHA256: config.SHA256, Output: request.Output, Environ: request.Environ,
		},
		Intent: intent, AcceptanceKeys: keys,
	}, nil
}

// RunAndAttach prepares, executes, and attaches one check run: one
// check_run evidence reference per acceptance key, or one without a key.
func RunAndAttach(ctx context.Context, client *httpclient.Client, request Request) (Outcome, error) {
	prepared, err := Prepare(ctx, client, request)
	if err != nil {
		return Outcome{}, err
	}
	result, err := Execute(ctx, prepared.Execution)
	if err != nil {
		return Outcome{}, err
	}
	outcome := Outcome{Result: result}
	state := protocol.EvidenceFailing
	if Passed(result.Observation) {
		state = protocol.EvidencePassing
	}
	keys := prepared.AcceptanceKeys
	if len(keys) == 0 {
		keys = []string{""}
	}
	commit := result.Observation.Commit
	if len(commit) > 12 {
		commit = commit[:12]
	}
	summary := fmt.Sprintf("Check %s exited %d in %d ms on the Intent owner's machine.", request.CheckKey, result.Observation.ExitCode, result.Observation.DurationMS)
	if result.Observation.TimedOut {
		summary = fmt.Sprintf("Check %s timed out after %d ms on the Intent owner's machine.", request.CheckKey, result.Observation.DurationMS)
	}
	for _, key := range keys {
		observation := result.Observation
		attached, err := client.AttachEvidence(ctx, request.ProjectID, request.IntentID, protocol.EvidenceAttachmentInput{
			IntentID: request.IntentID, ObjectiveID: prepared.Intent.ObjectiveID, ObjectiveRevision: prepared.Intent.ObjectiveRevision,
			AcceptanceKey: key, Kind: protocol.EvidenceKindCheckRun, State: state,
			Locator: fmt.Sprintf("conn-check:%s@%s", request.CheckKey, commit), ContentHash: observation.OutputSHA256,
			Summary: summary, Observation: &observation,
		})
		if err != nil {
			outcome.AttachErr = err
			break
		}
		outcome.Evidence = append(outcome.Evidence, attached.Data)
		outcome.AsOfEvent = attached.AsOfEvent
	}
	if len(outcome.Evidence) > 0 {
		named := filepath.Join(filepath.Dir(result.LogPath), outcome.Evidence[0].EvidenceID.String()+".log")
		if os.Rename(result.LogPath, named) == nil {
			outcome.Result.LogPath = named
		}
	}
	return outcome, nil
}
