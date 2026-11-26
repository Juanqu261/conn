// Package review builds Conn's review proof: scope drift between declared and
// observed claims, the commit-trailer parser, the shared loader for handoffs
// and review packets, and the bounded packet with its Markdown rendering. The
// CLI, the MCP adapter, and the later CI check share it. It reads Conn
// records through the authenticated client and local Git facts through
// read-only Git, and it persists nothing.
package review

import (
	"sort"
	"strings"

	"uuid"

	"github.com/Juanqu261/conn/internal/protocol"
)

// MaxDriftItems bounds each drift list; the omitted counts report the rest.
const MaxDriftItems = 100

// DriftInput is everything scope drift compares. It is pure data, so the
// CLI, MCP, and CI compute the same result from the same records.
type DriftInput struct {
	IntentID        uuid.UUID
	CurrentRevision int
	// Declared are the declared claims of the current Intent revision.
	Declared []protocol.SemanticClaimCandidate
	// Observed may contain other claims; only static-analysis claims of the
	// current revision are compared.
	Observed []protocol.SemanticClaim
	// Analysis is the latest analysis at or before the current revision,
	// or nil when the Intent was never analyzed.
	Analysis *protocol.WorkContextAnalysis
	// ChangedFiles are repository-relative paths changed between the
	// Intent base and the reviewed head.
	ChangedFiles []string
	// Written are repository-relative paths a Conn-connected agent session
	// wrote in this worktree.
	Written map[string]bool
	// Acceptance maps each acceptance key of the Objective revision to the
	// state of this Intent's latest evidence for it.
	Acceptance map[string]string
}

// notObservable lists claim kinds that static analysis cannot observe.
var notObservable = map[string]bool{
	"behavior": true, "invariant": true, "data_resource": true, "infrastructure_resource": true,
	"dependency": true, "assumption": true, "question": true, "acceptance_criterion": true,
}

