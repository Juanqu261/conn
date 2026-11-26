package preedit

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"uuid"

	"github.com/Juanqu261/conn/internal/analyzer"
	"github.com/Juanqu261/conn/internal/httpclient"
	"github.com/Juanqu261/conn/internal/protocol"
)

// Suggestion bounds.
const (
	MaxCandidates     = 100
	maxFocusRunes     = 200
	maxCatalogMatches = 50
	maxReasonRunes    = 1000
)

// Candidate sources, in merge priority order.
const (
	SourceAnalysis  = "analysis"
	SourceObjective = "objective"
	SourceCatalog   = "catalog"
)

// SuggestRequest describes the work a participant plans before editing.
type SuggestRequest struct {
	ProjectID   uuid.UUID
	ObjectiveID uuid.UUID
	// Worktree is the trusted, already resolved local worktree.
	Worktree string
	Paths    []string
	Symbols  map[string][]string
	Focus    string
}

// SuggestDeps are the local and backend capabilities suggestions use.
type SuggestDeps struct {
	Client            *httpclient.Client
	Analyzer          analyzer.Config
	AnalyzerAvailable bool
	Discover          Discover
}

// Candidate is one claim the participant may declare. Nothing is persisted
// until the participant includes it in publish_intent or revise_intent.
type Candidate struct {
	Claim  protocol.SemanticClaimCandidate `json:"claim"`
	Source string                          `json:"source"`
	Reason string                          `json:"reason"`
}

// AnalysisStatus reports whether planned local analysis contributed.
type AnalysisStatus struct {
	Available bool   `json:"available"`
	Mode      string `json:"mode"`
	Reason    string `json:"reason,omitempty"`
}

// Suggestions is the bounded, source-free suggestion result.
type Suggestions struct {
	Candidates   []Candidate                   `json:"candidates"`
	NewPaths     []string                      `json:"new_paths"`
	OmittedCount int                           `json:"omitted_count"`
	Diagnostics  []protocol.AnalyzerDiagnostic `json:"diagnostics"`
	Analysis     AnalysisStatus                `json:"analysis"`
	RepositoryID *uuid.UUID                    `json:"repository_id,omitempty"`
	AsOfEvent    int64                         `json:"-"`
}

// SuggestClaims turns planned paths, optional symbols, an Objective, and a
// free-text focus into declared-claim candidates. Analysis candidates come
// first, then Objective-derived subjects, then catalog matches. When the
// analyzer is unavailable, catalog candidates are still returned.
func SuggestClaims(ctx context.Context, deps SuggestDeps, request SuggestRequest) (Suggestions, error) {
	if len([]rune(request.Focus)) > maxFocusRunes {
		return Suggestions{}, fmt.Errorf("focus must be at most %d characters", maxFocusRunes)
	}
	paths, err := PlannedPaths(request.Worktree, request.Paths)
	if err != nil {
		return Suggestions{}, err
	}
	symbols, err := plannedSymbols(request.Worktree, paths, request.Symbols)
	if err != nil {
		return Suggestions{}, err
	}
	result := Suggestions{Candidates: []Candidate{}, NewPaths: []string{}, Diagnostics: []protocol.AnalyzerDiagnostic{}, Analysis: AnalysisStatus{Mode: protocol.AnalyzerModePlanned}}
	merged := newMerger()

	repository, reason := analysisRepository(ctx, deps, request)
	switch {
	case repository == nil:
		result.Analysis.Reason = reason
	case !deps.AnalyzerAvailable:
		result.Analysis.Reason = "The local TypeScript analyzer or Node.js is unavailable; run session_status or conn doctor."
	default:
		id := repository.RepositoryID
		result.RepositoryID = &id
		report, err := analyzer.Run(ctx, deps.Analyzer, analyzer.Request{
			Mode: protocol.AnalyzerModePlanned, Worktree: request.Worktree, CanonicalRemote: repository.CanonicalRemote,
			Paths: paths, Symbols: symbols,
		})
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return Suggestions{}, err
			}
			result.Analysis.Reason = "Planned analysis did not complete: " + err.Error()
			break
		}
		result.Analysis.Available = true
		result.Diagnostics = report.Diagnostics
		for _, diagnostic := range report.Diagnostics {
			if diagnostic.Code == analyzer.DiagnosticPlannedPathNew && diagnostic.Path != "" {
				result.NewPaths = append(result.NewPaths, diagnostic.Path)
			}
		}
		for _, claim := range report.Claims {
			declared := claim
			declared.ProvenanceKind = "declared"
			declared.Confidence = nil
			merged.add(Candidate{Claim: declared, Source: SourceAnalysis, Reason: analysisReason(claim)})
		}
	}

	if request.ObjectiveID != (uuid.UUID{}) {
		concepts, err := deps.Client.Concepts(ctx, request.ProjectID, httpclient.ConceptQuery{ObjectiveID: request.ObjectiveID, Limit: 200})
		if err != nil {
			return Suggestions{}, err
		}
		result.AsOfEvent = max(result.AsOfEvent, concepts.AsOfEvent)
		for _, entry := range concepts.Data.Concepts {
			if entry.SubjectKind != "invariant" && entry.SubjectKind != "acceptance_criterion" {
				continue
			}
			merged.add(Candidate{Claim: conceptCandidate(entry), Source: SourceObjective, Reason: objectiveReason(entry, request.ObjectiveID)})
		}
	}
	if focus := strings.TrimSpace(request.Focus); focus != "" {
		concepts, err := deps.Client.Concepts(ctx, request.ProjectID, httpclient.ConceptQuery{Query: focus, Limit: maxCatalogMatches})
		if err != nil {
			return Suggestions{}, err
		}
		result.AsOfEvent = max(result.AsOfEvent, concepts.AsOfEvent)
		for _, entry := range concepts.Data.Concepts {
			merged.add(Candidate{Claim: conceptCandidate(entry), Source: SourceCatalog, Reason: catalogReason(entry)})
		}
	}
	result.Candidates, result.OmittedCount = merged.bounded(MaxCandidates)
	return result, nil
}

