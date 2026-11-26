package application_test

import (
	"math"
	"testing"

	"uuid"

	"github.com/Juanqu261/conn/internal/application"
	"github.com/Juanqu261/conn/internal/protocol"
)

// TestSubjectSimilarityIsDeterministic protects the catalog's ranking and
// near-match threshold: token similarity reads only subjects and display
// names, splits namespaces, paths, and camelCase, and drops short and stop
// tokens.
func TestSubjectSimilarityIsDeterministic(t *testing.T) {
	tests := []struct {
		name               string
		left, leftDisplay  string
		right, rightDispla string
		want               float64
	}{
		{"identical subjects", "contract:typescript:src/a.ts/run", "run", "contract:typescript:src/a.ts/run", "other", 1},
		{"singular and plural directory", "contract:typescript:src/payment/retry.ts/shouldRetryCharge", "shouldRetryCharge", "contract:typescript:src/payments/retry.ts/shouldRetryCharge", "shouldRetryCharge", 0.6},
		{"camelCase and acronyms split", "symbol:typescript:src/http.ts/HTTPServerStart", "HTTPServerStart", "symbol:typescript:src/server.ts/startServer", "startServer", 2.0 / 3.0},
		{"namespace is ignored", "invariant:checkout:one-charge", "one charge", "contract:checkout:one-charge", "one charge", 1.0 / 1.0},
		{"stop and short tokens are dropped", "symbol:typescript:src/index.ts/id", "id", "symbol:typescript:lib/index.ts/db", "db", 0},
		{"unrelated subjects", "data:postgres:orders:status", "Order status", "contract:typescript:src/payments/retry.ts/refund", "refund", 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := application.SubjectSimilarity(tc.left, tc.leftDisplay, tc.right, tc.rightDispla)
			if math.Abs(got-tc.want) > 1e-9 {
				t.Fatalf("similarity = %v, want %v", got, tc.want)
			}
			if again := application.SubjectSimilarity(tc.right, tc.rightDispla, tc.left, tc.leftDisplay); math.Abs(again-got) > 1e-9 {
				t.Fatalf("similarity is not symmetric: %v and %v", got, again)
			}
		})
	}
}

// TestObjectiveSubjectsAreStable protects requirement 2.1 and 2.2: the
// Objective key, or its ID when no key exists, and the key-point key derive
// the subject, so later revisions that keep both keys derive the same one.
func TestObjectiveSubjectsAreStable(t *testing.T) {
	id := uuid.NewV7()
	keyed := protocol.Objective{ObjectiveID: id, Key: "checkout", Revision: 1}
	unkeyed := protocol.Objective{ObjectiveID: id, Revision: 7}
	tests := []struct {
		objective protocol.Objective
		point     protocol.KeyPoint
		subject   string
		kind      string
		ok        bool
	}{
		{keyed, protocol.KeyPoint{Key: "one-charge", Kind: protocol.KeyPointInvariant}, "invariant:checkout:one-charge", "invariant", true},
		{protocol.Objective{ObjectiveID: id, Key: "checkout", Revision: 9}, protocol.KeyPoint{Key: "one-charge", Kind: protocol.KeyPointInvariant}, "invariant:checkout:one-charge", "invariant", true},
		{keyed, protocol.KeyPoint{Key: "retry-evidence", Kind: protocol.KeyPointAcceptanceCriterion}, "acceptance:checkout:retry-evidence", "acceptance_criterion", true},
		{unkeyed, protocol.KeyPoint{Key: "one-charge", Kind: protocol.KeyPointInvariant}, "invariant:objective-" + id.String() + ":one-charge", "invariant", true},
		{keyed, protocol.KeyPoint{Key: "limits", Kind: protocol.KeyPointQuestion}, "", "", false},
	}
	for _, tc := range tests {
		subject, kind, ok := application.ObjectiveSubject(tc.objective, tc.point)
		if subject != tc.subject || kind != tc.kind || ok != tc.ok {
			t.Errorf("ObjectiveSubject(%s, %s) = %q %q %v", tc.objective.Key, tc.point.Key, subject, kind, ok)
		}
	}
}
