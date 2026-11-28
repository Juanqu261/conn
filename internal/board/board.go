// Package board derives Conn Lab's Intent board ("The Slab") from Conn
// records. Every Intent's lane follows from one deterministic rule set over
// a loaded snapshot; nobody sets a lane, so the board can never be out of
// date or gamed (web-lab-board requirements 1 and 3).
package board

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"uuid"

	"github.com/Juanqu261/conn/internal/protocol"
	"github.com/Juanqu261/conn/internal/review"
)

// Window bounds for released Intents.
const (
	DefaultWindowDays = 14
	MaxWindowDays     = 90
)

// MaxLaneCards bounds the cards one lane lists; the lane total still counts
// every Intent.
const MaxLaneCards = 500

// maxListedKeys bounds the acceptance keys a sentence names.
const maxListedKeys = 5

// ObjectiveRef names one exact Objective revision.
type ObjectiveRef struct {
	ID       uuid.UUID
	Revision int
}

// Snapshot is everything the lane rules read, loaded at one event cursor.
type Snapshot struct {
	Now        time.Time
	WindowDays int
	Intents    []protocol.IntentView
	// ReleasedAt holds when each released Intent was released, when known.
	ReleasedAt map[uuid.UUID]time.Time
	// Collisions are every collision in state open or under_review.
	Collisions []protocol.CollisionAssessment
	// Analyzed maps an Intent to the latest revision with an observed
	// analysis report.
	Analyzed map[uuid.UUID]int
	// Objectives holds the Objective revision of every Intent.
	Objectives map[ObjectiveRef]protocol.Objective
	// Evidence holds the evidence attached to Intents for those Objective
	// revisions, in any order.
	Evidence []protocol.EvidenceReference
}

// Card returns the board card of one Intent, or false when the Intent is
// not on the board: superseded, or released before the window.
func Card(intent protocol.IntentView, s Snapshot) (string, protocol.BoardCard, bool) {
	revision := intent.IntentRevision
	card := protocol.BoardCard{
		IntentID: revision.IntentID, IntentRevision: revision.Revision, Status: intent.Status,
		SummaryLine: SummaryLine(revision.SummaryMarkdown), OwnerHumanID: intent.OwnerHumanID,
		ObjectiveID: revision.ObjectiveID, ObjectiveRevision: revision.ObjectiveRevision,
		RepositoryID: revision.RepositoryID, BranchRef: boundRef(revision.BranchRef),
		BaseRevisionRef: boundRef(revision.BaseRevisionRef), HeadRevisionRef: boundRef(revision.HeadRevisionRef),
		OpenCollisionIDs: []uuid.UUID{},
	}
	accept := acceptance(revision, s)
	card.Acceptance = accept.counts
	card.LatestEvidenceAt, card.EvidenceStale = accept.latestAt, accept.latestStale

	switch intent.Status {
	case "active":
	case "released":
		releasedAt, known := s.ReleasedAt[revision.IntentID]
		window := time.Duration(s.WindowDays) * 24 * time.Hour
		if !known || releasedAt.Before(s.Now.Add(-window)) {
			return "", protocol.BoardCard{}, false
		}
		at := protocol.NewTimestamp(releasedAt)
		card.ReleasedAt = &at
		card.Reason = protocol.BoardReason{
			Rule:          protocol.RuleReleasedRecently,
			FactsMarkdown: fmt.Sprintf("Released %s; released Intents stay on the board for %d days.", ago(s.Now, releasedAt), s.WindowDays),
		}
		return protocol.LaneBuried, card, true
	default:
		return "", protocol.BoardCard{}, false
	}

	var open []protocol.CollisionAssessment
	for _, collision := range review.IntentCollisions(s.Collisions, revision.IntentID, revision.Revision) {
		if collision.State == protocol.CollisionOpen || collision.State == protocol.CollisionUnderReview {
			open = append(open, collision)
		}
	}
	sort.Slice(open, func(i, j int) bool { return open[i].CollisionID.String() < open[j].CollisionID.String() })
	for i, collision := range open {
		if i < 100 {
			card.OpenCollisionIDs = append(card.OpenCollisionIDs, collision.CollisionID)
		}
	}
	if len(open) > 0 {
		card.Reason = protocol.BoardReason{
			Rule:          protocol.RuleOpenCollision,
			FactsMarkdown: collisionFacts(open, revision.IntentID, s),
			NextStep:      "A human decision on each open stitch moves this card on: decide it on the collision board, in conn app, or with conn resolution decide.",
		}
		return protocol.LaneStitching, card, true
	}

	if strings.TrimSpace(revision.HeadRevisionRef) == "" {
		card.Reason = protocol.BoardReason{
			Rule:          protocol.RuleNoHeadRevision,
			FactsMarkdown: fmt.Sprintf("Revision %d records no head commit yet.", revision.Revision),
			NextStep:      "Publishing a revision with the head commit moves this card on.",
		}
		return protocol.LaneAssembling, card, true
	}
	if analyzed, ok := s.Analyzed[revision.IntentID]; !ok || analyzed != revision.Revision {
		facts := fmt.Sprintf("Revision %d has no observed analysis yet.", revision.Revision)
		if ok {
			facts = fmt.Sprintf("The latest observed analysis covers revision %d, not the current revision %d.", analyzed, revision.Revision)
		}
		card.Reason = protocol.BoardReason{
			Rule:          protocol.RuleNoObservedAnalysis,
			FactsMarkdown: facts,
			NextStep:      fmt.Sprintf("Running conn intent analyze on revision %d moves this card on.", revision.Revision),
		}
		return protocol.LaneAssembling, card, true
	}

	if accept.counts.Required == 0 {
		card.Reason = protocol.BoardReason{
			Rule:          protocol.RuleNoAcceptanceCriteria,
			FactsMarkdown: "The Objective revision has no acceptance criteria, so the proof rule was skipped.",
		}
		return protocol.LaneAlive, card, true
	}
	if len(accept.missing) > 0 {
		card.Reason = protocol.BoardReason{
			Rule: protocol.RuleAcceptanceIncomplete,
			FactsMarkdown: fmt.Sprintf("%d of %d acceptance criteria passing; %s.",
				accept.counts.Passing, accept.counts.Required, describeMissing(accept.missing)),
			NextStep: fmt.Sprintf("Attaching passing evidence at the head commit for %s moves this to Alive.", keyList(missingKeys(accept.missing))),
		}
		return protocol.LaneCharging, card, true
	}
	card.Reason = protocol.BoardReason{
		Rule:          protocol.RuleReady,
		FactsMarkdown: fmt.Sprintf("All %d acceptance criteria have passing evidence at the head commit.", accept.counts.Required),
	}
	return protocol.LaneAlive, card, true
}

