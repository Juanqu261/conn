package review

import (
	"fmt"
	"strings"

	"uuid"

	"github.com/Juanqu261/conn/internal/protocol"
)

// Packet bounds that the schema also enforces.
const (
	maxPacketStatements  = 100
	maxPacketAcceptance  = 50
	maxPacketCollisions  = 100
	maxEvidencePerKey    = 10
	maxPacketAssumptions = 100
)

// LocalObservationNote states the trust level of locally observed runs.
const LocalObservationNote = "Observed runs ran on the Intent owner's machine at the recorded commit; they are stronger than reported states but are not independent CI proof."

// PacketInput is what BuildPacket combines: authoritative records, local Git
// facts, and the paths an agent session wrote in this worktree.
type PacketInput struct {
	Records Records
	Git     GitFacts
	Written map[string]bool
	// Observed, when non-nil, replaces the stored observed claims for drift,
	// as the CI check does with the claims it observed itself.
	Observed []protocol.SemanticClaim
	// OwnerLogin, when set, names the Intent owner instead of the reader.
	OwnerLogin string
}

// BuildPacket assembles a schema-valid review packet. It persists nothing.
func BuildPacket(in PacketInput) (protocol.ReviewPacket, error) {
	records, git := in.Records, in.Git
	work := records.Work
	focus := work.Focus
	intent := records.Intent()
	objective := focus.Objective
	packet := protocol.ReviewPacket{
		PacketVersion:    protocol.ReviewPacketVersion,
		GeneratedAtEvent: records.AsOfEvent,
		Project:          protocol.ReviewProject{ID: work.Project.ProjectID, Name: SanitizeTitle(work.Project.Name)},
		Intent: protocol.ReviewIntent{
			ID: intent.IntentID, Revision: intent.Revision, ParticipantID: intent.ParticipantID,
			OwnerLogin: ownerLogin(in, work), Active: intentActive(records.Radar, intent),
			Summary: SanitizeProse(intent.SummaryMarkdown, maxSummaryRunes),
		},
		Objective:      protocol.ReviewObjective{ID: objective.ObjectiveID, Revision: objective.Revision, Title: SanitizeTitle(objective.Title), SourceKind: objective.SourceKind()},
		Acceptance:     []protocol.ReviewAcceptance{},
		Collisions:     []protocol.ReviewCollision{},
		IntegrityNotes: []string{},
		OpenQuestions:  []string{},
		KnownUnknowns:  []string{},
		Truncated:      []string{},
	}
	if packet.Intent.Summary == "" {
		packet.Intent.Summary = "No summary text remained after sanitization."
	}
	var truncated []string
	var omitted int
	packet.Intent.Assumptions, omitted = boundedStatements(intent.Assumptions, maxPacketAssumptions)
	if omitted > 0 {
		truncated = append(truncated, fmt.Sprintf("%d assumption(s) were omitted.", omitted))
	}
	if intent.BlueprintID != nil && intent.BlueprintRevision != nil {
		packet.Blueprint = &protocol.ReviewRevisionRef{ID: *intent.BlueprintID, Revision: *intent.BlueprintRevision}
	}
	if intent.CharterID != nil {
		packet.Charter = &protocol.ReviewCharter{ID: *intent.CharterID, Accepted: charterAccepted(focus, *intent.CharterID)}
	}

	head := git.Head
	if head == "" {
		head = intent.HeadRevisionRef
	}
	base := git.Base
	if base == "" && revisionRef.MatchString(intent.BaseRevisionRef) {
		base = intent.BaseRevisionRef
	}
	packet.Git = protocol.ReviewGit{
		Available: git.Available, Base: base, Head: head, HeadIsWorktree: git.Dirty, Dirty: git.Dirty,
		ChangedFileCount: len(git.ChangedFiles), ChecksConfigChanged: containsPath(git.ChangedFiles, ChecksConfigPath),
	}
	if branch := git.Branch; branch != "" {
		packet.Git.Branch = branch
	} else if intent.BranchRef != "" {
		packet.Git.Branch = SanitizeLine(intent.BranchRef, 256)
	}

	var notes, unknowns, questions []string
	if packet.Git.ChecksConfigChanged {
		notes = append(notes, ChecksConfigPath+" changed on this branch; review the check definitions before trusting observed runs.")
	}
	if git.Dirty {
		notes = append(notes, "The working tree has uncommitted changes; the reviewed head includes changes that no commit records yet.")
	}

	acceptanceStates := map[string]string{}
	observedAny := false
	acceptanceCount := 0
	for _, point := range objective.KeyPoints {
		if point.Kind != protocol.KeyPointAcceptanceCriterion {
			if point.Kind == protocol.KeyPointQuestion {
				questions = append(questions, point.Statement)
			}
			continue
		}
		acceptanceCount++
		item := protocol.ReviewAcceptance{Key: point.Key, Statement: sanitizeStatement(point.Statement), State: protocol.EvidenceMissing, Evidence: []protocol.ReviewEvidence{}}
		if item.Statement == "" {
			item.Statement = "Acceptance criterion " + point.Key + "."
		}
		for _, evidence := range records.Evidence {
			if evidence.AcceptanceKey != point.Key || evidence.ObjectiveID != objective.ObjectiveID || evidence.ObjectiveRevision != objective.Revision {
				continue
			}
			if len(item.Evidence) == maxEvidencePerKey {
				truncated = append(truncated, fmt.Sprintf("Acceptance key %s lists only its %d most recent evidence references.", point.Key, maxEvidencePerKey))
				break
			}
			item.Evidence = append(item.Evidence, reviewEvidence(evidence, head))
		}
		if len(item.Evidence) == 0 {
			notes = append(notes, fmt.Sprintf("Acceptance key %s has no evidence from this Intent.", point.Key))
		} else {
			latest := item.Evidence[0]
			item.State, item.Provenance = latest.State, latest.Provenance
			switch {
			case latest.Provenance == protocol.ProvenanceReported:
				notes = append(notes, fmt.Sprintf("Acceptance key %s relies on reported evidence that Conn did not observe.", point.Key))
			case latest.Stale:
				notes = append(notes, fmt.Sprintf("Acceptance key %s: the latest observed run is at commit %s, not the reviewed head %s.", point.Key, shortCommit(latest.Commit), shortCommit(head)))
			}
			if latest.Dirty {
				notes = append(notes, fmt.Sprintf("Acceptance key %s: the latest observed run ran on uncommitted changes.", point.Key))
			}
		}
		for _, evidence := range item.Evidence {
			observedAny = observedAny || evidence.Provenance == protocol.ProvenanceObservedRun
		}
		acceptanceStates[point.Key] = item.State
		if len(packet.Acceptance) < maxPacketAcceptance {
			packet.Acceptance = append(packet.Acceptance, item)
		}
	}
	if acceptanceCount > maxPacketAcceptance {
		truncated = append(truncated, fmt.Sprintf("Only %d of %d acceptance criteria are listed.", maxPacketAcceptance, acceptanceCount))
	}
	if acceptanceCount == 0 {
		unknowns = append(unknowns, "The Objective revision has no keyed acceptance criteria, so no evidence maps to it.")
	}
	if observedAny {
		notes = append(notes, LocalObservationNote)
	}
	unkeyed := 0
	for _, evidence := range records.Evidence {
		if evidence.AcceptanceKey == "" {
			unkeyed++
		}
	}
	if unkeyed > 0 {
		unknowns = append(unknowns, fmt.Sprintf("%d evidence reference(s) on this Intent name no acceptance key and are not mapped.", unkeyed))
	}

	observed := records.Radar.SemanticClaims
	if in.Observed != nil {
		observed = in.Observed
	}
	packet.Drift = Drift(DriftInput{
		IntentID: intent.IntentID, CurrentRevision: intent.Revision, Declared: intent.DeclaredClaims,
		Observed: observed, Analysis: focus.Analysis, ChangedFiles: git.ChangedFiles,
		Written: in.Written, Acceptance: acceptanceStates,
	})
	switch analysis := packet.Drift.Analysis; {
	case analysis.Status == protocol.AnalysisIncomplete:
		notes = append(notes, fmt.Sprintf("Observed analysis of revision %d was incomplete; drift may miss changes.", intent.Revision))
	case analysis.Status == protocol.AnalysisNotAnalyzed && analysis.IntentRevision != nil:
		notes = append(notes, fmt.Sprintf("Observed analysis covers revision %d, not the current revision %d, so drift is not reported.", *analysis.IntentRevision, intent.Revision))
	case analysis.Status == protocol.AnalysisNotAnalyzed:
		notes = append(notes, "The Intent was not analyzed, so drift is not reported. This is not evidence of no drift.")
	}
	if !git.Available || git.Base == "" {
		unknowns = append(unknowns, "Changed files are unknown, so files without a declared claim cannot be listed.")
	}
	if git.Trailers != nil && len(git.Trailers.Commits) > 0 {
		named := len(git.Trailers.ByIntent[intent.IntentID].Commits)
		if missing := len(git.Trailers.Commits) - named; missing > 0 {
			notes = append(notes, fmt.Sprintf("%d of %d commit(s) on the branch carry no Conn-Intent trailer for this Intent.", missing, len(git.Trailers.Commits)))
		}
	}

	for i, collision := range records.Collisions {
		if i == maxPacketCollisions {
			truncated = append(truncated, fmt.Sprintf("Only %d of %d collisions are listed.", maxPacketCollisions, len(records.Collisions)))
			break
		}
		item := reviewCollision(collision, intent.IntentID, records)
		if _, ok := records.Proposals[collision.CollisionID]; !ok && i >= MaxCollisionDetails {
			unknowns = append(unknowns, fmt.Sprintf("Decision details and proposal counts are loaded for the first %d collisions only.", MaxCollisionDetails))
		}
		if collision.State == protocol.CollisionOpen || collision.State == protocol.CollisionUnderReview {
			notes = append(notes, fmt.Sprintf("Collision %s (%s) is %s and has no settling human decision.", shortID(collision.CollisionID), collision.RuleID, strings.ReplaceAll(collision.State, "_", " ")))
		}
		packet.Collisions = append(packet.Collisions, item)
	}
	for _, claim := range intent.DeclaredClaims {
		if claim.SubjectKind == "question" || claim.Relation == "questions" {
			questions = append(questions, claim.DisplayName)
		}
	}
	unknowns = append(unknowns, git.Notes...)
	for _, unknown := range work.KnownUnknowns {
		// These describe the reading adapter's own Participant, not the
		// reviewed Intent, which may belong to another of the Human's
		// Participants.
		if !strings.HasPrefix(unknown, "No owned active Intent") && !strings.HasPrefix(unknown, "No Participant identity exists") &&
			!strings.HasPrefix(unknown, "Several owned active Intents") {
			unknowns = append(unknowns, unknown)
		}
	}

	packet.IntegrityNotes, omitted = boundedStatements(notes, maxPacketStatements)
	if omitted > 0 {
		truncated = append(truncated, fmt.Sprintf("%d integrity note(s) were omitted.", omitted))
	}
	packet.OpenQuestions, omitted = boundedStatements(questions, maxPacketStatements)
	if omitted > 0 {
		truncated = append(truncated, fmt.Sprintf("%d open question(s) were omitted.", omitted))
	}
	packet.KnownUnknowns, omitted = boundedStatements(unknowns, maxPacketStatements)
	if omitted > 0 {
		truncated = append(truncated, fmt.Sprintf("%d known unknown(s) were omitted.", omitted))
	}
	for _, omission := range []struct {
		name  string
		count int
	}{
		{"matched subjects", packet.Drift.Omitted.Matched},
		{"declared but unobserved subjects", packet.Drift.Omitted.DeclaredNotObserved},
		{"undeclared observed subjects", packet.Drift.Omitted.ObservedNotDeclared},
		{"changed files without a claim", packet.Drift.Omitted.ChangedFilesWithoutClaim},
		{"claims not observable by analysis", packet.Drift.Omitted.NotObservable},
	} {
		if omission.count > 0 {
			truncated = append(truncated, fmt.Sprintf("%d %s were omitted from drift.", omission.count, omission.name))
		}
	}
	packet.Truncated, _ = boundedStatements(truncated, maxPacketStatements)
	if _, err := protocol.Encode(packet); err != nil {
		return protocol.ReviewPacket{}, err
	}
	return packet, nil
}

