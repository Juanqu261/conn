package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"uuid"

	"github.com/Juanqu261/conn/internal/analyzer"
	"github.com/Juanqu261/conn/internal/preedit"
	"github.com/Juanqu261/conn/internal/protocol"
	"github.com/Juanqu261/conn/internal/repositorygit"
	"github.com/Juanqu261/conn/internal/review"
)

type analyzeIntentInput struct {
	ProjectID        string   `json:"project_id,omitempty"`
	IntentID         string   `json:"intent_id"`
	ExpectedRevision int      `json:"expected_intent_revision"`
	Worktree         string   `json:"worktree,omitempty"`
	Paths            []string `json:"paths,omitempty"`
}

type analyzedClaimReference struct {
	ClaimID           uuid.UUID `json:"claim_id"`
	SubjectKind       string    `json:"subject_kind"`
	NormalizedSubject string    `json:"normalized_subject"`
	Relation          string    `json:"relation"`
	ProvenanceKind    string    `json:"provenance_kind"`
}

type analyzeIntentOutput struct {
	IntentID          uuid.UUID                      `json:"intent_id"`
	IntentRevision    int                            `json:"intent_revision"`
	ObservedClaims    []analyzedClaimReference       `json:"observed_claims"`
	BaseRevisionRef   string                         `json:"base_revision_ref,omitempty"`
	BaseSource        string                         `json:"base_source,omitempty"`
	Diagnostics       []protocol.AnalyzerDiagnostic  `json:"diagnostics"`
	Incomplete        bool                           `json:"incomplete"`
	Collisions        []protocol.CollisionAssessment `json:"collisions"`
	DecidedCollisions []protocol.CollisionAssessment `json:"decided_collisions"`
}

func shortRef(ref string) string {
	if len(ref) > 12 {
		return ref[:12]
	}
	if ref == "" {
		return "the base revision"
	}
	return ref
}

