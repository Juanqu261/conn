package review

import (
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"uuid"

	"github.com/Juanqu261/conn/internal/protocol"
)

// TestPacket protects requirement 7: the packet is schema-valid JSON, its
// Markdown stays under the bound with oversized inputs and states what it
// truncated, participant prose is sanitized, objective overviews, collision
// explanations, and evidence summaries never appear, and the integrity notes
// flag a changed check configuration, stale and uncommitted runs, reported
// evidence, local observation, and incomplete analysis.
func TestPacket(t *testing.T) {
	const marker = "PACKET_SOURCE_MARKER_MUST_NOT_APPEAR"
	at := protocol.NewTimestamp(time.Date(2026, 11, 22, 9, 0, 0, 0, time.UTC))
	projectID, objectiveID, intentID, participantID, humanID := uuid.NewV7(), uuid.NewV7(), uuid.NewV7(), uuid.NewV7(), uuid.NewV7()
	head, oldCommit := strings.Repeat("d", 40), strings.Repeat("a", 40)
	retry := "contract:typescript:src/payments/retry.ts/shouldRetryCharge"
	intent := protocol.IntentRevision{
		IntentID: intentID, ProjectID: projectID, ParticipantID: participantID, Revision: 3, ObjectiveID: objectiveID, ObjectiveRevision: 1,
		BaseRevisionRef: strings.Repeat("b", 40), BranchRef: "alice-retry",
		SummaryMarkdown: "Change retry eligibility.\n<script>alert(1)</script>\x1b[31m" + strings.Repeat("long summary ", 3000),
		DeclaredClaims: []protocol.SemanticClaimCandidate{
			{SubjectKind: "contract", NormalizedSubject: retry, DisplayName: "shouldRetryCharge", Relation: "changes", ProvenanceKind: "declared"},
			{SubjectKind: "question", NormalizedSubject: "question:checkout:backoff", DisplayName: "Which backoff applies?", Relation: "questions", ProvenanceKind: "declared"},
		},
	}
	for i := range 100 {
		intent.Assumptions = append(intent.Assumptions, fmt.Sprintf("Assumption %03d %s", i, strings.Repeat("x", 900)))
	}
	objective := protocol.Objective{
		ObjectiveID: objectiveID, ProjectID: projectID, Revision: 1, Title: "Reliable <b>checkout</b> retries",
		OverviewMarkdown: marker, AccountableHumanID: humanID, CreatedAt: at,
		KeyPoints: []protocol.KeyPoint{
			{Key: "observed-tests", Kind: protocol.KeyPointAcceptanceCriterion, Statement: "Retry tests pass."},
			{Key: "reported-review", Kind: protocol.KeyPointAcceptanceCriterion, Statement: "A reviewer approved."},
			{Key: "untested", Kind: protocol.KeyPointAcceptanceCriterion, Statement: "Nobody checked this."},
		},
	}
	observation := func(commit string, dirty bool) *protocol.EvidenceObservation {
		return &protocol.EvidenceObservation{Method: protocol.ObservationMethodCheck, Runner: protocol.ObservationRunnerLocal, CheckKey: "retry-tests",
			ConfigSHA256: strings.Repeat("1", 64), CommandSHA256: strings.Repeat("2", 64), Commit: commit, WorktreeDirty: dirty,
			DurationMS: 120, OutputSHA256: strings.Repeat("3", 64), StartedAt: at}
	}
	evidence := []protocol.EvidenceReference{
		{EvidenceID: uuid.NewV7(), ProjectID: projectID, IntentID: &intentID, ObjectiveID: objectiveID, ObjectiveRevision: 1, AcceptanceKey: "observed-tests",
			Kind: protocol.EvidenceKindCheckRun, State: "passing", Locator: "conn-check:retry-tests@aaaaaaaaaaaa", Summary: marker, Observation: observation(oldCommit, true), ObservedAt: at},
		{EvidenceID: uuid.NewV7(), ProjectID: projectID, IntentID: &intentID, ObjectiveID: objectiveID, ObjectiveRevision: 1, AcceptanceKey: "reported-review",
			Kind: "external_link", State: "passing", Locator: "review:1", ObservedAt: at},
	}
	var collisions []protocol.CollisionAssessment
	for i := range 100 {
		var matched []string
		for j := range 50 {
			matched = append(matched, fmt.Sprintf("contract:typescript:src/generated/%03d/%s/export%02d", i, strings.Repeat("p", 400), j))
		}
		collisions = append(collisions, protocol.CollisionAssessment{
			CollisionID: uuid.NewV7(), ProjectID: projectID, LeftIntentID: intentID, LeftIntentRevision: 3, RightIntentID: uuid.NewV7(), RightIntentRevision: 1,
			RuleID: "shared_exported_symbol", Severity: "amber", Confidence: 0.9, State: protocol.CollisionOpen, MatchedSubjects: matched,
			ExplanationMarkdown: marker, CreatedAt: at,
		})
	}
	decisionID := uuid.NewV7()
	collisions[0].State, collisions[0].DecisionRef = protocol.CollisionResolved, &decisionID
	changed := []string{ChecksConfigPath, "src/payments/retry.ts"}
	for i := range 300 {
		changed = append(changed, fmt.Sprintf("src/generated/%03d/%s.ts", i, strings.Repeat("f", 900)))
	}
	records := Records{
		Work: protocol.WorkContext{
			Project: protocol.WorkContextProject{ProjectID: projectID, Name: "Checkout", Role: protocol.RoleMember},
			Reader:  protocol.WorkContextReader{Human: protocol.Human{HumanID: humanID, GitHubUserID: 1, GitHubLogin: "alice"}},
			Focus: &protocol.WorkContextFocus{Objective: objective, OwnCharters: []protocol.OwnCharter{}, SelectedIntent: &intent,
				Analysis: &protocol.WorkContextAnalysis{IntentRevision: 3, Status: protocol.AnalysisIncomplete, ObservedAt: at, DiagnosticCodes: []string{"compiler_error"}}},
			KnownUnknowns: []string{"No Blueprint revision is available."},
		},
		Radar: protocol.RadarView{ActiveIntents: []protocol.IntentRevision{intent}, SemanticClaims: []protocol.SemanticClaim{
			{IntentID: intentID, IntentRevision: 3, SubjectKind: "contract", NormalizedSubject: retry, DisplayName: "shouldRetryCharge", Relation: "changes", ProvenanceKind: "static_analysis"},
			{IntentID: intentID, IntentRevision: 3, SubjectKind: "contract", NormalizedSubject: "contract:typescript:src/payments/retry.ts/ChargeAttempt", DisplayName: "ChargeAttempt", Relation: "changes", ProvenanceKind: "static_analysis"},
		}},
		Collisions: collisions, Evidence: evidence, AsOfEvent: 42,
		Logins:    map[uuid.UUID]string{humanID: "alice"},
		Proposals: map[uuid.UUID]int{collisions[0].CollisionID: 2},
		Decisions: map[uuid.UUID]protocol.ResolutionDecision{decisionID: {DecisionID: decisionID, Kind: "sequencing", DecidedByHumanID: humanID, RationaleMarkdown: "Land <the> producer first.\n" + strings.Repeat("r", 900)}},
	}
	git := GitFacts{Available: true, Branch: "alice-retry", Base: intent.BaseRevisionRef, Head: head, Dirty: true, ChangedFiles: changed,
		Trailers: &TrailerLog{Commits: []string{head, oldCommit}, ByIntent: map[uuid.UUID]IntentTrailers{intentID: {IntentID: intentID, LatestRevision: 3, Commits: []string{head}}}}}

	packet, err := BuildPacket(PacketInput{Records: records, Git: git, Written: map[string]bool{ChecksConfigPath: true}})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := RenderJSON(packet)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := protocol.Decode[protocol.ReviewPacket]([]byte(encoded)); err != nil {
		t.Fatalf("packet JSON is not schema-valid: %v", err)
	}
	markdown := RenderMarkdown(packet)
	if count := utf8.RuneCountInString(markdown); count >= MaxMarkdownChars {
		t.Fatalf("Markdown has %d characters, want fewer than %d", count, MaxMarkdownChars)
	}

	for _, output := range []string{encoded, markdown} {
		for _, forbidden := range []string{marker, "<script>", "<b>", "\x1b"} {
			if strings.Contains(output, forbidden) {
				t.Errorf("packet output contains %q", forbidden)
			}
		}
	}
	if !packet.Git.ChecksConfigChanged || !packet.Git.HeadIsWorktree || packet.Drift.Analysis.Status != protocol.AnalysisIncomplete ||
		packet.Acceptance[0].State != "passing" || !packet.Acceptance[0].Evidence[0].Stale || !packet.Acceptance[0].Evidence[0].Dirty ||
		packet.Acceptance[1].Provenance != protocol.ProvenanceReported || packet.Acceptance[2].State != protocol.EvidenceMissing ||
		len(packet.Drift.ObservedNotDeclared) != 1 || len(packet.Drift.Matched) != 1 ||
		packet.Collisions[0].Decision == nil || packet.Collisions[0].Decision.DecidedByLogin != "alice" || *packet.Collisions[0].ProposalCount != 2 ||
		utf8.RuneCountInString(packet.Collisions[0].Decision.RationaleExcerpt) > maxExcerptRunes || len(packet.OpenQuestions) != 1 {
		t.Fatalf("packet = %+v", packet)
	}
	notes := strings.Join(packet.IntegrityNotes, "\n")
	for _, want := range []string{
		ChecksConfigPath + " changed on this branch",
		"observed-tests: the latest observed run is at commit aaaaaaaaaaaa, not the reviewed head dddddddddddd",
		"observed-tests: the latest observed run ran on uncommitted changes",
		"reported-review relies on reported evidence",
		"untested has no evidence",
		"not independent CI proof",
		"was incomplete",
		"1 of 2 commit(s) on the branch carry no Conn-Intent trailer",
		"uncommitted changes",
	} {
		if !strings.Contains(notes, want) {
			t.Errorf("integrity notes omit %q:\n%s", want, notes)
		}
	}
	for _, want := range []string{"## What this change set out to do", "## Acceptance evidence", "## Scope drift", "## Collisions and decisions", "## Integrity notes", "## Known unknowns", "## Truncated", "Markdown omitted the last", "&lt;script&gt;", PacketFooter} {
		if !strings.Contains(markdown, want) {
			t.Errorf("Markdown omits %q", want)
		}
	}
}
