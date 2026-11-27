// Package githubissue reads one GitHub issue through the GitHub REST API and
// turns it into reviewed Objective import content with provenance. An
// optional GH_TOKEN or GITHUB_TOKEN stays in this process: it is never
// persisted, logged, or sent to Conn.
package githubissue

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Juanqu261/conn/internal/protocol"
	"github.com/Juanqu261/conn/internal/secret"
)

// Bounds and API constants.
const (
	MaxBodyChars    = 32_768
	MaxCriteria     = 20
	maxTitleRunes   = 200
	maxStatement    = 1000
	maxResponse     = 2 << 20
	apiVersion      = "2022-11-28"
	defaultBaseURL  = "https://api.github.com"
	defaultTimeout  = 30 * time.Second
	SourceKind      = "github_issue"
	maxKeyWords     = 6
	maxKeyCharacter = 48
)

// Errors a human can act on.
var (
	ErrPullRequest = errors.New("the reference names a pull request, not an issue; Conn imports issues only")
	ErrNotFound    = errors.New("GitHub has no such issue, or it is private; set GH_TOKEN to read private issues")
	ErrTooLong     = fmt.Errorf("the issue body exceeds %d characters, the Objective overview bound; shorten the imported text or import a summary with conn objective create", MaxBodyChars)
)

// Reference names one issue.
type Reference struct {
	Owner  string
	Name   string
	Number int
}

var (
	shortPattern = regexp.MustCompile(`^([A-Za-z0-9._-]{1,100})/([A-Za-z0-9._-]{1,100})#([1-9][0-9]{0,9})$`)
	urlPath      = regexp.MustCompile(`^/([A-Za-z0-9._-]{1,100})/([A-Za-z0-9._-]{1,100})/issues/([1-9][0-9]{0,9})/?$`)
)

// ParseReference accepts owner/name#number or
// https://github.com/owner/name/issues/number.
func ParseReference(value string) (Reference, error) {
	value = strings.TrimSpace(value)
	if match := shortPattern.FindStringSubmatch(value); match != nil {
		return newReference(match[1], match[2], match[3])
	}
	if parsed, err := url.Parse(value); err == nil && parsed.Scheme == "https" && strings.EqualFold(parsed.Host, "github.com") && parsed.User == nil {
		if match := urlPath.FindStringSubmatch(parsed.Path); match != nil {
			return newReference(match[1], match[2], match[3])
		}
	}
	return Reference{}, fmt.Errorf("%q is not owner/name#number or https://github.com/owner/name/issues/number", value)
}

func newReference(owner, name, number string) (Reference, error) {
	for _, part := range []string{owner, name} {
		if part == "." || part == ".." {
			return Reference{}, errors.New("the repository owner and name must be real path segments")
		}
	}
	n, err := strconv.Atoi(number)
	if err != nil {
		return Reference{}, err
	}
	return Reference{Owner: owner, Name: name, Number: n}, nil
}

// SourceRef is the stable source reference recorded with the Objective.
func (r Reference) SourceRef() string {
	return fmt.Sprintf("github.com/%s/%s/issues/%d", r.Owner, r.Name, r.Number)
}

// Repository is the canonical remote of the issue's repository.
func (r Reference) Repository() string { return "github.com/" + r.Owner + "/" + r.Name }

func (r Reference) String() string { return fmt.Sprintf("%s/%s#%d", r.Owner, r.Name, r.Number) }

// Issue is the part of a GitHub issue an import uses.
type Issue struct {
	Title     string
	Body      string
	UpdatedAt string
}

// Client reads issues from the GitHub REST API.
type Client struct {
	// BaseURL defaults to https://api.github.com.
	BaseURL string
	HTTP    *http.Client
	// Token is optional. It is sent only to BaseURL.
	Token secret.String
}

// TokenFromEnv returns GH_TOKEN, then GITHUB_TOKEN, when one is set.
func TokenFromEnv(lookup func(string) (string, bool)) secret.String {
	for _, name := range []string{"GH_TOKEN", "GITHUB_TOKEN"} {
		if value, ok := lookup(name); ok && strings.TrimSpace(value) != "" {
			return secret.New(strings.TrimSpace(value))
		}
	}
	return secret.String{}
}

