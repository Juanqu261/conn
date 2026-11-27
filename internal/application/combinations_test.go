package application

import (
	"testing"

	"github.com/Juanqu261/conn/internal/protocol"
)

// TestCombinationOutcomeFollowsItsFacts protects requirement 1.7 of
// verification-ci-and-adoption at the server, where any member may submit a
// combination check. It reproduces a defect: a record whose own facts showed
// a timed-out check, a failed setup, or diagnostics that never ran was
// accepted as passing, although conn verify-pair labels each incomplete.
func TestCombinationOutcomeFollowsItsFacts(t *testing.T) {
	clean := func(outcome string, change func(*protocol.CombinationCheckInput)) protocol.CombinationCheckInput {
		input := protocol.CombinationCheckInput{
			MergeResult: protocol.MergeClean, ConflictedPaths: []string{}, Outcome: outcome,
			Diagnostics: protocol.CombinationDiagnostics{Ran: true, New: []protocol.CombinationDiagnostic{}},
			Checks:      []protocol.CombinationCheckRun{{Key: "test", State: protocol.EvidencePassing}},
		}
		if change != nil {
			change(&input)
		}
		return input
	}
	timedOut := func(input *protocol.CombinationCheckInput) {
		input.Checks[0] = protocol.CombinationCheckRun{Key: "test", ExitCode: -1, TimedOut: true, State: protocol.CombinationCheckTimedOut}
	}
	setupFailed := func(input *protocol.CombinationCheckInput) {
		input.Setup, input.Checks = &protocol.CombinationRun{ExitCode: 1}, []protocol.CombinationCheckRun{}
	}
	notRun := func(input *protocol.CombinationCheckInput) { input.Diagnostics.Ran = false }
	newDiagnostic := func(input *protocol.CombinationCheckInput) {
		input.Diagnostics.NewCount, input.Diagnostics.New = 1, []protocol.CombinationDiagnostic{{Code: "TS2554"}}
	}
	for _, tc := range []struct {
		name  string
		input protocol.CombinationCheckInput
		valid bool
	}{
		{"everything ran and passed", clean(protocol.CombinationPassing, nil), true},
		{"passing with a timed-out check", clean(protocol.CombinationPassing, timedOut), false},
		{"incomplete with a timed-out check", clean(protocol.CombinationIncomplete, timedOut), true},
		{"passing after a failed setup", clean(protocol.CombinationPassing, setupFailed), false},
		{"incomplete after a failed setup", clean(protocol.CombinationIncomplete, setupFailed), true},
		{"checks after a failed setup", clean(protocol.CombinationIncomplete, func(input *protocol.CombinationCheckInput) {
			input.Setup = &protocol.CombinationRun{ExitCode: 1}
		}), false},
		{"passing without diagnostics", clean(protocol.CombinationPassing, notRun), false},
		{"incomplete without diagnostics", clean(protocol.CombinationIncomplete, notRun), true},
		{"incomplete with a new diagnostic", clean(protocol.CombinationIncomplete, newDiagnostic), false},
		{"failing with a new diagnostic", clean(protocol.CombinationFailing, newDiagnostic), true},
		{"failing without any failure", clean(protocol.CombinationFailing, nil), false},
	} {
		if err := validateCombinationOutcome(tc.input); (err == nil) != tc.valid {
			t.Errorf("%s: valid=%v, error %v", tc.name, tc.valid, err)
		}
	}
}
