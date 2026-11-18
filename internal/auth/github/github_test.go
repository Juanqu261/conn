package github_test

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/Juanqu261/conn/internal/auth/github"
	"github.com/Juanqu261/conn/internal/auth/github/githubtest"
	"github.com/Juanqu261/conn/internal/secret"
)

// TestDeviceFlow protects decision 0032: the CLI honors GitHub's polling
// interval and slow-down responses, stops on denial and expiry, and returns
// the token only after authorization.
func TestDeviceFlow(t *testing.T) {
	cases := []struct {
		name       string
		polls      []githubtest.Poll
		expiresIn  int
		want       error
		wantSleeps []time.Duration
	}{
		{
			name:       "pending, then authorized",
			polls:      []githubtest.Poll{{Error: "authorization_pending"}},
			wantSleeps: []time.Duration{5 * time.Second, 5 * time.Second},
		},
		{
			name:       "slow down raises the interval",
			polls:      []githubtest.Poll{{Error: "slow_down", Interval: 10}, {Error: "authorization_pending"}},
			wantSleeps: []time.Duration{5 * time.Second, 10 * time.Second, 10 * time.Second},
		},
		{
			name:       "human denies",
			polls:      []githubtest.Poll{{Error: "authorization_pending"}, {Error: "access_denied"}},
			want:       github.ErrAccessDenied,
			wantSleeps: []time.Duration{5 * time.Second, 5 * time.Second},
		},
		{
			name:       "GitHub reports expiry",
			polls:      []githubtest.Poll{{Error: "expired_token"}},
			want:       github.ErrExpired,
			wantSleeps: []time.Duration{5 * time.Second},
		},
		{
			name:       "code expires locally without another poll",
			polls:      []githubtest.Poll{{Error: "authorization_pending"}, {Error: "authorization_pending"}},
			expiresIn:  12,
			want:       github.ErrExpired,
			wantSleeps: []time.Duration{5 * time.Second, 5 * time.Second},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := &githubtest.Server{ClientID: "client-1", Polls: tc.polls, ExpiresIn: tc.expiresIn, Token: "gho_secret"}
			endpoints := fake.Start(t)

			now := time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)
			var sleeps []time.Duration
			flow := &github.DeviceFlow{
				HTTP:      http.DefaultClient,
				Endpoints: endpoints,
				ClientID:  "client-1",
				Now:       func() time.Time { return now },
				Sleep: func(_ context.Context, d time.Duration) error {
					sleeps = append(sleeps, d)
					now = now.Add(d)
					return nil
				},
			}

			auth, err := flow.Start(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if auth.UserCode != "WDJB-MJHT" || auth.VerificationURI == "" {
				t.Fatalf("unexpected authorization: %+v", auth)
			}
			token, err := flow.Wait(context.Background(), auth)

			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
			if tc.want == nil && token.Reveal() != "gho_secret" {
				t.Fatal("expected the issued token")
			}
			if !slices.Equal(sleeps, tc.wantSleeps) {
				t.Fatalf("slept %v, want %v", sleeps, tc.wantSleeps)
			}
		})
	}
}

// TestUser protects identity verification: the backend learns the
// immutable user ID from a valid token and refuses an invalid one.
func TestUser(t *testing.T) {
	fake := &githubtest.Server{Users: map[string]github.Identity{"gho_valid": {UserID: 583231, Login: "octocat"}}}
	api := &github.API{HTTP: http.DefaultClient, Endpoints: fake.Start(t)}

	identity, err := api.User(context.Background(), secret.New("gho_valid"))
	if err != nil || identity != (github.Identity{UserID: 583231, Login: "octocat"}) {
		t.Fatalf("got %+v, %v", identity, err)
	}
	if _, err := api.User(context.Background(), secret.New("gho_other")); !errors.Is(err, github.ErrTokenRejected) {
		t.Fatalf("got %v, want ErrTokenRejected", err)
	}
}