// Drift compares declared and observed code_symbol and contract subjects
// with changes or removes relations. A consumes difference is not drift:
// using an existing contract does not change it. Without an analysis of the
// current revision it reports not_analyzed and no subject comparison, never
// the absence of drift. Drift is advisory and changes no record.
func Drift(in DriftInput) protocol.ScopeDrift {
	drift := protocol.ScopeDrift{
		Analysis:                 protocol.DriftAnalysis{Status: protocol.AnalysisNotAnalyzed},
		Matched:                  []protocol.DriftSubject{},
		DeclaredNotObserved:      []protocol.DriftSubject{},
		ObservedNotDeclared:      []protocol.DriftSubject{},
		ChangedFilesWithoutClaim: []protocol.DriftFile{},
		NotObservable:            []protocol.DriftNotObservable{},
	}
	if in.Analysis != nil {
		revision, observedAt := in.Analysis.IntentRevision, in.Analysis.ObservedAt
		drift.Analysis.IntentRevision, drift.Analysis.ObservedAt = &revision, &observedAt
		if revision == in.CurrentRevision {
			drift.Analysis.Status = in.Analysis.Status
			drift.Analysis.DiagnosticCodes = append([]string(nil), in.Analysis.DiagnosticCodes...)
		}
	}

	declared := map[string]protocol.DriftSubject{}
	for _, claim := range in.Declared {
		if comparable(claim.SubjectKind, claim.Relation) {
			if _, ok := declared[claim.NormalizedSubject]; !ok {
				declared[claim.NormalizedSubject] = subject(claim.SubjectKind, claim.NormalizedSubject, claim.DisplayName, claim.Relation)
			}
		}
	}
	if drift.Analysis.Status != protocol.AnalysisNotAnalyzed {
		observed := map[string]protocol.DriftSubject{}
		for _, claim := range in.Observed {
			if claim.IntentID != in.IntentID || claim.IntentRevision != in.CurrentRevision || claim.ProvenanceKind != "static_analysis" {
				continue
			}
			if comparable(claim.SubjectKind, claim.Relation) {
				if _, ok := observed[claim.NormalizedSubject]; !ok {
					observed[claim.NormalizedSubject] = subject(claim.SubjectKind, claim.NormalizedSubject, claim.DisplayName, claim.Relation)
				}
			}
		}
		var matched, declaredOnly, observedOnly []protocol.DriftSubject
		for key, item := range observed {
			if _, ok := declared[key]; ok {
				matched = append(matched, item)
			} else {
				observedOnly = append(observedOnly, item)
			}
		}
		for key, item := range declared {
			if _, ok := observed[key]; !ok {
				declaredOnly = append(declaredOnly, item)
			}
		}
		drift.Matched, drift.Omitted.Matched = boundSubjects(matched)
		drift.DeclaredNotObserved, drift.Omitted.DeclaredNotObserved = boundSubjects(declaredOnly)
		drift.ObservedNotDeclared, drift.Omitted.ObservedNotDeclared = boundSubjects(observedOnly)
	}

	covered := map[string]bool{}
	for _, claim := range in.Declared {
		for _, ref := range claim.EvidenceRefs {
			if ref.Path != "" {
				covered[cleanPath(ref.Path)] = true
			}
		}
		if path, ok := subjectPath(claim.NormalizedSubject); ok {
			covered[path] = true
		}
	}
	files := append([]string(nil), in.ChangedFiles...)
	sort.Strings(files)
	seen := map[string]bool{}
	for _, file := range files {
		file = cleanPath(file)
		if file == "" || seen[file] || covered[file] {
			continue
		}
		seen[file] = true
		if len(drift.ChangedFilesWithoutClaim) == MaxDriftItems {
			drift.Omitted.ChangedFilesWithoutClaim++
			continue
		}
		drift.ChangedFilesWithoutClaim = append(drift.ChangedFilesWithoutClaim, protocol.DriftFile{Path: file, WrittenByAgent: in.Written[file]})
	}

	listed := map[string]bool{}
	for _, claim := range in.Declared {
		if !notObservable[claim.SubjectKind] || listed[claim.NormalizedSubject+"|"+claim.Relation] {
			continue
		}
		listed[claim.NormalizedSubject+"|"+claim.Relation] = true
		if len(drift.NotObservable) == MaxDriftItems {
			drift.Omitted.NotObservable++
			continue
		}
		item := protocol.DriftNotObservable{
			SubjectKind: claim.SubjectKind, NormalizedSubject: claim.NormalizedSubject,
			DisplayName: SanitizeTitle(claim.DisplayName), Relation: claim.Relation,
		}
		if key, ok := acceptanceKey(claim.NormalizedSubject); ok {
			if state, known := in.Acceptance[key]; known {
				item.AcceptanceKey, item.EvidenceState = key, state
			}
		}
		drift.NotObservable = append(drift.NotObservable, item)
	}
	return drift
}

func comparable(kind, relation string) bool {
	return (kind == "code_symbol" || kind == "contract") && (relation == "changes" || relation == "removes")
}

func subject(kind, normalized, display, relation string) protocol.DriftSubject {
	return protocol.DriftSubject{SubjectKind: kind, NormalizedSubject: normalized, DisplayName: SanitizeTitle(display), Relation: relation}
}

func boundSubjects(items []protocol.DriftSubject) ([]protocol.DriftSubject, int) {
	sort.Slice(items, func(i, j int) bool { return items[i].NormalizedSubject < items[j].NormalizedSubject })
	if items == nil {
		items = []protocol.DriftSubject{}
	}
	if len(items) > MaxDriftItems {
		return items[:MaxDriftItems], len(items) - MaxDriftItems
	}
	return items, 0
}

// subjectPath extracts the repository path from a TypeScript symbol or
// contract subject such as contract:typescript:src/payments/retry.ts/name.
func subjectPath(subject string) (string, bool) {
	for _, prefix := range []string{"contract:typescript:", "symbol:typescript:"} {
		if rest, ok := strings.CutPrefix(subject, prefix); ok {
			if index := strings.LastIndex(rest, "/"); index > 0 {
				return rest[:index], true
			}
		}
	}
	return "", false
}

// acceptanceKey extracts the key from an acceptance:<objective-ref>:<key>
// subject.
func acceptanceKey(subject string) (string, bool) {
	if !strings.HasPrefix(subject, "acceptance:") {
		return "", false
	}
	index := strings.LastIndex(subject, ":")
	if index < len("acceptance:") || index == len(subject)-1 {
		return "", false
	}
	return subject[index+1:], true
}

func cleanPath(path string) string {
	return strings.TrimPrefix(strings.ReplaceAll(strings.TrimSpace(path), "\\", "/"), "./")
}
