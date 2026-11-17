package contracts_test

import (
	"encoding/json"
	"errors"
	"io/fs"
	"testing"

	canonical "github.com/Juanqu261/conn/contracts"
	"github.com/Juanqu261/conn/internal/contracts"
)

// TestSharedFixtures protects cross-language schema interpretation: the Go
// validator must accept and reject exactly the documents the Node.js
// validator does, and report each rejection at the fixture's location.
func TestSharedFixtures(t *testing.T) {
	registry, err := contracts.Load(canonical.V1, "v1/*.schema.json")
	if err != nil {
		t.Fatal(err)
	}

	raw, err := fs.ReadFile(canonical.V1, "v1/fixtures/cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var table struct {
		Cases []struct {
			Name     string          `json:"name"`
			Schema   string          `json:"schema"`
			Valid    bool            `json:"valid"`
			At       *string         `json:"at"`
			Document json.RawMessage `json:"document"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &table); err != nil {
		t.Fatal(err)
	}
	if len(table.Cases) == 0 {
		t.Fatal("fixture table is empty")
	}

	for _, tc := range table.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			err := registry.ValidateJSON(tc.Schema, tc.Document)
			if tc.Valid {
				if err != nil {
					t.Fatalf("expected valid document: %v", err)
				}
				return
			}

			if tc.At == nil {
				t.Fatal("rejected fixture has no expected location")
			}
			var validationErr *contracts.ValidationError
			if !errors.As(err, &validationErr) {
				t.Fatalf("expected a validation error, got %v", err)
			}
			for _, violation := range validationErr.Violations {
				if violation.InstancePath == *tc.At {
					return
				}
			}
			t.Fatalf("no violation at %q: %+v", *tc.At, validationErr.Violations)
		})
	}
}
