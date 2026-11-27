package application

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"uuid"

	"github.com/Juanqu261/conn/internal/protocol"
)

// maxAssessorClaims and maxAssessorPairs bound a single synchronous write.
// The limits make assessment time predictable without introducing a worker or
// background hosted service. They are deliberately much larger than the 100
// claims allowed on one Intent input.
const (
	maxAssessorClaims = 2_000
	maxAssessorPairs  = 10_000
)

type collisionCandidate struct {
	leftIntentID  uuid.UUID
	leftRevision  int
	rightIntentID uuid.UUID
	rightRevision int
	ruleID        string
	subjects      map[string]struct{}
	claims        map[uuid.UUID]struct{}
	provenance    map[string]struct{}
	// inherited is the decided assessment whose state this candidate would
	// carry forward, or nil.
	inherited *protocol.CollisionAssessment
}

// persistDeclaredClaims turns the transport candidates in an IntentRevision
// into immutable ledger records. Candidate normalized_subject values already
// passed the canonical semantic-subject contract; Conn supplies all identity,
// attribution, and time fields.
func persistDeclaredClaims(ctx context.Context, tx Tx, revision protocol.IntentRevision) ([]protocol.SemanticClaim, error) {
	claims := make([]protocol.SemanticClaim, 0, len(revision.DeclaredClaims))
	for _, candidate := range revision.DeclaredClaims {
		claim := protocol.SemanticClaim{
			ClaimID:               uuid.NewV7(),
			ProjectID:             revision.ProjectID,
			IntentID:              revision.IntentID,
			IntentRevision:        revision.Revision,
			SubjectKind:           candidate.SubjectKind,
			NormalizedSubject:     candidate.NormalizedSubject,
			DisplayName:           candidate.DisplayName,
			Relation:              candidate.Relation,
			ProvenanceKind:        "declared",
			Confidence:            candidate.Confidence,
			EvidenceRefs:          candidate.EvidenceRefs,
			ProducerParticipantID: &revision.ParticipantID,
			ObservedAt:            revision.CreatedAt,
		}
		if err := validateRecord(claim); err != nil {
			return nil, err
		}
		claims = append(claims, claim)
	}
	if len(claims) == 0 {
		return claims, nil
	}
	return claims, tx.InsertSemanticClaims(ctx, claims)
}

func (p *Projects) appendClaimEvents(ctx context.Context, tx Tx, actor uuid.UUID, claims []protocol.SemanticClaim, at protocol.Timestamp) error {
	for _, claim := range claims {
		if _, err := tx.AppendEvent(ctx, protocol.EventEnvelope{
			ProjectID: claim.ProjectID, EventID: uuid.NewV7(), EntityID: claim.ClaimID,
			ActorHumanID: actor, OccurredAt: at,
			Payload: protocol.ClaimsObserved{IntentID: claim.IntentID, IntentRevision: claim.IntentRevision},
		}); err != nil {
			return err
		}
	}
	return nil
}

func (p *Projects) staleIntentCollisions(ctx context.Context, tx Tx, actor, projectID, intentID uuid.UUID, revision int, at protocol.Timestamp) error {
	collisionIDs, err := tx.StaleOpenCollisionsForIntent(ctx, projectID, intentID, revision, at)
	if err != nil {
		return err
	}
	for _, collisionID := range collisionIDs {
		if _, err := tx.AppendEvent(ctx, protocol.EventEnvelope{
			ProjectID: projectID, EventID: uuid.NewV7(), EntityID: collisionID,
			ActorHumanID: actor, OccurredAt: at, Payload: protocol.CollisionUpdated{},
		}); err != nil {
			return err
		}
	}
	return nil
}

// assessActiveIntentCollisions compares only active current revisions from one
// Project. It uses normalized semantic subjects, never source paths or files;
// evidence paths therefore remain supporting context and cannot cause a rule.
func (p *Projects) assessActiveIntentCollisions(ctx context.Context, tx Tx, actor uuid.UUID, changed protocol.IntentRevision, at protocol.Timestamp) error {
	claims, err := tx.ActiveSemanticClaims(ctx, changed.ProjectID)
	if err != nil {
		return err
	}
	if len(claims) > maxAssessorClaims {
		return &InvalidError{Message: fmt.Sprintf("The active claim set exceeds the bounded assessor limit of %d claims.", maxAssessorClaims)}
	}
	var own, others []protocol.SemanticClaim
	for _, claim := range claims {
		if claim.IntentID == changed.IntentID {
			own = append(own, claim)
		} else {
			others = append(others, claim)
		}
	}
	candidates, err := assessCandidates(own, others, txDecisionIndex(ctx, tx, changed.ProjectID))
	if err != nil {
		return err
	}
	for _, candidate := range candidates {
		assessment := candidate.assessment(changed.ProjectID, at)
		inherited := candidate.inherit(&assessment)
		inserted, err := tx.InsertCollisionAssessment(ctx, assessment)
		if err != nil {
			return err
		}
		if !inserted {
			continue
		}
		// A carried-forward decision is a state change of the decided
		// collision, not a new warning.
		payload := protocol.EventPayload(protocol.CollisionOpened{})
		if inherited {
			payload = protocol.CollisionUpdated{}
		}
		if _, err := tx.AppendEvent(ctx, protocol.EventEnvelope{
			ProjectID: changed.ProjectID, EventID: uuid.NewV7(), EntityID: assessment.CollisionID,
			ActorHumanID: actor, OccurredAt: at, Payload: payload,
		}); err != nil {
			return err
		}
	}
	return nil
}