// Build derives the whole board. Lanes appear in display order; within a
// lane, the most recently revised Intent comes first.
func Build(projectID uuid.UUID, asOfEvent int64, s Snapshot) protocol.BoardView {
	type placed struct {
		card protocol.BoardCard
		at   time.Time
	}
	byLane := map[string][]placed{}
	for _, intent := range s.Intents {
		lane, card, ok := Card(intent, s)
		if ok {
			byLane[lane] = append(byLane[lane], placed{card, intent.IntentRevision.CreatedAt.Time()})
		}
	}
	view := protocol.BoardView{BoardVersion: protocol.BoardVersion, ProjectID: projectID, AsOfEvent: asOfEvent, WindowDays: s.WindowDays, Lanes: []protocol.BoardLane{}}
	for _, name := range protocol.BoardLanes {
		cards := byLane[name]
		sort.SliceStable(cards, func(i, j int) bool {
			if !cards[i].at.Equal(cards[j].at) {
				return cards[i].at.After(cards[j].at)
			}
			return cards[i].card.IntentID.String() < cards[j].card.IntentID.String()
		})
		lane := protocol.BoardLane{Lane: name, Count: len(cards), Cards: []protocol.BoardCard{}}
		for i, item := range cards {
			if i == MaxLaneCards {
				view.Omitted += len(cards) - MaxLaneCards
				break
			}
			lane.Cards = append(lane.Cards, item.card)
		}
		view.Lanes = append(view.Lanes, lane)
	}
	return view
}

type missingKey struct{ key, state string }

type acceptanceFacts struct {
	counts      protocol.BoardAcceptance
	missing     []missingKey
	latestAt    *protocol.Timestamp
	latestStale bool
}

// acceptance applies the review packet's evidence rule to every acceptance
// criterion of the Intent's Objective revision: the latest evidence for the
// key from this Intent decides, and it counts only when it passes and is not
// stale against the recorded head.
func acceptance(revision protocol.IntentRevision, s Snapshot) acceptanceFacts {
	facts := acceptanceFacts{counts: protocol.BoardAcceptance{MissingKeys: []string{}}}
	var own []protocol.EvidenceReference
	for _, evidence := range s.Evidence {
		if evidence.IntentID != nil && *evidence.IntentID == revision.IntentID &&
			evidence.ObjectiveID == revision.ObjectiveID && evidence.ObjectiveRevision == revision.ObjectiveRevision {
			own = append(own, evidence)
		}
	}
	// Newest first, as the Intent evidence route lists it.
	sort.Slice(own, func(i, j int) bool {
		a, b := own[i].ObservedAt.Time(), own[j].ObservedAt.Time()
		if !a.Equal(b) {
			return a.After(b)
		}
		return own[i].EvidenceID.String() > own[j].EvidenceID.String()
	})
	head := revision.HeadRevisionRef
	if len(own) > 0 {
		at := own[0].ObservedAt
		facts.latestAt, facts.latestStale = &at, review.EvidenceStale(own[0], head)
	}
	objective := s.Objectives[ObjectiveRef{revision.ObjectiveID, revision.ObjectiveRevision}]
	for _, point := range objective.KeyPoints {
		if point.Kind != protocol.KeyPointAcceptanceCriterion {
			continue
		}
		facts.counts.Required++
		state := protocol.EvidenceMissing
		for _, evidence := range own {
			if evidence.AcceptanceKey != point.Key {
				continue
			}
			state = evidence.State
			if review.EvidenceStale(evidence, head) {
				state = protocol.EvidenceStale
			}
			break
		}
		if state == protocol.EvidencePassing {
			facts.counts.Passing++
			continue
		}
		facts.missing = append(facts.missing, missingKey{point.Key, state})
		if len(facts.counts.MissingKeys) < 50 {
			facts.counts.MissingKeys = append(facts.counts.MissingKeys, point.Key)
		}
	}
	return facts
}

