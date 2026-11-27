package cicheck

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Juanqu261/conn/internal/secret"
)

// GitHubClient finds and updates the one Conn comment on a pull request.
type GitHubClient struct {
	// BaseURL defaults to https://api.github.com.
	BaseURL string
	HTTP    *http.Client
}

const (
	gitHubAPIVersion  = "2022-11-28"
	maxCommentPages   = 10
	maxGitHubResponse = 8 << 20
	// CommentAuthor is the identity of the workflow's github.token. Only its
	// comments can be Conn's: anyone may post a comment that carries the
	// marker, and updating theirs would put the report under their control.
	CommentAuthor = "github-actions[bot]"
)

func (b *builder) comment(ctx context.Context, body string) error {
	token, ok := lookup(b.cfg, GitHubTokenEnv)
	if !ok {
		return errors.New("GITHUB_TOKEN is not set, so no pull-request comment was written")
	}
	if b.event.Number == 0 || b.event.Owner == "" {
		return errors.New("the event names no pull request repository, so no comment was written")
	}
	client := b.cfg.GitHub
	if client == nil {
		client = &GitHubClient{}
	}
	return client.UpsertComment(ctx, secret.New(token), b.event.Owner, b.event.Name, b.event.Number, body)
}

// UpsertComment updates the comment that CommentAuthor wrote with Marker, or
// creates one.
func (c *GitHubClient) UpsertComment(ctx context.Context, token secret.String, owner, name string, number int, body string) error {
	type comment struct {
		ID   int64  `json:"id"`
		Body string `json:"body"`
		User *struct {
			Login string `json:"login"`
		} `json:"user"`
	}
	for page := 1; page <= maxCommentPages; page++ {
		var comments []comment
		path := fmt.Sprintf("/repos/%s/%s/issues/%d/comments?per_page=100&page=%d", owner, name, number, page)
		if err := c.call(ctx, token, http.MethodGet, path, nil, &comments); err != nil {
			return err
		}
		for _, existing := range comments {
			if existing.User != nil && existing.User.Login == CommentAuthor && strings.Contains(existing.Body, Marker) {
				return c.call(ctx, token, http.MethodPatch, fmt.Sprintf("/repos/%s/%s/issues/comments/%d", owner, name, existing.ID), map[string]string{"body": body}, nil)
			}
		}
		if len(comments) < 100 {
			break
		}
	}
	return c.call(ctx, token, http.MethodPost, fmt.Sprintf("/repos/%s/%s/issues/%d/comments", owner, name, number), map[string]string{"body": body}, nil)
}

func (c *GitHubClient) call(ctx context.Context, token secret.String, method, path string, input, output any) error {
	base := strings.TrimRight(c.BaseURL, "/")
	if base == "" {
		base = "https://api.github.com"
	}
	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	var reader io.Reader
	if input != nil {
		data, err := json.Marshal(input)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", gitHubAPIVersion)
	req.Header.Set("Authorization", "Bearer "+token.Reveal())
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return errors.New("the GitHub API could not be reached")
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxGitHubResponse))
	if err != nil {
		return errors.New("the GitHub API response could not be read")
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("the GitHub API returned HTTP %d for %s; the workflow needs pull-requests: write", resp.StatusCode, method)
	}
	if output != nil {
		if err := json.Unmarshal(data, output); err != nil {
			return errors.New("the GitHub API returned an unreadable response")
		}
	}
	return nil
}
