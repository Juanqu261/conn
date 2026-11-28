package speckit

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Juanqu261/conn/internal/protocol"
)

var fixture = filepath.Join("..", "..", "fixtures", "speckit")

// TestFixtureFeatures protects speckit-coordination requirements 1.1-1.5,
// 2.1-2.5, and 8.1 over the committed fixture: titles, overviews, key points
// with placeholders and repeated IDs skipped, constitution principles, and
// every path rule for tasks.md and plan.md.
func TestFixtureFeatures(t *testing.T) {
	root, err := filepath.Abs(fixture)
	if err != nil {
		t.Fatal(err)
	}
	constitution, err := FindConstitution(root)
	if err != nil || constitution == nil || constitution.Ref != ".specify/memory/constitution.md" || ConstitutionVersion(constitution.Content) != "2.1.1" {
		t.Fatalf("constitution = %+v, %v", constitution, err)
	}
	type keyPoint struct {
		key       string
		kind      protocol.KeyPointKind
		statement string
	}
	for _, tc := range []struct {
		dir        string
		title      string
		overview   string
		points     []keyPoint
		extraction Extraction
	}{
		{
			dir:      "001-checkout-retries",
			title:    "Checkout retries",
			overview: "Retries must never charge a customer twice.",
			points: []keyPoint{
				{"fr-001", protocol.KeyPointAcceptanceCriterion, "System MUST reuse the idempotency key on every retry of one checkout attempt."},
				{"fr-002", protocol.KeyPointAcceptanceCriterion, "System MUST stop retrying after [NEEDS CLARIFICATION: retry limit not specified - three or five?] attempts."},
				{"sc-001", protocol.KeyPointOutcome, "No shopper is charged twice for one checkout attempt."},
				{"sc-002", protocol.KeyPointOutcome, "Retried checkouts complete within 10 seconds for 95% of attempts."},
				{"question-1", protocol.KeyPointQuestion, "provider timeout semantics are unknown"},
				{"question-2", protocol.KeyPointQuestion, "retry limit not specified - three or five?"},
				{"constitution-library-first", protocol.KeyPointConstraint, "I. Library-First. Every feature starts as a standalone library."},
				{"constitution-test-first", protocol.KeyPointConstraint, "II. Test-First (NON-NEGOTIABLE). Tests are written before the implementation and fail first."},
			},
			extraction: Extraction{
				Source:           protocol.SpecKitSourceTasks,
				Paths:            []string{"src/payments/retry.ts", "src/payments/retry-policy.ts", "src/orders/checkout.ts"},
				NewPaths:         []string{"src/payments/retry-policy.ts"},
				Unresolved:       []string{"docs/missing/guide.md"},
				TasksWithoutPath: []string{"T001", "T004", "T006"},
			},
		},
		{
			dir:      "002-order-limits",
			title:    "Order limits",
			overview: "Orders above the account limit are declined before any charge is attempted.",
			points: []keyPoint{
				{"fr-001", protocol.KeyPointAcceptanceCriterion, "System MUST decline an order above the account limit before charging."},
				{"sc-001", protocol.KeyPointOutcome, "No order above the account limit reaches the payment provider."},
			},
			extraction: Extraction{
				Source: protocol.SpecKitSourceTasks, Paths: []string{"src/payments/retry.ts"},
				NewPaths: []string{}, Unresolved: []string{}, TasksWithoutPath: []string{},
			},
		},
		{
			dir:      "003-refund-audit",
			title:    "Refund audit",
			overview: "Imported from the Spec Kit feature `specs/003-refund-audit/spec.md`.",
			points: []keyPoint{
				{"fr-001", protocol.KeyPointAcceptanceCriterion, "System MUST record every refund with the original charge reference."},
			},
			extraction: Extraction{
				Source:   protocol.SpecKitSourcePlan,
				Paths:    []string{"src/payments/retry.ts", "src/payments/refunds.ts", "src/orders/checkout.ts"},
				NewPaths: []string{"src/payments/refunds.ts"}, Unresolved: []string{"tests/refunds.test.ts"}, TasksWithoutPath: []string{},
			},
		},
	} {
		t.Run(tc.dir, func(t *testing.T) {
			for _, given := range []string{filepath.Join(root, "specs", tc.dir), filepath.Join(root, "specs", tc.dir, SpecFile)} {
				feature, err := Resolve(root, given)
				if err != nil || feature.SpecRef != "specs/"+tc.dir+"/spec.md" || feature.DirRef != "specs/"+tc.dir {
					t.Fatalf("Resolve(%s) = %+v, %v", given, feature, err)
				}
			}
			feature, _ := Resolve(root, filepath.Join(root, "specs", tc.dir))
			spec, err := ReadFile(feature.SpecPath())
			if err != nil {
				t.Fatal(err)
			}
			if got := Title(spec, feature.Name()); got != tc.title {
				t.Errorf("title = %q, want %q", got, tc.title)
			}
			if got := Overview(spec, feature.SpecRef); got != tc.overview {
				t.Errorf("overview = %q, want %q", got, tc.overview)
			}
			var withConstitution []byte
			if tc.dir == "001-checkout-retries" {
				withConstitution = constitution.Content
			}
			points, omitted := KeyPoints(spec, withConstitution)
			got := make([]keyPoint, 0, len(points))
			for _, point := range points {
				got = append(got, keyPoint{point.Key, point.Kind, point.Statement})
			}
			if !reflect.DeepEqual(got, tc.points) || len(omitted) != 0 {
				t.Errorf("key points =\n%q\nwant\n%q (omitted %v)", got, tc.points, omitted)
			}
			extraction, err := PlannedPaths(feature)
			if err != nil || !reflect.DeepEqual(extraction, tc.extraction) {
				t.Errorf("extraction = %+v, %v\nwant %+v", extraction, err, tc.extraction)
			}
		})
	}
}

