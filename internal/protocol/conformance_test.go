package protocol_test

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"reflect"
	"testing"

	canonical "github.com/Juanqu261/conn/contracts"
	"github.com/Juanqu261/conn/internal/protocol"
)

// TestConformance holds the handwritten Go types to the canonical schemas
// (decision 0033). Every valid shared fixture must decode into its Go type
// without unknown fields and encode back to an equal, schema-valid document.
// Every invalid fixture must fail decoding.
func TestConformance(t *testing.T) {
	roundTrips := map[string]func([]byte) ([]byte, error){
		protocol.ProjectSchema:                roundTrip[protocol.Project],
		protocol.ProjectListSchema:            roundTrip[protocol.ProjectList],
		protocol.ProjectCreateSchema:          roundTrip[protocol.ProjectCreate],
		protocol.ProjectMembershipSchema:      roundTrip[protocol.ProjectMembership],
		protocol.ProjectMemberListSchema:      roundTrip[protocol.ProjectMemberList],
		protocol.ObjectiveSchema:              roundTrip[protocol.Objective],
		protocol.ObjectiveRevisionInputSchema: roundTrip[protocol.ObjectiveRevisionInput],
		protocol.BlueprintRevisionSchema:      roundTrip[protocol.BlueprintRevision],
		protocol.BlueprintRevisionViewSchema:  roundTrip[protocol.BlueprintRevisionView],
		protocol.BlueprintProposalSchema:      roundTrip[protocol.BlueprintProposal],
		protocol.CharterDecisionSchema:        roundTrip[protocol.CharterDecision],
		protocol.CharterRejectionSchema:       roundTrip[protocol.CharterRejection],
		protocol.ProjectMemberAddSchema:       roundTrip[protocol.ProjectMemberAdd],
		protocol.ProjectStatusSchema:          roundTrip[protocol.ProjectStatus],
		protocol.CharterViewSchema:            roundTrip[protocol.CharterView],
		protocol.EventEnvelopeSchema:          roundTrip[protocol.EventEnvelope],
		protocol.RepositoryConnectionSchema:   roundTrip[protocol.RepositoryConnection],
		protocol.ParticipantSchema:            roundTrip[protocol.Participant],
		protocol.IntentInputSchema:            roundTrip[protocol.IntentInput],
		protocol.IntentRevisionSchema:         roundTrip[protocol.IntentRevision],
		protocol.SemanticClaimCandidateSchema: roundTrip[protocol.SemanticClaimCandidate],
		protocol.SemanticClaimSchema:          roundTrip[protocol.SemanticClaim],
		protocol.LocalAnalyzerReportSchema:    roundTrip[protocol.LocalAnalyzerReport],
		protocol.CollisionAssessmentSchema:    roundTrip[protocol.CollisionAssessment],
		protocol.ConflictContextPackSchema:    roundTrip[protocol.ConflictContextPack],
		protocol.ResolutionProposalSchema:     roundTrip[protocol.ResolutionProposal],
		protocol.ResolutionDecisionSchema:     roundTrip[protocol.ResolutionDecision],
		protocol.EvidenceReferenceSchema:      roundTrip[protocol.EvidenceReference],
		protocol.ObjectiveImportInputSchema:   roundTrip[protocol.ObjectiveImportInput],
		protocol.RadarViewSchema:              roundTrip[protocol.RadarView],
		protocol.HumanSchema:                  roundTrip[protocol.Human],
		protocol.SessionGrantSchema:           roundTrip[protocol.SessionGrant],
		protocol.GitHubLoginSchema:            roundTrip[protocol.GitHubLogin],
		protocol.ErrorSchema:                  roundTrip[protocol.Error],
	}

	for _, tc := range loadFixtures(t) {
		t.Run(tc.Name, func(t *testing.T) {
			run, ok := roundTrips[tc.Schema]
			if !ok {
				t.Fatalf("no Go type mirrors schema %s", tc.Schema)
			}
			encoded, err := run(tc.Document)
			if !tc.Valid {
				if err == nil {
					t.Fatal("expected decoding to fail")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !jsonEqual(t, tc.Document, encoded) {
				t.Fatalf("round trip changed the document\nwant %s\ngot  %s", tc.Document, encoded)
			}
		})
	}

	// The schema checks timestamp shape only; decoding also rejects
	// impossible calendar dates.
	t.Run("decoding rejects impossible calendar dates", func(t *testing.T) {
		_, err := protocol.Decode[protocol.Project]([]byte(`{
			"project_id": "019a8f00-0001-7000-8000-000000000001",
			"name": "Checkout reliability",
			"owner_human_id": "019a8f00-0000-7000-8000-00000000000a",
			"created_at": "2026-02-30T09:00:00Z"
		}`))
		if err == nil {
			t.Fatal("expected decoding to fail")
		}
	})
}

func roundTrip[T protocol.Document](data []byte) ([]byte, error) {
	doc, err := protocol.Decode[T](data)
	if err != nil {
		return nil, err
	}
	return protocol.Encode(doc)
}

type fixture struct {
	Name     string          `json:"name"`
	Schema   string          `json:"schema"`
	Valid    bool            `json:"valid"`
	Document json.RawMessage `json:"document"`
}

func loadFixtures(t *testing.T) []fixture {
	t.Helper()
	raw, err := fs.ReadFile(canonical.V1, "v1/fixtures/cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var table struct {
		Cases []fixture `json:"cases"`
	}
	if err := json.Unmarshal(raw, &table); err != nil {
		t.Fatal(err)
	}
	if len(table.Cases) == 0 {
		t.Fatal("fixture table is empty")
	}
	return table.Cases
}

func jsonEqual(t *testing.T, a, b []byte) bool {
	t.Helper()
	return reflect.DeepEqual(parseJSON(t, a), parseJSON(t, b))
}

func parseJSON(t *testing.T, data []byte) any {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		t.Fatal(err)
	}
	return value
}
