package application

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"uuid"

	"github.com/Juanqu261/conn/internal/protocol"
)

// Catalog bounds.
const (
	DefaultConceptPage      = 50
	MaxConceptPage          = 200
	maxConceptQueryRunes    = 200
	maxConceptClaimRefs     = 20
	maxConceptObjectiveRefs = 20
	maxNearMatchesPerClaim  = 3
	maxNearMatchObservation = 20
	nearMatchThreshold      = 0.5
)

// ConceptQuery filters and pages the concept catalog. Kind and Query are
// optional. After is the opaque continuation from a previous page.
type ConceptQuery struct {
	Kind        string
	Query       string
	ObjectiveID uuid.UUID
	Limit       int
	After       string
}

// conceptStopTokens are tokens too common in subjects to signal similarity.
var conceptStopTokens = map[string]bool{"typescript": true, "src": true, "index": true, "lib": true, "the": true, "and": true, "for": true}

// catalogEntry is one subject in the in-memory catalog read model.
type catalogEntry struct {
	protocol.ConceptEntry
	tokens map[string]bool
}

// ObjectiveSubject derives the stable catalog subject for one keyed Objective
// point. Only invariants and acceptance criteria derive subjects. The
// Objective key is used when present so revisions that keep both keys derive
// the same subject.
func ObjectiveSubject(objective protocol.Objective, point protocol.KeyPoint) (subject, kind string, ok bool) {
	ref := objective.Key
	if ref == "" {
		ref = "objective-" + objective.ObjectiveID.String()
	}
	switch point.Kind {
	case protocol.KeyPointInvariant:
		return "invariant:" + ref + ":" + point.Key, "invariant", true
	case protocol.KeyPointAcceptanceCriterion:
		return "acceptance:" + ref + ":" + point.Key, "acceptance_criterion", true
	}
	return "", "", false
}

// buildCatalog merges Objective-derived subjects and the normalized subjects
// of active claims. Claims of excludeIntent are left out so a publication's
// own claims never count as an existing team subject.
func buildCatalog(objectives []protocol.Objective, claims []protocol.SemanticClaim, excludeIntent uuid.UUID) map[string]*catalogEntry {
	catalog := map[string]*catalogEntry{}
	entry := func(subject, kind, display string) *catalogEntry {
		item := catalog[subject]
		if item == nil {
			item = &catalogEntry{ConceptEntry: protocol.ConceptEntry{
				NormalizedSubject: subject, SubjectKind: kind, DisplayName: boundedTitle(display),
				Sources: []string{}, ObjectiveRefs: []protocol.ConceptObjectiveRef{}, Claims: []protocol.ConceptClaimRef{},
			}}
			catalog[subject] = item
		}
		return item
	}
	for _, objective := range objectives {
		for _, point := range objective.KeyPoints {
			subject, kind, ok := ObjectiveSubject(objective, point)
			if !ok {
				continue
			}
			item := entry(subject, kind, point.Statement)
			addSource(item, protocol.ConceptSourceObjective)
			if len(item.ObjectiveRefs) < maxConceptObjectiveRefs {
				item.ObjectiveRefs = append(item.ObjectiveRefs, protocol.ConceptObjectiveRef{ObjectiveID: objective.ObjectiveID, ObjectiveRevision: objective.Revision, KeyPointKey: point.Key})
			}
		}
	}
	for _, claim := range claims {
		if claim.IntentID == excludeIntent {
			continue
		}
		item := entry(claim.NormalizedSubject, claim.SubjectKind, claim.DisplayName)
		addSource(item, protocol.ConceptSourceActiveClaim)
		item.ClaimCount++
		if len(item.Claims) < maxConceptClaimRefs {
			item.Claims = append(item.Claims, protocol.ConceptClaimRef{ClaimID: claim.ClaimID, IntentID: claim.IntentID, IntentRevision: claim.IntentRevision, Relation: claim.Relation, ProvenanceKind: claim.ProvenanceKind})
		}
	}
	for _, item := range catalog {
		item.tokens = conceptTokens(item.NormalizedSubject, item.DisplayName)
	}
	return catalog
}

func addSource(item *catalogEntry, source string) {
	for _, existing := range item.Sources {
		if existing == source {
			return
		}
	}
	item.Sources = append(item.Sources, source)
	sort.Strings(item.Sources)
}

