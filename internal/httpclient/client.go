// Package httpclient is the backend client shared by CLI commands and the
// MCP adapter. It refuses plain HTTP except to loopback development servers,
// bounds every request and response, and never follows redirects, so a
// credential is only ever sent to the configured server.
package httpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Juanqu261/conn/internal/protocol"
	"github.com/Juanqu261/conn/internal/secret"
)

const (
	// DefaultServerURL is the local development server.
	DefaultServerURL = "http://127.0.0.1:8080"
	// maxResponseBytes fits the largest BlueprintRevision view, which the
	// server accepts as a request of up to 1 MiB, with room for its envelope.
	maxResponseBytes = 2 << 20
)

// APIError is an error response from the Conn server.
type APIError struct {
	Status  int
	Code    string
	Message string
	// Pointer locates the offending request field of an invalid_input error.
	Pointer string
}

func (e *APIError) Error() string {
	if e.Pointer != "" {
		return e.Message + " (at " + e.Pointer + ")"
	}
	return e.Message
}

// IsCode reports whether err is an APIError with code.
func IsCode(err error, code string) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.Code == code
}

// ErrUnreachable means the server could not be contacted.
var ErrUnreachable = errors.New("the Conn server could not be reached")

// NewHTTPClient returns an http.Client with Conn's timeouts that never
// follows redirects.
func NewHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// Client calls one Conn server, optionally with a session token.
type Client struct {
	base    *url.URL
	http    *http.Client
	token   secret.String
	adapter string
}

// New validates serverURL and returns a client for it.
func New(serverURL string, httpClient *http.Client) (*Client, error) {
	base, err := ParseServerURL(serverURL)
	if err != nil {
		return nil, err
	}
	return &Client{base: base, http: httpClient}, nil
}

// ParseServerURL accepts https URLs, and http URLs only for loopback hosts.
// A value that still holds a ${VAR} placeholder means the MCP client did not
// substitute the variable, so the error names it.
func ParseServerURL(raw string) (*url.URL, error) {
	if strings.Contains(raw, "${") {
		return nil, fmt.Errorf("Conn server URL %q is an unsubstituted placeholder; set CONN_SERVER_URL to your Conn backend, or leave it unset to use %s", raw, DefaultServerURL)
	}
	u, err := url.Parse(strings.TrimRight(raw, "/"))
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("invalid Conn server URL %q", raw)
	}
	switch u.Scheme {
	case "https":
	case "http":
		if !isLoopback(u.Hostname()) {
			return nil, fmt.Errorf("Conn server URL %q must use https; plain http is allowed only for loopback development servers", raw)
		}
	default:
		return nil, fmt.Errorf("Conn server URL %q must use https", raw)
	}
	return u, nil
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// URL returns the server URL in canonical form.
func (c *Client) URL() string { return c.base.String() }

// WithToken returns a client that authenticates with token.
func (c *Client) WithToken(token secret.String) *Client {
	clone := *c
	clone.token = token
	return &clone
}

// WithAdapter identifies a bounded adapter participant for participant-owned
// operations. It does not carry human authority, which always comes from the
// bearer session. An empty adapter retains the CLI default.
func (c *Client) WithAdapter(adapter string) *Client {
	clone := *c
	clone.adapter = adapter
	return &clone
}

// GitHubLogin returns the server's GitHub OAuth client configuration.
func (c *Client) GitHubLogin(ctx context.Context) (protocol.GitHubLogin, error) {
	return get[protocol.GitHubLogin](ctx, c, "/v1/auth/github/client")
}

// Exchange trades a GitHub access token for a Conn session. The GitHub
// token is sent once, as a bearer credential.
func (c *Client) Exchange(ctx context.Context, githubToken secret.String) (protocol.SessionGrant, error) {
	body, err := c.do(ctx, http.MethodPost, "/v1/auth/github/exchange", githubToken)
	if err != nil {
		return protocol.SessionGrant{}, err
	}
	return decodeData[protocol.SessionGrant](body)
}

// Me returns the authenticated human.
func (c *Client) Me(ctx context.Context) (protocol.Human, error) {
	return get[protocol.Human](ctx, c, "/v1/humans/me")
}

// Logout revokes the current session on the server.
func (c *Client) Logout(ctx context.Context) error {
	_, err := c.do(ctx, http.MethodDelete, "/v1/session", c.token)
	return err
}

// Probe reports whether path answers 200. It is used for /healthz and
// /readyz diagnostics.
func (c *Client) Probe(ctx context.Context, path string) error {
	_, err := c.do(ctx, http.MethodGet, path, secret.String{})
	return err
}

func get[T protocol.Document](ctx context.Context, c *Client, path string) (T, error) {
	body, err := c.do(ctx, http.MethodGet, path, c.token)
	if err != nil {
		var zero T
		return zero, err
	}
	return decodeData[T](body)
}

// do performs one bodiless request and returns the body of a 2xx response.
func (c *Client) do(ctx context.Context, method, path string, credential secret.String) ([]byte, error) {
	return c.send(ctx, method, path, credential, nil)
}

// send performs one request with an optional JSON body and returns the
// body of a 2xx response.
func (c *Client) send(ctx context.Context, method, path string, credential secret.String, body []byte) ([]byte, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base.String()+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if !credential.IsZero() {
		req.Header.Set("Authorization", "Bearer "+credential.Reveal())
	}
	if c.adapter != "" {
		req.Header.Set("X-Conn-Participant-Adapter", c.adapter)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("%w at %s", ErrUnreachable, c.base)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%w at %s", ErrUnreachable, c.base)
	}
	if len(respBody) > maxResponseBytes {
		return nil, errors.New("the Conn server response is too large")
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return respBody, nil
	}
	return nil, decodeError(resp.StatusCode, respBody)
}

func decodeData[T protocol.Document](body []byte) (T, error) {
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || len(envelope.Data) == 0 {
		var zero T
		return zero, errors.New("the Conn server returned an unreadable response")
	}
	doc, err := protocol.Decode[T](envelope.Data)
	if err != nil {
		return doc, fmt.Errorf("the Conn server returned an invalid %s: %w", doc.SchemaID(), err)
	}
	return doc, nil
}

func decodeError(status int, body []byte) error {
	if doc, err := protocol.Decode[protocol.Error](bytes.TrimSpace(body)); err == nil {
		return &APIError{Status: status, Code: doc.Error.Code, Message: doc.Error.Message, Pointer: doc.Error.Pointer}
	}
	return &APIError{Status: status, Code: fmt.Sprintf("http_%d", status), Message: fmt.Sprintf("The Conn server returned HTTP %d.", status)}
}