// Fetch reads one issue and rejects pull requests.
func (c Client) Fetch(ctx context.Context, reference Reference) (Issue, error) {
	base := strings.TrimRight(c.BaseURL, "/")
	if base == "" {
		base = defaultBaseURL
	}
	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultTimeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	path := fmt.Sprintf("/repos/%s/%s/issues/%d", url.PathEscape(reference.Owner), url.PathEscape(reference.Name), reference.Number)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
	if err != nil {
		return Issue{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", apiVersion)
	if !c.Token.IsZero() {
		req.Header.Set("Authorization", "Bearer "+c.Token.Reveal())
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return Issue{}, ctx.Err()
		}
		return Issue{}, errors.New("the GitHub API could not be reached")
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if err != nil || len(data) > maxResponse {
		return Issue{}, errors.New("the GitHub API response is unreadable or too large")
	}
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return Issue{}, ErrNotFound
	case (resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests) && resp.Header.Get("X-RateLimit-Remaining") == "0":
		reset := "later"
		if seconds, err := strconv.ParseInt(resp.Header.Get("X-RateLimit-Reset"), 10, 64); err == nil {
			reset = "at " + time.Unix(seconds, 0).UTC().Format(time.RFC3339)
		}
		hint := " Set GH_TOKEN for a higher limit."
		if !c.Token.IsZero() {
			hint = ""
		}
		return Issue{}, fmt.Errorf("the GitHub API rate limit is exhausted; it resets %s.%s", reset, hint)
	case resp.StatusCode == http.StatusUnauthorized:
		return Issue{}, errors.New("GitHub rejected GH_TOKEN or GITHUB_TOKEN")
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		return Issue{}, fmt.Errorf("the GitHub API returned HTTP %d", resp.StatusCode)
	}
	var document struct {
		Title       *string          `json:"title"`
		Body        *string          `json:"body"`
		UpdatedAt   string           `json:"updated_at"`
		PullRequest *json.RawMessage `json:"pull_request"`
	}
	if err := json.Unmarshal(data, &document); err != nil || document.Title == nil {
		return Issue{}, errors.New("the GitHub API returned an unreadable issue")
	}
	if document.PullRequest != nil {
		return Issue{}, ErrPullRequest
	}
	issue := Issue{Title: *document.Title, UpdatedAt: document.UpdatedAt}
	if document.Body != nil {
		issue.Body = *document.Body
	}
	return issue, nil
}

// Import is reviewed Objective import content.
type Import struct {
	Reference Reference
	Input     protocol.ObjectiveImportInput
	// Notes describe adjustments, such as a shortened title.
	Notes []string
}

// ContentHash is sha256(title + "\n\n" + body).
func ContentHash(issue Issue) string {
	sum := sha256.Sum256([]byte(issue.Title + "\n\n" + issue.Body))
	return hex.EncodeToString(sum[:])
}

// Prepare builds the import input for human review. It refuses a body over
// the overview bound instead of cutting it.
func Prepare(reference Reference, issue Issue, criteria bool) (Import, error) {
	body := strings.ReplaceAll(issue.Body, "\x00", "")
	if utf8.RuneCountInString(body) > MaxBodyChars {
		return Import{}, ErrTooLong
	}
	result := Import{Reference: reference}
	title := strings.Join(strings.Fields(strings.ReplaceAll(issue.Title, "\x00", "")), " ")
	if title == "" {
		title = "GitHub issue " + reference.String()
	}
	if runes := []rune(title); len(runes) > maxTitleRunes {
		title = string(runes[:maxTitleRunes-1]) + "…"
		result.Notes = append(result.Notes, "The issue title was shortened to 200 characters.")
	}
	overview := body
	if strings.TrimSpace(overview) == "" {
		overview = "Imported from GitHub issue " + reference.String() + ", which has no description."
	}
	revision := issue.UpdatedAt
	if _, err := time.Parse(time.RFC3339, revision); err != nil {
		revision = ""
	}
	result.Input = protocol.ObjectiveImportInput{
		SourceKind: SourceKind, SourceRef: reference.SourceRef(), SourceRevisionRef: revision,
		ContentHash: ContentHash(issue), Title: title, OverviewMarkdown: overview,
	}
	if criteria {
		result.Input.KeyPoints = SuggestCriteria(body)
	}
	return result, nil
}

var (
	headingPattern = regexp.MustCompile(`^\s{0,3}#{1,6}\s+(.*?)\s*#*\s*$`)
	itemPattern    = regexp.MustCompile(`^\s*(?:[-*+]|[0-9]{1,3}[.)])\s+(?:\[[ xX]\]\s+)?(.+?)\s*$`)
)

// SuggestCriteria returns up to 20 keyed acceptance criteria from the list
// items under the first heading whose text contains "acceptance criteria".
// Keys are slugs of the first words, made unique.
func SuggestCriteria(body string) []protocol.KeyPoint {
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	start := -1
	for i, line := range lines {
		if match := headingPattern.FindStringSubmatch(line); match != nil && strings.Contains(strings.ToLower(match[1]), "acceptance criteria") {
			start = i + 1
			break
		}
	}
	if start < 0 {
		return nil
	}
	used := map[string]bool{}
	var points []protocol.KeyPoint
	for _, line := range lines[start:] {
		if headingPattern.MatchString(line) {
			break
		}
		match := itemPattern.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		statement := strings.Join(strings.Fields(match[1]), " ")
		if statement == "" {
			continue
		}
		if runes := []rune(statement); len(runes) > maxStatement {
			statement = string(runes[:maxStatement-1]) + "…"
		}
		key := uniqueKey(slug(statement), used)
		points = append(points, protocol.KeyPoint{Key: key, Kind: protocol.KeyPointAcceptanceCriterion, Statement: statement})
		if len(points) == MaxCriteria {
			break
		}
	}
	return points
}

func slug(statement string) string {
	words := strings.FieldsFunc(strings.ToLower(statement), func(r rune) bool { return !(r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r))) })
	if len(words) > maxKeyWords {
		words = words[:maxKeyWords]
	}
	key := strings.Join(words, "-")
	if len(key) > maxKeyCharacter {
		key = strings.TrimRight(key[:maxKeyCharacter], "-")
	}
	if key == "" || key[0] < 'a' || key[0] > 'z' {
		key = "criterion-" + key
	}
	return strings.TrimRight(key, "-")
}

func uniqueKey(key string, used map[string]bool) string {
	candidate := key
	for n := 2; used[candidate]; n++ {
		candidate = fmt.Sprintf("%s-%d", key, n)
	}
	used[candidate] = true
	return candidate
}