func collisionFacts(open []protocol.CollisionAssessment, intentID uuid.UUID, s Snapshot) string {
	names := map[uuid.UUID]string{}
	for _, intent := range s.Intents {
		names[intent.IntentRevision.IntentID] = SummaryLine(intent.IntentRevision.SummaryMarkdown)
	}
	parts := make([]string, 0, len(open))
	for i, collision := range open {
		if i == 3 {
			parts = append(parts, fmt.Sprintf("and %d more", len(open)-3))
			break
		}
		other := collision.RightIntentID
		if other == intentID {
			other = collision.LeftIntentID
		}
		name := names[other]
		if name == "" {
			name = "an unlisted Intent"
		} else {
			name = "“" + name + "”"
		}
		subjects := collision.MatchedSubjects
		if len(subjects) > 2 {
			subjects = append(append([]string{}, subjects[:2]...), fmt.Sprintf("%d more", len(collision.MatchedSubjects)-2))
		}
		parts = append(parts, fmt.Sprintf("collides with %s on %s (%s)", name, strings.Join(subjects, ", "), strings.ReplaceAll(collision.State, "_", " ")))
	}
	plural := "collision needs"
	if len(open) != 1 {
		plural = "collisions need"
	}
	return bound(fmt.Sprintf("%d %s a human decision: %s.", len(open), plural, strings.Join(parts, "; ")))
}

func describeMissing(missing []missingKey) string {
	byState := map[string][]string{}
	order := []string{}
	for _, item := range missing {
		if _, seen := byState[item.state]; !seen {
			order = append(order, item.state)
		}
		byState[item.state] = append(byState[item.state], item.key)
	}
	parts := make([]string, 0, len(order))
	for _, state := range order {
		parts = append(parts, state+" "+keyList(byState[state]))
	}
	return bound(strings.Join(parts, "; "))
}

func missingKeys(missing []missingKey) []string {
	keys := make([]string, len(missing))
	for i, item := range missing {
		keys[i] = item.key
	}
	return keys
}

func keyList(keys []string) string {
	shown := keys
	if len(shown) > maxListedKeys {
		shown = shown[:maxListedKeys]
	}
	quoted := make([]string, len(shown))
	for i, key := range shown {
		quoted[i] = "`" + key + "`"
	}
	text := strings.Join(quoted, ", ")
	if len(keys) > len(shown) {
		text += fmt.Sprintf(" and %d more", len(keys)-len(shown))
	}
	return text
}

// SummaryLine is an Intent's display name: the first meaningful line of its
// summary without Markdown markers, bounded for the title contract. The
// Lab's name directory applies the same rule.
func SummaryLine(markdown string) string {
	for _, raw := range strings.Split(strings.ReplaceAll(markdown, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(raw)
		line = strings.TrimSpace(strings.TrimLeft(line, "#"))
		for _, marker := range []string{"- ", "* ", "+ ", "> "} {
			line = strings.TrimPrefix(line, marker)
		}
		line = strings.Map(func(r rune) rune {
			if strings.ContainsRune("*_`~", r) {
				return -1
			}
			return r
		}, line)
		if line = review.SanitizeLine(line, 120); line != "" {
			return line
		}
	}
	return "Untitled Intent"
}

func ago(now, then time.Time) string {
	days := int(now.Sub(then).Hours() / 24)
	switch {
	case days <= 0:
		return "today"
	case days == 1:
		return "1 day ago"
	default:
		return fmt.Sprintf("%d days ago", days)
	}
}

func boundRef(ref string) string {
	ref = strings.TrimSpace(ref)
	if runes := []rune(ref); len(runes) > 256 {
		return string(runes[:256])
	}
	return ref
}

func bound(text string) string { return review.SanitizeLine(text, 1000) }