// DecisionIndex returns the most recent resolved or dismissed assessment for
// one ordered Intent pair and rule, across revisions, or nil.
type DecisionIndex func(leftIntentID, rightIntentID uuid.UUID, ruleID string) (*protocol.CollisionAssessment, error)

func txDecisionIndex(ctx context.Context, tx Tx, projectID uuid.UUID) DecisionIndex {
	return func(leftIntentID, rightIntentID uuid.UUID, ruleID string) (*protocol.CollisionAssessment, error) {
		prior, err := tx.LatestDecidedCollision(ctx, projectID, leftIntentID, rightIntentID, ruleID)
		if errors.Is(err, ErrNotFound) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		return &prior, nil
	}
}

// assessCandidates is the pure assessor core that persistence and previews
// share. It compares the claims of one changed Intent revision with the
// active claims of other Intents by normalized subject, applies the rules,
// and resolves which human decision each candidate would inherit. Results
// are sorted by Intent pair, revisions, and rule.
func assessCandidates(changed, active []protocol.SemanticClaim, prior DecisionIndex) ([]*collisionCandidate, error) {
	bySubject := make(map[string][]protocol.SemanticClaim)
	for _, claim := range active {
		bySubject[claim.NormalizedSubject] = append(bySubject[claim.NormalizedSubject], claim)
	}
	ordered := append([]protocol.SemanticClaim(nil), changed...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].NormalizedSubject != ordered[j].NormalizedSubject {
			return ordered[i].NormalizedSubject < ordered[j].NormalizedSubject
		}
		return ordered[i].ClaimID.String() < ordered[j].ClaimID.String()
	})
	candidates := map[string]*collisionCandidate{}
	pairCount := 0
	for _, mine := range ordered {
		for _, theirs := range bySubject[mine.NormalizedSubject] {
			if theirs.IntentID == mine.IntentID {
				continue
			}
			pairCount++
			if pairCount > maxAssessorPairs {
				return nil, &InvalidError{Message: fmt.Sprintf("The active claim comparison exceeds the bounded assessor limit of %d matching pairs.", maxAssessorPairs)}
			}
			for _, ruleID := range matchingRules(mine, theirs) {
				addCollisionCandidate(candidates, ruleID, mine.NormalizedSubject, mine, theirs)
			}
		}
	}
	keys := make([]string, 0, len(candidates))
	for key := range candidates {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]*collisionCandidate, 0, len(keys))
	for _, key := range keys {
		candidate := candidates[key]
		decided, err := prior(candidate.leftIntentID, candidate.rightIntentID, candidate.ruleID)
		if err != nil {
			return nil, err
		}
		// A decided subject list at its bound may have been truncated and is
		// never treated as complete.
		if decided != nil && decided.DecisionRef != nil && len(decided.MatchedSubjects) < protocol.MaxMatchedSubjects && subjectsCovered(candidate.subjects, decided.MatchedSubjects) {
			candidate.inherited = decided
		}
		result = append(result, candidate)
	}
	return result, nil
}

// inherit carries a human decision forward to a new assessment for the same
// Intent pair and rule. The new assessment inherits the decided state only
// when every subject it matched was covered by the decision, so a new
// matching subject always reopens the warning.
func (c *collisionCandidate) inherit(assessment *protocol.CollisionAssessment) bool {
	if c.inherited == nil {
		return false
	}
	prior := c.inherited
	assessment.State = prior.State
	decision := *prior.DecisionRef
	assessment.DecisionRef = &decision
	assessment.ExplanationMarkdown += fmt.Sprintf(" Human decision %s on collision %s covered every matched subject, so this assessment inherits the %s state.", decision, prior.CollisionID, prior.State)
	return true
}