// conceptTokens returns the similarity tokens of a subject after its
// namespace and of a display name. Text splits on `: / . _ - ~`, whitespace,
// and camelCase boundaries and is lowercased. Short and stop-list tokens are
// dropped. It reads no source content.
func conceptTokens(subject, display string) map[string]bool {
	tokens := map[string]bool{}
	if _, rest, found := strings.Cut(subject, ":"); found {
		addTokens(tokens, rest)
	} else {
		addTokens(tokens, subject)
	}
	addTokens(tokens, display)
	return tokens
}

func addTokens(tokens map[string]bool, text string) {
	for _, word := range splitWords(text) {
		word = strings.ToLower(word)
		if utf8.RuneCountInString(word) >= 3 && !conceptStopTokens[word] {
			tokens[word] = true
		}
	}
}

func splitWords(text string) []string {
	var words []string
	var current []rune
	flush := func() {
		if len(current) > 0 {
			words = append(words, string(current))
			current = current[:0]
		}
	}
	runes := []rune(text)
	for i, r := range runes {
		switch {
		case strings.ContainsRune(":/._-~", r) || unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r):
			flush()
		case unicode.IsUpper(r) && len(current) > 0 && (unicode.IsLower(current[len(current)-1]) || unicode.IsDigit(current[len(current)-1]) ||
			(i+1 < len(runes) && unicode.IsLower(runes[i+1]) && unicode.IsUpper(current[len(current)-1]))):
			// fooBar → foo|Bar, HTTPServer → HTTP|Server.
			flush()
			current = append(current, r)
		default:
			current = append(current, r)
		}
	}
	flush()
	return words
}

// SubjectSimilarity is the deterministic Jaccard similarity between the token
// sets of two subjects and their display names. Equal subjects score 1.
func SubjectSimilarity(leftSubject, leftDisplay, rightSubject, rightDisplay string) float64 {
	if leftSubject == rightSubject {
		return 1
	}
	score, _ := jaccard(conceptTokens(leftSubject, leftDisplay), conceptTokens(rightSubject, rightDisplay))
	return score
}

func jaccard(left, right map[string]bool) (float64, []string) {
	if len(left) == 0 || len(right) == 0 {
		return 0, nil
	}
	shared := []string{}
	for token := range left {
		if right[token] {
			shared = append(shared, token)
		}
	}
	sort.Strings(shared)
	union := len(left) + len(right) - len(shared)
	return float64(len(shared)) / float64(union), shared
}

// ConceptsAs returns one bounded catalog page at one cursor to an authorized reader.
func (p *Projects) ConceptsAs(ctx context.Context, reader Reader, projectID uuid.UUID, query ConceptQuery) (Result[protocol.ConceptList], error) {
	if query.Limit == 0 {
		query.Limit = DefaultConceptPage
	}
	if query.Limit < 1 || query.Limit > MaxConceptPage {
		return Result[protocol.ConceptList]{}, &InvalidError{Message: fmt.Sprintf("The concept limit must be between 1 and %d.", MaxConceptPage)}
	}
	if utf8.RuneCountInString(query.Query) > maxConceptQueryRunes {
		return Result[protocol.ConceptList]{}, &InvalidError{Message: fmt.Sprintf("The concept query must be at most %d characters.", maxConceptQueryRunes)}
	}
	offset := 0
	if query.After != "" {
		value, err := strconv.Atoi(query.After)
		if err != nil || value < 0 || value > 999_999 || len(query.After) > 6 {
			return Result[protocol.ConceptList]{}, &InvalidError{Message: "The after continuation is invalid."}
		}
		offset = value
	}
	var result Result[protocol.ConceptList]
	err := p.Store.Read(ctx, func(tx Tx) error {
		if err := reader.authorize(ctx, tx, projectID); err != nil {
			return err
		}
		objectives, err := tx.CurrentObjectives(ctx, projectID)
		if err != nil {
			return err
		}
		claims, err := tx.ActiveSemanticClaims(ctx, projectID)
		if err != nil {
			return err
		}
		ranked := rankCatalog(buildCatalog(objectives, claims, uuid.UUID{}), query)
		list := protocol.ConceptList{ProjectID: projectID, Concepts: []protocol.ConceptEntry{}}
		if offset < len(ranked) {
			end := min(len(ranked), offset+query.Limit)
			list.Concepts = ranked[offset:end]
			if end < len(ranked) {
				list.HasMore = true
				list.ContinuationAfter = strconv.Itoa(end)
			}
		}
		if err := validateRecord(list); err != nil {
			return err
		}
		result.Data = list
		result.AsOfEvent, err = tx.LastEvent(ctx, projectID)
		return err
	})
	return result, err
}