// analysisRepository returns the analysis-enabled RepositoryConnection that
// matches the worktree remote, or a reason why analysis cannot run.
func analysisRepository(ctx context.Context, deps SuggestDeps, request SuggestRequest) (*protocol.RepositoryConnection, string) {
	discover := deps.Discover
	if discover == nil {
		discover = defaultDiscover
	}
	metadata, err := discover(ctx, request.Worktree)
	if err != nil {
		return nil, "The worktree has no usable origin remote, so planned analysis did not run."
	}
	repositories, err := deps.Client.RepositoryConnections(ctx, request.ProjectID)
	if err != nil {
		return nil, "The Project repository connections could not be read, so planned analysis did not run."
	}
	for i, repository := range repositories.Data.Repositories {
		if repository.CanonicalRemote != metadata.CanonicalRemote {
			continue
		}
		if !repository.AnalysisEnabled {
			return nil, "Local analysis is disabled for the matching RepositoryConnection."
		}
		return &repositories.Data.Repositories[i], ""
	}
	return nil, fmt.Sprintf("No RepositoryConnection in this Project matches %s, so planned analysis did not run.", metadata.CanonicalRemote)
}

// PlannedPaths confines planned repository-relative paths to the worktree.
// A planned path may not exist yet, so containment is checked on its deepest
// existing ancestor after resolving symlinks.
func PlannedPaths(worktree string, requested []string) ([]string, error) {
	if len(requested) == 0 || len(requested) > analyzer.MaxPlannedPaths {
		return nil, fmt.Errorf("paths must contain 1 to %d planned repository-relative paths", analyzer.MaxPlannedPaths)
	}
	root, err := filepath.EvalSymlinks(worktree)
	if err != nil {
		return nil, fmt.Errorf("resolve the worktree: %w", err)
	}
	paths := make([]string, 0, len(requested))
	seen := map[string]bool{}
	for _, raw := range requested {
		if raw == "" || len(raw) > 1024 || filepath.IsAbs(raw) || filepath.VolumeName(raw) != "" || strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, `\`) {
			return nil, fmt.Errorf("planned path %q must be a repository-relative path", raw)
		}
		for _, part := range strings.FieldsFunc(raw, func(r rune) bool { return r == '/' || r == '\\' }) {
			if part == ".." {
				return nil, fmt.Errorf("planned path %q must not contain parent traversal", raw)
			}
		}
		cleaned := filepath.Clean(filepath.FromSlash(raw))
		if cleaned == "." {
			return nil, fmt.Errorf("planned path %q must identify a file", raw)
		}
		existing := filepath.Join(root, cleaned)
		for {
			if _, err := os.Lstat(existing); err == nil {
				break
			}
			parent := filepath.Dir(existing)
			if parent == existing {
				return nil, fmt.Errorf("planned path %q escapes the worktree", raw)
			}
			existing = parent
		}
		resolved, err := filepath.EvalSymlinks(existing)
		if err != nil {
			return nil, fmt.Errorf("resolve planned path %q: %w", raw, err)
		}
		if relative, err := filepath.Rel(root, resolved); err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
			return nil, fmt.Errorf("planned path %q escapes the worktree", raw)
		}
		slashed := filepath.ToSlash(cleaned)
		if !seen[slashed] {
			seen[slashed] = true
			paths = append(paths, slashed)
		}
	}
	return paths, nil
}

func plannedSymbols(worktree string, paths []string, requested map[string][]string) (map[string][]string, error) {
	if len(requested) == 0 {
		return nil, nil
	}
	allowed := make(map[string]bool, len(paths))
	for _, path := range paths {
		allowed[path] = true
	}
	symbols := make(map[string][]string, len(requested))
	for raw, names := range requested {
		normalized, err := PlannedPaths(worktree, []string{raw})
		if err != nil {
			return nil, err
		}
		if !allowed[normalized[0]] {
			return nil, fmt.Errorf("symbols names path %q that is not among the planned paths", raw)
		}
		if len(names) == 0 || len(names) > 50 {
			return nil, fmt.Errorf("symbols for %q must list 1 to 50 export names", raw)
		}
		for _, name := range names {
			if name == "" || len(name) > 200 {
				return nil, fmt.Errorf("symbol names for %q must be 1 to 200 characters", raw)
			}
		}
		symbols[normalized[0]] = names
	}
	return symbols, nil
}

func conceptCandidate(entry protocol.ConceptEntry) protocol.SemanticClaimCandidate {
	return protocol.SemanticClaimCandidate{
		SubjectKind: entry.SubjectKind, NormalizedSubject: entry.NormalizedSubject, DisplayName: entry.DisplayName,
		Relation: "affects", ProvenanceKind: "declared",
	}
}

func analysisReason(claim protocol.SemanticClaimCandidate) string {
	location := "a planned file"
	if len(claim.EvidenceRefs) > 0 && claim.EvidenceRefs[0].Path != "" {
		location = claim.EvidenceRefs[0].Path
	}
	switch claim.Relation {
	case "consumes":
		return bounded(fmt.Sprintf("Planned analysis: %s imports this repository contract.", location))
	default:
		return bounded(fmt.Sprintf("Planned analysis: %s exports this %s and is planned to change.", location, strings.ReplaceAll(claim.SubjectKind, "_", " ")))
	}
}

func objectiveReason(entry protocol.ConceptEntry, objectiveID uuid.UUID) string {
	for _, ref := range entry.ObjectiveRefs {
		if ref.ObjectiveID == objectiveID {
			return bounded(fmt.Sprintf("Objective %s revision %d key point %s. Use relation changes when this work changes it.%s", ref.ObjectiveID, ref.ObjectiveRevision, ref.KeyPointKey, claimants(entry)))
		}
	}
	return bounded("Objective-derived subject." + claimants(entry))
}

func catalogReason(entry protocol.ConceptEntry) string {
	reason := "Catalog match for the task focus."
	if entry.Reason != "" {
		reason += " " + entry.Reason
	}
	return bounded(reason + claimants(entry))
}

func claimants(entry protocol.ConceptEntry) string {
	if len(entry.Claims) == 0 {
		return ""
	}
	parts := make([]string, 0, 3)
	for _, claim := range entry.Claims[:min(3, len(entry.Claims))] {
		parts = append(parts, fmt.Sprintf("Intent %s@%d %s", claim.IntentID, claim.IntentRevision, claim.Relation))
	}
	if entry.ClaimCount > len(parts) {
		parts = append(parts, fmt.Sprintf("%d more", entry.ClaimCount-len(parts)))
	}
	return " Active claims: " + strings.Join(parts, ", ") + "."
}

func bounded(text string) string {
	runes := []rune(text)
	if len(runes) <= maxReasonRunes {
		return text
	}
	return string(runes[:maxReasonRunes-1]) + "…"
}

type merger struct {
	seen       map[string]bool
	candidates []Candidate
}

func newMerger() *merger { return &merger{seen: map[string]bool{}} }

// add keeps the first candidate for each subject and relation, so the
// earlier, more precise source wins.
func (m *merger) add(candidate Candidate) {
	key := candidate.Claim.NormalizedSubject + " " + candidate.Claim.Relation
	if m.seen[key] {
		return
	}
	m.seen[key] = true
	m.candidates = append(m.candidates, candidate)
}

func (m *merger) bounded(limit int) ([]Candidate, int) {
	order := map[string]int{SourceAnalysis: 0, SourceObjective: 1, SourceCatalog: 2}
	sort.SliceStable(m.candidates, func(i, j int) bool { return order[m.candidates[i].Source] < order[m.candidates[j].Source] })
	if len(m.candidates) <= limit {
		return append([]Candidate{}, m.candidates...), 0
	}
	return append([]Candidate{}, m.candidates[:limit]...), len(m.candidates) - limit
}
