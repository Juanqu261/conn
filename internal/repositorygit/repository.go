// Package repositorygit derives credential-free repository identity from a
// local Git worktree. It never returns the raw origin URL.
package repositorygit

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

var (
	ErrNoOrigin          = errors.New("the worktree has no origin remote")
	ErrUnsupportedRemote = errors.New("the origin remote cannot be normalized into a credential-free repository identity")
	hostPattern          = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]*$`)
	pathSegmentPattern   = regexp.MustCompile(`^[A-Za-z0-9._~-]+$`)
)

// Metadata is safe to send to Conn. CanonicalRemote has no scheme, port,
// user information, query, fragment, or trailing .git suffix.
type Metadata struct {
	HostKind        string
	CanonicalRemote string
}

// Discover reads origin from worktree and converts it to safe metadata.
func Discover(ctx context.Context, worktree string) (Metadata, error) {
	output, err := git(ctx, worktree, "config", "--get", "remote.origin.url")
	if err != nil {
		return Metadata{}, ErrNoOrigin
	}
	return NormalizeRemote(output)
}

// NormalizeRemote accepts common Git remote forms and returns only their safe
// host/path identity. It deliberately omits any credential-like user info.
func NormalizeRemote(raw string) (Metadata, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return Metadata{}, ErrNoOrigin
	}

	var host, path string
	switch {
	case strings.Contains(value, "://"):
		parsed, err := url.Parse(value)
		if err != nil || parsed.Hostname() == "" || parsed.Port() != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
			return Metadata{}, ErrUnsupportedRemote
		}
		host, path = parsed.Hostname(), parsed.EscapedPath()
		if parsed.Scheme != "https" && parsed.Scheme != "http" && parsed.Scheme != "ssh" {
			return Metadata{}, ErrUnsupportedRemote
		}
	case strings.Count(value, "@") == 1 && strings.Contains(value, ":"):
		// SCP-style syntax, for example git@github.com:acme/payments.git.
		_, target, _ := strings.Cut(value, "@")
		host, path, _ = strings.Cut(target, ":")
		if host == "" || path == "" || strings.Contains(path, ":") {
			return Metadata{}, ErrUnsupportedRemote
		}
	default:
		host, path, _ = strings.Cut(value, "/")
		if host == "" || path == "" {
			return Metadata{}, ErrUnsupportedRemote
		}
	}

	host = strings.ToLower(host)
	path = strings.Trim(strings.TrimSuffix(path, ".git"), "/")
	if !hostPattern.MatchString(host) || path == "" {
		return Metadata{}, ErrUnsupportedRemote
	}
	for _, segment := range strings.Split(path, "/") {
		if segment == "." || segment == ".." || !pathSegmentPattern.MatchString(segment) {
			return Metadata{}, ErrUnsupportedRemote
		}
	}
	kind := "generic_git"
	if host == "github.com" {
		kind = "github"
	}
	return Metadata{HostKind: kind, CanonicalRemote: host + "/" + path}, nil
}

// String returns a diagnostic that remains safe when the remote was rejected.
func (m Metadata) String() string {
	return fmt.Sprintf("%s (%s)", m.CanonicalRemote, m.HostKind)
}
