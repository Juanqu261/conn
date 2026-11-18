package credentials_test

import (
	"errors"
	"testing"

	"github.com/Juanqu261/conn/internal/credentials"
	"github.com/Juanqu261/conn/internal/secret"
)

const (
	envToken    = "conn_sess_EEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEE"
	storedToken = "conn_sess_SSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSS"
)

// TestResolve protects decisions 0019 and 0020: CONN_TOKEN takes precedence
// and is never persisted, the stored token is used otherwise, deletion
// removes it, and an unavailable store is reported rather than bypassed.
func TestResolve(t *testing.T) {
	cases := []struct {
		name       string
		env        string
		stored     string
		storeErr   error
		wantToken  string
		wantSource credentials.Source
		wantErr    error
	}{
		{name: "environment wins over the store", env: envToken, stored: storedToken, wantToken: envToken, wantSource: credentials.SourceEnv},
		{name: "environment alone is not persisted", env: envToken, wantToken: envToken, wantSource: credentials.SourceEnv},
		{name: "store is used without the environment", stored: storedToken, wantToken: storedToken, wantSource: credentials.SourceStore},
		{name: "nothing configured", wantErr: credentials.ErrNotLoggedIn},
		{name: "malformed environment token", env: "gho_not_conn", stored: storedToken, wantErr: credentials.ErrInvalid},
		{name: "unavailable store has no fallback", storeErr: errors.New("secret service not running"), wantErr: credentials.ErrUnavailable},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &credentials.Memory{}
			if tc.stored != "" {
				if err := store.Set(secret.New(tc.stored)); err != nil {
					t.Fatal(err)
				}
			}
			store.Err = tc.storeErr
			lookupEnv := func(key string) (string, bool) {
				if key == credentials.TokenEnv && tc.env != "" {
					return tc.env, true
				}
				return "", false
			}

			token, source, err := credentials.Resolve(lookupEnv, store)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("got error %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr != nil {
				return
			}
			if token.Reveal() != tc.wantToken || source != tc.wantSource {
				t.Fatalf("got %s from %q", token, source)
			}

			// Resolving never writes the environment token to the store.
			got, err := store.Get()
			if tc.stored == "" && !errors.Is(err, credentials.ErrNotFound) {
				t.Fatalf("store gained a credential: %v", err)
			}
			if tc.stored != "" && got.Reveal() != tc.stored {
				t.Fatal("store contents changed")
			}
		})
	}

	t.Run("delete removes the stored token", func(t *testing.T) {
		store := &credentials.Memory{}
		_ = store.Set(secret.New(storedToken))
		if err := store.Delete(); err != nil {
			t.Fatal(err)
		}
		if _, _, err := credentials.Resolve(func(string) (string, bool) { return "", false }, store); !errors.Is(err, credentials.ErrNotLoggedIn) {
			t.Fatalf("got %v, want ErrNotLoggedIn", err)
		}
	})
}