func reviewEvidence(evidence protocol.EvidenceReference, head string) protocol.ReviewEvidence {
	item := protocol.ReviewEvidence{
		ID: evidence.EvidenceID, Kind: evidence.Kind, State: evidence.State, Locator: evidence.Locator,
		Provenance: evidence.Provenance(), ObservedAt: evidence.ObservedAt,
	}
	if observation := evidence.Observation; observation != nil {
		exitCode, duration := observation.ExitCode, observation.DurationMS
		item.Commit, item.Dirty, item.ExitCode, item.DurationMS = observation.Commit, observation.WorktreeDirty, &exitCode, &duration
		item.Stale = EvidenceStale(evidence, head)
	}
	return item
}

// EvidenceStale reports observed evidence that ran at a commit other than
// the reviewed head. Reported evidence and an unknown head are never stale.
// The Intent board uses this definition too, so the board and the review
// packet cannot disagree.
func EvidenceStale(evidence protocol.EvidenceReference, head string) bool {
	return evidence.Observation != nil && head != "" && !protocol.SameCommit(head, evidence.Observation.Commit)
}

func reviewCollision(collision protocol.CollisionAssessment, intentID uuid.UUID, records Records) protocol.ReviewCollision {
	counterpart := protocol.ReviewIntentRef{IntentID: collision.RightIntentID, Revision: collision.RightIntentRevision}
	if collision.RightIntentID == intentID {
		counterpart = protocol.ReviewIntentRef{IntentID: collision.LeftIntentID, Revision: collision.LeftIntentRevision}
	}
	item := protocol.ReviewCollision{
		ID: collision.CollisionID, RuleID: collision.RuleID, Severity: collision.Severity, State: collision.State,
		MatchedSubjects: append([]string{}, collision.MatchedSubjects...), Counterpart: counterpart,
		LatestCombinationOutcome: collision.LatestCombinationOutcome,
	}
	if count, ok := records.Proposals[collision.CollisionID]; ok {
		item.ProposalCount = &count
	}
	if collision.DecisionRef != nil {
		if decision, ok := records.Decisions[*collision.DecisionRef]; ok {
			excerpt := SanitizeLine(decision.RationaleMarkdown, maxExcerptRunes)
			if excerpt == "" {
				excerpt = "No rationale text remained after sanitization."
			}
			item.Decision = &protocol.ReviewDecision{
				Kind: decision.Kind, Binding: decision.Binding, DecidedByLogin: records.Logins[decision.DecidedByHumanID], RationaleExcerpt: excerpt,
			}
		}
	}
	return item
}

func ownerLogin(in PacketInput, work protocol.WorkContext) string {
	if in.OwnerLogin != "" {
		return in.OwnerLogin
	}
	return work.Reader.Human.GitHubLogin
}

func intentActive(radar protocol.RadarView, intent protocol.IntentRevision) bool {
	for _, active := range radar.ActiveIntents {
		if active.IntentID == intent.IntentID && active.Revision == intent.Revision {
			return true
		}
	}
	return false
}

func charterAccepted(focus *protocol.WorkContextFocus, charterID uuid.UUID) bool {
	if focus.Blueprint == nil || focus.Blueprint.Status != protocol.BlueprintActive {
		return false
	}
	for _, own := range focus.OwnCharters {
		if own.Charter.CellID == charterID && own.Decision == protocol.CellAccepted {
			return true
		}
	}
	return false
}

func containsPath(paths []string, target string) bool {
	for _, path := range paths {
		if cleanPath(path) == target {
			return true
		}
	}
	return false
}

func shortCommit(commit string) string {
	if len(commit) > 12 {
		return commit[:12]
	}
	if commit == "" {
		return "unknown"
	}
	return commit
}

func shortID(id uuid.UUID) string { return id.String()[:8] }
