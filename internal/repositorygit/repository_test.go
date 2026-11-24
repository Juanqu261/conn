package repositorygit

import "testing"

func TestNormalizeRemote(t *testing.T) {
	cases := []struct {
		name, raw, wantHost, wantRemote string
		wantErr                         error
	}{
		{"HTTPS GitHub", "https://github.com/acme/payments.git", "github", "github.com/acme/payments", nil},
		{"SSH GitHub", "git@github.com:acme/payments.git", "github", "github.com/acme/payments", nil},
		{"credential is removed", "https://token@example.test/acme/payments.git", "generic_git", "example.test/acme/payments", nil},
		{"missing origin", " ", "", "", ErrNoOrigin},
		{"port is not canonical", "https://git.example.test:8443/acme/payments.git", "", "", ErrUnsupportedRemote},
		{"path traversal is rejected", "https://github.com/acme/../payments.git", "", "", ErrUnsupportedRemote},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NormalizeRemote(tc.raw)
			if err != tc.wantErr {
				t.Fatalf("NormalizeRemote error = %v, want %v", err, tc.wantErr)
			}
			if err == nil && (got.HostKind != tc.wantHost || got.CanonicalRemote != tc.wantRemote) {
				t.Fatalf("NormalizeRemote = %#v, want host %q remote %q", got, tc.wantHost, tc.wantRemote)
			}
		})
	}
}
