// Package github runs GitHub's OAuth device flow from the CLI and verifies
// GitHub tokens on the backend. Tokens stay in
// secret.String values and never appear in errors.
package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Juanqu261/conn/internal/application"
	"github.com/Juanqu261/conn/internal/secret"
)

// Endpoints locates GitHub. Tests point it at a fake server.
type Endpoints struct {
	Web string // device and token endpoints
	API string // REST API
}

// Production is github.com.
var Production = Endpoints{Web: "https://github.com", API: "https://api.github.com"}

var (
	ErrAccessDenied       = errors.New("GitHub authorization was denied")
	ErrExpired            = errors.New("GitHub device code expired before authorization")
	ErrDeviceFlowDisabled = errors.New("device flow is not enabled for this GitHub OAuth app")
	ErrTokenRejected      = errors.New("GitHub rejected the access token")
)

const (
	maxResponseBytes = 64 << 10
	defaultInterval  = 5 * time.Second
	slowDownStep     = 5 * time.Second
	// GitHub issues 15-minute codes. A longer value from a misbehaving
	// server must not keep the CLI polling indefinitely.
	maxCodeLifetime = 30 * time.Minute
)

// DeviceAuthorization is what the human needs to authorize this device.
type DeviceAuthorization struct {
	UserCode        string
	VerificationURI string
	ExpiresAt       time.Time

	deviceCode secret.String
	interval   time.Duration
}

// DeviceFlow requests and polls a device authorization. It requests no
// OAuth scope: Conn needs only the public identity of the user.
type DeviceFlow struct {
	HTTP      *http.Client
	Endpoints Endpoints
	ClientID  string
	// Now and Sleep default to the real clock. Tests replace them.
	Now   func() time.Time
	Sleep func(ctx context.Context, d time.Duration) error
}

// Start requests a device and user code.
func (f *DeviceFlow) Start(ctx context.Context) (*DeviceAuthorization, error) {
	var body struct {
		DeviceCode      string `json:"device_code"`
		UserCode        string `json:"user_code"`
		VerificationURI string `json:"verification_uri"`
		ExpiresIn       int    `json:"expires_in"`
		Interval        int    `json:"interval"`
		Error           string `json:"error"`
	}
	if err := f.post(ctx, "/login/device/code", url.Values{"client_id": {f.ClientID}}, &body); err != nil {
		return nil, err
	}
	switch {
	case body.Error == "device_flow_disabled":
		return nil, ErrDeviceFlowDisabled
	case body.Error != "":
		return nil, fmt.Errorf("GitHub device authorization failed: %s", safeCode(body.Error))
	case body.DeviceCode == "" || body.UserCode == "" || body.VerificationURI == "" || body.ExpiresIn <= 0:
		return nil, errors.New("GitHub returned an incomplete device authorization")
	}

	lifetime := min(time.Duration(body.ExpiresIn)*time.Second, maxCodeLifetime)
	interval := time.Duration(body.Interval) * time.Second
	if interval <= 0 {
		interval = defaultInterval
	}
	return &DeviceAuthorization{
		UserCode:        body.UserCode,
		VerificationURI: body.VerificationURI,
		ExpiresAt:       f.now().Add(lifetime),
		deviceCode:      secret.New(body.DeviceCode),
		interval:        interval,
	}, nil
}

// Wait polls at the interval GitHub requires until the human authorizes,
// denies, or the code expires. The returned token must stay in memory.
func (f *DeviceFlow) Wait(ctx context.Context, auth *DeviceAuthorization) (secret.String, error) {
	interval := auth.interval
	for {
		if !f.now().Add(interval).Before(auth.ExpiresAt) {
			return secret.String{}, ErrExpired
		}
		if err := f.sleep(ctx, interval); err != nil {
			return secret.String{}, err
		}

		var body struct {
			AccessToken string `json:"access_token"`
			Error       string `json:"error"`
			Interval    int    `json:"interval"`
		}
		form := url.Values{
			"client_id":   {f.ClientID},
			"device_code": {auth.deviceCode.Reveal()},
			"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
		}
		if err := f.post(ctx, "/login/oauth/access_token", form, &body); err != nil {
			return secret.String{}, err
		}

		switch body.Error {
		case "":
			if body.AccessToken == "" {
				return secret.String{}, errors.New("GitHub returned no access token")
			}
			return secret.New(body.AccessToken), nil
		case "authorization_pending":
		case "slow_down":
			if body.Interval > 0 {
				interval = time.Duration(body.Interval) * time.Second
			} else {
				interval += slowDownStep
			}
		case "expired_token":
			return secret.String{}, ErrExpired
		case "access_denied":
			return secret.String{}, ErrAccessDenied
		case "device_flow_disabled":
			return secret.String{}, ErrDeviceFlowDisabled
		default:
			return secret.String{}, fmt.Errorf("GitHub device authorization failed: %s", safeCode(body.Error))
		}
	}
}

func (f *DeviceFlow) post(ctx context.Context, path string, form url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, f.Endpoints.Web+path, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := f.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("reach GitHub: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GitHub returned HTTP %d", resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(out); err != nil {
		return errors.New("GitHub returned an unreadable response")
	}
	return nil
}

func (f *DeviceFlow) now() time.Time {
	if f.Now != nil {
		return f.Now()
	}
	return time.Now()
}

func (f *DeviceFlow) sleep(ctx context.Context, d time.Duration) error {
	if f.Sleep != nil {
		return f.Sleep(ctx, d)
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// Identity is the immutable GitHub user ID and the current display login.
type Identity struct {
	UserID int64
	Login  string
}

// API verifies GitHub tokens on the backend.
type API struct {
	HTTP      *http.Client
	Endpoints Endpoints
}

// User returns the identity that owns token. The token is not retained.
func (a *API) User(ctx context.Context, token secret.String) (Identity, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.Endpoints.API+"/user", nil)
	if err != nil {
		return Identity{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token.Reveal())
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := a.HTTP.Do(req)
	if err != nil {
		return Identity{}, fmt.Errorf("reach GitHub: %w", err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return Identity{}, ErrTokenRejected
	case resp.StatusCode != http.StatusOK:
		return Identity{}, fmt.Errorf("GitHub returned HTTP %d", resp.StatusCode)
	}
	var user struct {
		ID    int64  `json:"id"`
		Login string `json:"login"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&user); err != nil {
		return Identity{}, errors.New("GitHub returned an unreadable user")
	}
	if user.ID <= 0 || user.Login == "" {
		return Identity{}, errors.New("GitHub returned an incomplete user")
	}
	return Identity{UserID: user.ID, Login: user.Login}, nil
}

// Verify adapts User to the backend's token verifier, mapping failures to
// application errors.
func (a *API) Verify(ctx context.Context, token secret.String) (application.GitHubIdentity, error) {
	identity, err := a.User(ctx, token)
	switch {
	case errors.Is(err, ErrTokenRejected):
		return application.GitHubIdentity{}, application.ErrGitHubTokenRejected
	case err != nil:
		return application.GitHubIdentity{}, fmt.Errorf("%w: %v", application.ErrGitHubUnavailable, err)
	}
	return application.GitHubIdentity{UserID: identity.UserID, Login: identity.Login}, nil
}

// safeCode keeps an error code from a remote server short and printable.
func safeCode(code string) string {
	if len(code) > 64 {
		code = code[:64]
	}
	return strings.Map(func(r rune) rune {
		if r == '_' || r == '-' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			return r
		}
		return '?'
	}, code)
}