// TestBoundsAndRefusals protects requirements 1.6, 2.4, and 7.2: key points
// stop at 50 in priority order and report the rest, paths stop at 200,
// files over 1 MiB and symbolic links are refused, and a feature outside
// the repository is refused.
func TestBoundsAndRefusals(t *testing.T) {
	var spec strings.Builder
	spec.WriteString("# Feature Specification: Many\n\n")
	for i := 1; i <= 45; i++ {
		fmt.Fprintf(&spec, "- **FR-%03d**: System MUST do thing %d.\n", i, i)
	}
	for i := 1; i <= 10; i++ {
		fmt.Fprintf(&spec, "- **SC-%03d**: Outcome %d holds.\n", i, i)
	}
	points, omitted := KeyPoints([]byte(spec.String()), []byte("### Simplicity\n\nStart simple.\n"))
	if len(points) != MaxKeyPoints || points[44].Key != "fr-045" || points[49].Key != "sc-005" ||
		len(omitted) != 6 || omitted[0].Key != "sc-006" || omitted[5] != (Omission{protocol.KeyPointConstraint, "constitution-simplicity"}) {
		t.Fatalf("bounded key points = %d, omitted %v", len(points), omitted)
	}

	root := t.TempDir()
	dir := filepath.Join(root, "specs", "001-many")
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	var tasks strings.Builder
	for i := 1; i <= MaxPaths+5; i++ {
		fmt.Fprintf(&tasks, "- [ ] T%03d Add src/file%d.ts\n", i, i)
	}
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(name, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(dir, SpecFile), spec.String())
	write(filepath.Join(dir, TasksFile), tasks.String())
	feature, err := Resolve(root, dir)
	if err != nil {
		t.Fatal(err)
	}
	extraction, err := PlannedPaths(feature)
	if err != nil || len(extraction.Paths) != MaxPaths || !extraction.Truncated || extraction.Paths[0] != "src/file1.ts" {
		t.Fatalf("bounded extraction = %d paths, truncated %v, %v", len(extraction.Paths), extraction.Truncated, err)
	}

	write(filepath.Join(dir, TasksFile), strings.Repeat("x", int(MaxFileBytes)+1))
	if _, err := PlannedPaths(feature); err == nil || !strings.Contains(err.Error(), "1 MiB") {
		t.Fatalf("an oversized tasks.md = %v", err)
	}
	if err := os.Remove(filepath.Join(dir, TasksFile)); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, SpecFile), filepath.Join(dir, TasksFile)); err == nil {
		if _, err := PlannedPaths(feature); err == nil || !strings.Contains(err.Error(), "symbolic link") {
			t.Fatalf("a symbolic-link tasks.md = %v", err)
		}
	} else {
		t.Logf("symbolic links are unavailable here: %v", err)
	}

	outside := t.TempDir()
	write(filepath.Join(outside, SpecFile), "# Outside\n")
	if _, err := Resolve(root, outside); err == nil {
		t.Fatal("a feature outside the repository was accepted")
	}
	if _, err := Resolve(root, filepath.Join(root, "src")); !errors.Is(err, ErrNotFeature) {
		t.Fatalf("a directory without spec.md = %v", err)
	}
}

// TestCurrentFeature protects the feature resolution order: the
// SPECIFY_FEATURE_DIRECTORY override, then .specify/feature.json, then the
// directory named after the Git branch.
func TestCurrentFeature(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"001-a", "002-b", "003-c"} {
		if err := os.MkdirAll(filepath.Join(root, "specs", name), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "specs", name, SpecFile), []byte("# x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	none := func(string) (string, bool) { return "", false }
	override := func(name string) (string, bool) { return "specs/001-a", name == FeatureDirectoryEnv }
	if got, err := CurrentFeature(root, none, "003-c"); err != nil || got != filepath.Join(root, "specs", "003-c") {
		t.Fatalf("branch feature = %q, %v", got, err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".specify"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".specify", "feature.json"), []byte(`{"feature_directory": "specs/002-b"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := CurrentFeature(root, none, "003-c"); err != nil || got != filepath.Join(root, "specs", "002-b") {
		t.Fatalf("recorded feature = %q, %v", got, err)
	}
	if got, err := CurrentFeature(root, override, "003-c"); err != nil || got != filepath.Join(root, "specs", "001-a") {
		t.Fatalf("overridden feature = %q, %v", got, err)
	}
	if got, err := CurrentFeature(t.TempDir(), none, "main"); err != nil || got != "" {
		t.Fatalf("no feature = %q, %v", got, err)
	}
}