func (t tools) analyzeIntent(ctx context.Context, _ *sdk.CallToolRequest, in analyzeIntentInput) (*sdk.CallToolResult, any, error) {
	if in.ExpectedRevision < 1 {
		return t.fail("analyze_intent", errors.New("expected_intent_revision must be positive"))
	}
	intentID, err := parseID("intent_id", in.IntentID)
	if err != nil {
		return t.fail("analyze_intent", err)
	}
	client, projectID, err := t.session(ctx, in.ProjectID)
	if err != nil {
		return t.fail("analyze_intent", err)
	}

	// An exact work-context read proves that this adapter owns the selected
	// active Intent before local source analysis starts.
	before, err := client.WorkContext(ctx, projectID, uuid.UUID{}, intentID, nil, 1)
	if err != nil {
		return t.fail("analyze_intent", err)
	}
	intent, err := ownedActiveIntent(before.Data, intentID)
	if err != nil {
		return t.fail("analyze_intent", err)
	}
	if intent.Revision != in.ExpectedRevision {
		return t.fail("analyze_intent", fmt.Errorf("Intent %s is at revision %d, not expected revision %d; reread work context before analyzing", intentID, intent.Revision, in.ExpectedRevision))
	}
	if intent.RepositoryID == nil {
		return t.fail("analyze_intent", errors.New("the Intent must reference a RepositoryConnection before local analysis can run"))
	}

	repositories, err := client.RepositoryConnections(ctx, projectID)
	if err != nil {
		return t.fail("analyze_intent", err)
	}
	repository, err := analysisRepository(repositories.Data.Repositories, *intent.RepositoryID)
	if err != nil {
		return t.fail("analyze_intent", err)
	}
	if !repository.AnalysisEnabled {
		return t.fail("analyze_intent", errors.New("local analysis is disabled for the Intent's RepositoryConnection"))
	}

	worktree, paths, err := resolveAnalysisWorkspace(t.cfg.WorkspaceRoot, in.Worktree, in.Paths)
	if err != nil {
		return t.fail("analyze_intent", err)
	}
	metadata, err := t.discoverAnalysisRepository(ctx, worktree)
	if err != nil {
		return t.fail("analyze_intent", fmt.Errorf("identify the trusted analysis worktree: %w", err))
	}
	if metadata.CanonicalRemote != repository.CanonicalRemote {
		return t.fail("analyze_intent", errors.New("the local worktree does not match the Intent's RepositoryConnection"))
	}

	// Observed analysis compares the Intent base with the working tree. An
	// Intent without a recorded base uses the merge base with the default
	// branch; when neither exists, the analyzer reports conn-base-unavailable.
	base, baseSource := intent.BaseRevisionRef, "intent"
	if base == "" {
		baseSource = "merge_base"
		if computed, err := preedit.BaseRevision(ctx, worktree, repository.DefaultBranch); err == nil {
			base = computed
		}
	}
	report, err := analyzer.Run(ctx, t.cfg.Analyzer, analyzer.Request{
		Mode: protocol.AnalyzerModeObserved, Worktree: worktree, CanonicalRemote: repository.CanonicalRemote, Paths: paths, BaseRevisionRef: base,
	})
	if err != nil {
		return t.fail("analyze_intent", err)
	}
	if analyzer.HasDiagnostic(report, analyzer.DiagnosticBaseUnavailable) {
		// Attaching an empty report would use the revision's one analysis, so
		// nothing is attached and a retry stays possible.
		output := analyzeIntentOutput{
			IntentID: intentID, IntentRevision: in.ExpectedRevision, ObservedClaims: []analyzedClaimReference{},
			Diagnostics: report.Diagnostics, Incomplete: true, Collisions: []protocol.CollisionAssessment{}, DecidedCollisions: []protocol.CollisionAssessment{},
		}
		data, err := json.Marshal(output)
		if err != nil {
			return t.fail("analyze_intent", err)
		}
		return envelope(fmt.Sprintf("Analysis is incomplete: the base revision of Intent %s is unavailable, so no observed claims were attached. Set base_revision_ref with revise_intent or fetch origin, then retry.", intentID), data, before.AsOfEvent)
	}
	attached, err := client.AttachStaticAnalysisAtRevision(ctx, projectID, intentID, in.ExpectedRevision, report)
	if err != nil {
		return t.fail("analyze_intent", err)
	}
	if attached.Data.IntentRevision.Revision != in.ExpectedRevision {
		return t.fail("analyze_intent", errors.New("the Conn server attached analysis to an unexpected Intent revision"))
	}

	// Read authoritative post-commit Radar state so the result contains the
	// persisted claim references, collision effects, and a cursor at least as
	// new as the attachment.
	radar, err := client.Radar(ctx, projectID)
	if err != nil {
		return t.fail("analyze_intent", err)
	}
	if radar.AsOfEvent < attached.AsOfEvent {
		return t.fail("analyze_intent", errors.New("the Conn server returned a stale post-analysis event cursor"))
	}
	collisions := intentCollisions(radar.Data.OpenCollisions, intentID, in.ExpectedRevision)
	decided := intentCollisions(radar.Data.DecidedCollisions, intentID, in.ExpectedRevision)
	claims := analyzedClaimReferences(radar.Data.SemanticClaims, intentID, in.ExpectedRevision)
	output := analyzeIntentOutput{
		IntentID: intentID, IntentRevision: in.ExpectedRevision, ObservedClaims: claims, BaseRevisionRef: report.Repository.BaseRevisionRef, BaseSource: baseSource,
		Diagnostics: report.Diagnostics, Incomplete: len(report.Diagnostics) > 0, Collisions: collisions, DecidedCollisions: decided,
	}
	data, err := json.Marshal(output)
	if err != nil {
		return t.fail("analyze_intent", err)
	}
	summary := fmt.Sprintf("Attached %d source-free observed claim(s) from the diff against %s to Intent %s revision %d; %d collision(s) need attention.", len(claims), shortRef(report.Repository.BaseRevisionRef), intentID, in.ExpectedRevision, len(collisions))
	if output.Incomplete {
		summary += fmt.Sprintf(" The analyzer returned %d bounded diagnostic(s), so analysis is incomplete.", len(output.Diagnostics))
	}
	return envelope(summary, data, radar.AsOfEvent)
}

