package secret_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/Juanqu261/conn/internal/secret"
)

// TestRedaction protects decisions 0019 and 0021: a credential never
// appears in formatted output, structured logs, or wrapped errors.
func TestRedaction(t *testing.T) {
	const raw = "conn_sess_do-not-print-me"
	token := secret.New(raw)
	holder := struct{ Token secret.String }{token}

	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	logger.Info("login", "token", token, "holder", holder)
	slog.New(slog.NewTextHandler(&logs, nil)).Info("login", "token", token)
	encoded, err := json.Marshal(holder)
	if err != nil {
		t.Fatal(err)
	}

	outputs := map[string]string{
		"%v":            fmt.Sprintf("%v", token),
		"%s":            fmt.Sprintf("%s", token),
		"%q":            fmt.Sprintf("%q", token),
		"%#v":           fmt.Sprintf("%#v", token),
		"%+v of holder": fmt.Sprintf("%+v", holder),
		"%#v of holder": fmt.Sprintf("%#v", holder),
		"wrapped error": fmt.Errorf("exchange %v: %w", token, errors.New("denied")).Error(),
		"slog":          logs.String(),
		"json":          string(encoded),
	}
	for name, output := range outputs {
		if strings.Contains(output, raw) {
			t.Errorf("%s leaked the credential: %s", name, output)
		}
	}
	if token.Reveal() != raw {
		t.Fatal("Reveal must return the credential")
	}
}