// rankCatalog filters by kind and Objective and, with a query, keeps entries
// that share at least one token, ordered by score and then subject. Without a
// query, entries are ordered by subject.
func rankCatalog(catalog map[string]*catalogEntry, query ConceptQuery) []protocol.ConceptEntry {
	queryText := strings.TrimSpace(query.Query)
	queryTokens := map[string]bool{}
	addTokens(queryTokens, queryText)
	entries := make([]protocol.ConceptEntry, 0, len(catalog))
	for _, item := range catalog {
		if query.Kind != "" && item.SubjectKind != query.Kind {
			continue
		}
		if query.ObjectiveID != (uuid.UUID{}) && !hasObjective(item.ObjectiveRefs, query.ObjectiveID) {
			continue
		}
		entry := item.ConceptEntry
		if queryText != "" {
			score, shared := jaccard(queryTokens, item.tokens)
			reason := ""
			if queryText == item.NormalizedSubject {
				score, reason = 1, "Exact subject match."
			} else if len(shared) > 0 {
				reason = "Shared tokens: " + strings.Join(shared, ", ") + "."
			}
			if score == 0 {
				continue
			}
			entry.Score, entry.Reason = &score, boundedStatement(reason)
		}
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Score != nil && entries[j].Score != nil && *entries[i].Score != *entries[j].Score {
			return *entries[i].Score > *entries[j].Score
		}
		return entries[i].NormalizedSubject < entries[j].NormalizedSubject
	})
	return entries
}

func hasObjective(refs []protocol.ConceptObjectiveRef, objectiveID uuid.UUID) bool {
	for _, ref := range refs {
		if ref.ObjectiveID == objectiveID {
			return true
		}
	}
	return false
}

// nearMatchObservations advises a participant when a declared subject is
// absent from the catalog but resembles a catalog subject of the same kind.
// It reads only subjects and identifiers and changes no record.
func nearMatchObservations(catalog map[string]*catalogEntry, revision protocol.IntentRevision) []string {
	observations := []string{}
	seen := map[string]bool{}
	for _, claim := range revision.DeclaredClaims {
		if len(observations) == maxNearMatchObservation {
			break
		}
		if seen[claim.NormalizedSubject] || catalog[claim.NormalizedSubject] != nil {
			continue
		}
		seen[claim.NormalizedSubject] = true
		declared := conceptTokens(claim.NormalizedSubject, claim.DisplayName)
		type match struct {
			entry *catalogEntry
			score float64
		}
		var matches []match
		for _, item := range catalog {
			if item.SubjectKind != claim.SubjectKind {
				continue
			}
			if score, _ := jaccard(declared, item.tokens); score >= nearMatchThreshold {
				matches = append(matches, match{item, score})
			}
		}
		if len(matches) == 0 {
			continue
		}
		sort.Slice(matches, func(i, j int) bool {
			if matches[i].score != matches[j].score {
				return matches[i].score > matches[j].score
			}
			return matches[i].entry.NormalizedSubject < matches[j].entry.NormalizedSubject
		})
		if len(matches) > maxNearMatchesPerClaim {
			matches = matches[:maxNearMatchesPerClaim]
		}
		parts := make([]string, 0, len(matches))
		for _, m := range matches {
			parts = append(parts, fmt.Sprintf("`%s` (%s)", m.entry.NormalizedSubject, claimantSummary(m.entry)))
		}
		text := fmt.Sprintf("Near match: declared subject `%s` is not in the Project catalog. Similar %s subjects: %s. Reuse an existing subject when it names the same concept, so Conn can compare the work.", claim.NormalizedSubject, claim.SubjectKind, strings.Join(parts, "; "))
		observations = append(observations, boundedRunes(text, 1000))
	}
	return observations
}

func claimantSummary(entry *catalogEntry) string {
	if len(entry.Claims) == 0 {
		return "from the Objective"
	}
	parts := make([]string, 0, min(3, len(entry.Claims)))
	for _, claim := range entry.Claims[:min(3, len(entry.Claims))] {
		parts = append(parts, fmt.Sprintf("Intent %s@%d %s", claim.IntentID, claim.IntentRevision, claim.Relation))
	}
	if entry.ClaimCount > len(parts) {
		parts = append(parts, fmt.Sprintf("%d more", entry.ClaimCount-len(parts)))
	}
	return strings.Join(parts, ", ")
}

// boundedTitle fits text into the title contract: at most 200 runes and at
// least one non-whitespace character.
func boundedTitle(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if text == "" {
		return "concept"
	}
	return boundedRunes(text, 200)
}

func boundedRunes(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return strings.TrimSpace(string(runes[:limit-1])) + truncatedStatementMarker
}