func analyzedClaimReferences(claims []protocol.SemanticClaim, intentID uuid.UUID, revision int) []analyzedClaimReference {
	result := make([]analyzedClaimReference, 0, len(claims))
	for _, claim := range claims {
		if claim.IntentID != intentID || claim.IntentRevision != revision || claim.ProvenanceKind != "static_analysis" {
			continue
		}
		result = append(result, analyzedClaimReference{
			ClaimID: claim.ClaimID, SubjectKind: claim.SubjectKind, NormalizedSubject: claim.NormalizedSubject,
			Relation: claim.Relation, ProvenanceKind: claim.ProvenanceKind,
		})
	}
	return result
}

func ownedActiveIntent(work protocol.WorkContext, intentID uuid.UUID) (protocol.IntentRevision, error) {
	for _, intent := range work.OwnedActiveIntents {
		if intent.IntentID == intentID {
			return intent, nil
		}
	}
	return protocol.IntentRevision{}, errors.New("the selected Intent is not an active Intent owned by this Kiro participant")
}

func analysisRepository(repositories []protocol.RepositoryConnection, repositoryID uuid.UUID) (protocol.RepositoryConnection, error) {
	for _, repository := range repositories {
		if repository.RepositoryID == repositoryID {
			return repository, nil
		}
	}
	return protocol.RepositoryConnection{}, errors.New("the Intent's RepositoryConnection is unavailable in the selected Project")
}

func intentCollisions(collisions []protocol.CollisionAssessment, intentID uuid.UUID, revision int) []protocol.CollisionAssessment {
	return review.IntentCollisions(collisions, intentID, revision)
}

func (t tools) discoverAnalysisRepository(ctx context.Context, worktree string) (repositorygit.Metadata, error) {
	if t.cfg.DiscoverRepositoryAt != nil {
		return t.cfg.DiscoverRepositoryAt(ctx, worktree)
	}
	return repositorygit.Discover(ctx, worktree)
}

// resolveAnalysisWorkspace confines model-supplied paths to one trusted local
// workspace. Existing paths are resolved through symlinks before containment is
// checked, so a lexical child cannot redirect analysis outside the worktree.
func resolveAnalysisWorkspace(workspaceRoot, requestedWorktree string, requestedPaths []string) (string, []string, error) {
	if workspaceRoot == "" {
		workspaceRoot = "."
	}
	root, err := resolvedDirectory(workspaceRoot)
	if err != nil {
		return "", nil, fmt.Errorf("resolve the trusted workspace: %w", err)
	}
	worktree := root
	if requestedWorktree != "" {
		candidate := requestedWorktree
		if !filepath.IsAbs(candidate) {
			candidate = filepath.Join(root, candidate)
		}
		worktree, err = resolvedDirectory(candidate)
		if err != nil {
			return "", nil, fmt.Errorf("resolve worktree: %w", err)
		}
		if !pathWithin(root, worktree) {
			return "", nil, errors.New("worktree must remain inside the trusted workspace")
		}
	}

	paths := make([]string, 0, len(requestedPaths))
	for _, requested := range requestedPaths {
		if requested == "" || filepath.IsAbs(requested) || filepath.VolumeName(requested) != "" || hasParentSegment(requested) {
			return "", nil, fmt.Errorf("analysis path %q must be a repository-relative path without traversal", requested)
		}
		cleaned := filepath.Clean(filepath.FromSlash(requested))
		if cleaned == "." {
			return "", nil, fmt.Errorf("analysis path %q must identify a file or directory", requested)
		}
		resolved, err := filepath.EvalSymlinks(filepath.Join(worktree, cleaned))
		if err != nil {
			return "", nil, fmt.Errorf("resolve analysis path %q: %w", requested, err)
		}
		resolved, err = filepath.Abs(resolved)
		if err != nil || !pathWithin(worktree, resolved) {
			return "", nil, fmt.Errorf("analysis path %q escapes the selected worktree", requested)
		}
		relative, err := filepath.Rel(worktree, resolved)
		if err != nil {
			return "", nil, fmt.Errorf("resolve analysis path %q: %w", requested, err)
		}
		paths = append(paths, filepath.ToSlash(relative))
	}
	return worktree, paths, nil
}

func resolvedDirectory(path string) (string, error) {
	absolute, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errors.New("path is not a directory")
	}
	return resolved, nil
}

func pathWithin(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}

func hasParentSegment(path string) bool {
	for _, part := range strings.FieldsFunc(path, func(r rune) bool { return r == '/' || r == '\\' }) {
		if part == ".." {
			return true
		}
	}
	return false
}