func subjectsCovered(subjects map[string]struct{}, decided []string) bool {
	covered := make(map[string]bool, len(decided))
	for _, subject := range decided {
		covered[subject] = true
	}
	for subject := range subjects {
		if !covered[subject] {
			return false
		}
	}
	return true
}
func matchingRules(left, right protocol.SemanticClaim) []string {
	// A model inference is never enough to open an assessment. Task 7 only
	// stores declared claims, but retaining this guard keeps the assessor safe
	// when a future adapter adds observed claims.
	if left.ProvenanceKind == "model_inference" && right.ProvenanceKind == "model_inference" {
		return nil
	}
	var rules []string
	if left.SubjectKind == "invariant" && right.SubjectKind == "invariant" &&
		left.ProvenanceKind == "declared" && right.ProvenanceKind == "declared" &&
		affectsOrChanges(left.Relation) && affectsOrChanges(right.Relation) {
		rules = append(rules, "shared_declared_invariant")
	}
	if left.SubjectKind == "code_symbol" && right.SubjectKind == "code_symbol" &&
		affectsOrChanges(left.Relation) && affectsOrChanges(right.Relation) {
		rules = append(rules, "shared_exported_symbol")
	}
	if left.SubjectKind == "contract" && right.SubjectKind == "contract" &&
		((changesOrRemoves(left.Relation) && consumesOrDepends(right.Relation)) ||
			(changesOrRemoves(right.Relation) && consumesOrDepends(left.Relation))) {
		rules = append(rules, "contract_producer_consumer")
	}
	if left.SubjectKind == "data_resource" && right.SubjectKind == "data_resource" &&
		changesOrRemoves(left.Relation) && changesOrRemoves(right.Relation) {
		rules = append(rules, "schema_resource_overlap")
	}
	return rules
}

func affectsOrChanges(relation string) bool { return relation == "affects" || relation == "changes" }
func changesOrRemoves(relation string) bool { return relation == "changes" || relation == "removes" }
func consumesOrDepends(relation string) bool {
	return relation == "consumes" || relation == "depends_on"
}

func addCollisionCandidate(candidates map[string]*collisionCandidate, ruleID, subject string, one, two protocol.SemanticClaim) {
	left, right := one, two
	if right.IntentID.String() < left.IntentID.String() {
		left, right = right, left
	}
	key := strings.Join([]string{left.IntentID.String(), fmt.Sprint(left.IntentRevision), right.IntentID.String(), fmt.Sprint(right.IntentRevision), ruleID}, "|")
	candidate := candidates[key]
	if candidate == nil {
		candidate = &collisionCandidate{
			leftIntentID: left.IntentID, leftRevision: left.IntentRevision,
			rightIntentID: right.IntentID, rightRevision: right.IntentRevision,
			ruleID: ruleID, subjects: map[string]struct{}{}, claims: map[uuid.UUID]struct{}{},
			provenance: map[string]struct{}{},
		}
		candidates[key] = candidate
	}
	candidate.subjects[subject] = struct{}{}
	candidate.claims[one.ClaimID] = struct{}{}
	candidate.claims[two.ClaimID] = struct{}{}
	candidate.provenance[one.ProvenanceKind] = struct{}{}
	candidate.provenance[two.ProvenanceKind] = struct{}{}
}

func (c collisionCandidate) assessment(projectID uuid.UUID, at protocol.Timestamp) protocol.CollisionAssessment {
	claimRefs := make([]uuid.UUID, 0, len(c.claims))
	for claimID := range c.claims {
		claimRefs = append(claimRefs, claimID)
	}
	sort.Slice(claimRefs, func(i, j int) bool { return claimRefs[i].String() < claimRefs[j].String() })
	if len(claimRefs) > 20 {
		claimRefs = claimRefs[:20]
	}
	subjects := make([]string, 0, len(c.subjects))
	for subject := range c.subjects {
		subjects = append(subjects, subject)
	}
	sort.Strings(subjects)
	matched := append([]string(nil), subjects...)
	if len(matched) > protocol.MaxMatchedSubjects {
		matched = matched[:protocol.MaxMatchedSubjects]
	}
	if len(subjects) > 3 {
		subjects = append(subjects[:3], "additional normalized subjects")
	}
	provenance := make([]string, 0, len(c.provenance))
	for kind := range c.provenance {
		provenance = append(provenance, kind)
	}
	sort.Strings(provenance)
	return protocol.CollisionAssessment{
		CollisionID: uuid.NewV7(), ProjectID: projectID,
		LeftIntentID: c.leftIntentID, LeftIntentRevision: c.leftRevision,
		RightIntentID: c.rightIntentID, RightIntentRevision: c.rightRevision,
		RuleID: c.ruleID, Severity: "amber", Confidence: 1, State: protocol.CollisionOpen,
		MatchingClaimRefs:   claimRefs,
		MatchedSubjects:     matched,
		ExplanationMarkdown: fmt.Sprintf("Rule `%s` matched normalized subject(s) `%s` from %s claims on active Intent revisions %s@%d and %s@%d. Matching claim identifiers provide the provenance for this advisory assessment.", c.ruleID, strings.Join(subjects, "`, `"), strings.Join(provenance, " and "), c.leftIntentID, c.leftRevision, c.rightIntentID, c.rightRevision),
		CreatedAt:           at,
	}
}
