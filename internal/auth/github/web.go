package github

import (
	"context"
	"errors"
	"fmt"
	"net/url"

	"github.com/Juanqu261/conn/internal/secret"
)

// ErrCodeRejected means GitHub refused an authorization code: it expired,
// was already used, or does not match the redirect URI or PKCE verifier.
var ErrCodeRejected = errors.New("GitHub rejected the authorization code")

// WebFlow runs GitHub's OAuth web authorization-code flow with PKCE for
// Conn Lab (decision 0013) on the same OAuth client as the device flow. It
// requests no OAuth scope: Conn needs only the public identity of the user.
// The client secret stays on the server and never appears in errors.
type WebFlow struct {
	Device       DeviceFlow // HTTP, Endpoints, and ClientID are shared
	ClientSecret secret.String
}

// AuthorizeURL is where the browser starts authorization. state and
// codeChallenge (base64url SHA-256 of the verifier) bind the callback to
// this browser.
func (f *WebFlow) AuthorizeURL(state, codeChallenge, redirectURI string) string {
	query := url.Values{
		"client_id":             {f.Device.ClientID},
		"redirect_uri":          {redirectURI},
		"state":                 {state},
		"code_challenge":        {codeChallenge},
		"code_challenge_method": {"S256"},
		"allow_signup":          {"false"},
	}
	return f.Device.Endpoints.Web + "/login/oauth/authorize?" + query.Encode()
}

// ExchangeCode trades an authorization code for a GitHub access token. The
// token must stay in memory and be discarded after identity exchange.
func (f *WebFlow) ExchangeCode(ctx context.Context, code string, verifier secret.String, redirectURI string) (secret.String, error) {
	var body struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	form := url.Values{
		"client_id":     {f.Device.ClientID},
		"client_secret": {f.ClientSecret.Reveal()},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"code_verifier": {verifier.Reveal()},
	}
	if err := f.Device.post(ctx, "/login/oauth/access_token", form, &body); err != nil {
		return secret.String{}, err
	}
	switch body.Error {
	case "":
		if body.AccessToken == "" {
			return secret.String{}, errors.New("GitHub returned no access token")
		}
		return secret.New(body.AccessToken), nil
	case "bad_verification_code", "invalid_grant", "redirect_uri_mismatch":
		return secret.String{}, ErrCodeRejected
	default:
		return secret.String{}, fmt.Errorf("GitHub web authorization failed: %s", safeCode(body.Error))
	}
}
