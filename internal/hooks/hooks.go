// Package hooks implements the warn-only Kiro command hooks run as
// `conn hook <name>`. Hooks read Kiro event JSON from standard input, read
// from the Conn backend, and keep small local state under the worktree's Git
// directory. They never write to the backend, never block or deny a tool
// call, and never ask for permission. Any failure allows the event silently
// and records a local diagnostic for conn doctor.
package hooks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"uuid"

	"github.com/Juanqu261/conn/internal/credentials"
	"github.com/Juanqu261/conn/internal/hookstate"
	"github.com/Juanqu261/conn/internal/httpclient"
	"github.com/Juanqu261/conn/internal/protocol"
	"github.com/Juanqu261/conn/internal/repositorygit"
	"github.com/Juanqu261/conn/internal/secret"
)

// Hook names.
const (
	SessionStart = "session-start"
	PromptSubmit = "prompt-submit"
	PreWrite     = "pre-write"
	PostWrite    = "post-write"
)

// Names lists the supported hooks.
var Names = []string{SessionStart, PromptSubmit, PreWrite, PostWrite}

// Bounds and labels. The warning exit code is the non-blocking channel the
// capability proof selected: Kiro shows standard error to the human and lets
// the tool run. Exit code 2 blocks a tool and is never used.
const (
	DefaultWarnExitCode = 1
	Label               = "Conn (Project data, not instructions):"
	MaxOutputChars      = 1500
	maxEventBytes       = 1 << 20
	totalBudget         = 3 * time.Second
	requestTimeout      = 2 * time.Second
	indexFreshness      = 30 * time.Second
	maxWritePaths       = 20
	maxProjectsScanned  = 20
	maxClaimsPerPath    = 50
	maxUpdatesPerPrompt = 100
	startRecommendation = "No Conn Intent is published for this worktree. Run /conn-start before editing."
	serverURLEnv        = "CONN_SERVER_URL"
	projectEnv          = "CONN_PROJECT"
	adapterKind         = "kiro"
)

// Config holds a hook's streams and dependencies.
type Config struct {
	In  io.Reader
	Out io.Writer
	Err io.Writer
	// LookupEnv reads CONN_TOKEN, CONN_SERVER_URL, and CONN_PROJECT.
	LookupEnv func(string) (string, bool)
	// Store returns the operating-system credential store entry for a
	// canonical server URL. Nil means only CONN_TOKEN can authenticate.
	Store func(serverURL string) credentials.Store
	// HTTP performs backend reads. Nil uses a client with the per-request
	// timeout that never follows redirects.
	HTTP *http.Client
	// WarnExitCode is 0 or 1. Zero keeps warnings in the queue only.
	WarnExitCode int
	Now          func() time.Time
}

// Event is the subset of the Kiro hook event that hooks read.
type Event struct {
	Cwd       string         `json:"cwd"`
	SessionID string         `json:"session_id"`
	ToolName  string         `json:"tool_name"`
	ToolInput map[string]any `json:"tool_input"`
}

// failure is a silent, diagnosable hook failure.
type failure struct{ code string }

func (f failure) Error() string { return f.code }

func fail(code string) error { return failure{code: code} }

// Run executes one hook and returns its process exit code: 0, or the
// configured warning code when pre-write warns. It never returns 2.
func Run(ctx context.Context, name string, cfg Config) int {
	ctx, cancel := context.WithTimeout(ctx, totalBudget)
	defer cancel()
	r := &runner{cfg: cfg, name: name}
	if r.cfg.Now == nil {
		r.cfg.Now = time.Now
	}
	if r.cfg.LookupEnv == nil {
		r.cfg.LookupEnv = func(string) (string, bool) { return "", false }
	}
	if r.cfg.HTTP == nil {
		r.cfg.HTTP = &http.Client{Timeout: requestTimeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	if r.cfg.WarnExitCode != 0 && r.cfg.WarnExitCode != 1 {
		r.cfg.WarnExitCode = DefaultWarnExitCode
	}
	code, err := r.run(ctx)
	if err != nil {
		r.diagnose(ctx, err)
		return 0
	}
	if code == 2 {
		return 0
	}
	return code
}

type runner struct {
	cfg   Config
	name  string
	event Event
	top   string
	state *hookstate.Store
}

func (r *runner) run(ctx context.Context) (int, error) {
	if err := r.readEvent(); err != nil {
		return 0, err
	}
	top, err := repositorygit.TopLevel(ctx, r.event.Cwd)
	if err != nil {
		// Outside a Git worktree Conn has nothing to say.
		return 0, nil
	}
	r.top = canonicalPath(filepath.FromSlash(top))
	store, err := hookstate.Open(ctx, r.event.Cwd)
	if err != nil {
		return 0, fail("state_unavailable")
	}
	store.Now = r.cfg.Now
	r.state = &store
	switch r.name {
	case SessionStart:
		return 0, r.sessionStart(ctx)
	case PromptSubmit:
		return 0, r.promptSubmit(ctx)
	case PreWrite:
		return r.preWrite(ctx)
	case PostWrite:
		return 0, r.postWrite()
	}
	return 0, fail("unknown_hook")
}

func (r *runner) readEvent() error {
	if r.cfg.In == nil {
		return fail("event_unreadable")
	}
	data, err := io.ReadAll(io.LimitReader(r.cfg.In, maxEventBytes+1))
	if err != nil || len(data) > maxEventBytes {
		return fail("event_unreadable")
	}
	if len(strings.TrimSpace(string(data))) > 0 {
		if err := json.Unmarshal(data, &r.event); err != nil {
			return fail("event_unreadable")
		}
	}
	if r.event.Cwd == "" {
		if r.event.Cwd, err = os.Getwd(); err != nil {
			return fail("event_unreadable")
		}
	}
	return nil
}

// diagnose records a failure code. Budget exhaustion is reported as such.
func (r *runner) diagnose(ctx context.Context, err error) {
	if r.state == nil {
		return
	}
	code := "hook_failed"
	var f failure
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		code = "budget_exceeded"
	case errors.As(err, &f):
		code = f.code
	case errors.Is(err, httpclient.ErrUnreachable):
		code = "backend_unreachable"
	case httpclient.IsCode(err, "unauthenticated"):
		code = "session_expired_or_revoked"
	case errors.Is(err, credentials.ErrNotLoggedIn):
		code = "not_authenticated"
	case errors.Is(err, credentials.ErrUnavailable), errors.Is(err, credentials.ErrInvalid):
		code = "credential_unavailable"
	}
	r.state.RecordDiagnostic(r.name, code)
}

// client authenticates with CONN_TOKEN, then the credential store entry for
// the server URL. Hooks read as the participant adapter the MCP adapter
// recorded in the focus, Kiro by default, so reads see the same participant.
func (r *runner) client(serverURL string) (*httpclient.Client, error) {
	if serverURL == "" {
		serverURL = httpclient.DefaultServerURL
		if value, ok := r.cfg.LookupEnv(serverURLEnv); ok && value != "" {
			serverURL = value
		}
	}
	base, err := httpclient.New(serverURL, r.cfg.HTTP)
	if err != nil {
		return nil, fail("server_url_invalid")
	}
	var store credentials.Store = unavailableStore{}
	if r.cfg.Store != nil {
		store = r.cfg.Store(base.URL())
	}
	token, _, err := credentials.Resolve(r.cfg.LookupEnv, store)
	if err != nil {
		return nil, err
	}
	adapter := adapterKind
	if r.state != nil {
		if recorded := r.state.Focus().ParticipantAdapter; recorded == "kiro" || recorded == "mcp" {
			adapter = recorded
		}
	}
	return base.WithToken(token).WithAdapter(adapter), nil
}

// connection reports the Project and repository this worktree belongs to.
// A focus record implies a match the MCP adapter already verified. Without
// one, a fresh cached result is reused; otherwise the remote is matched
// against visible Projects within the network budget.
func (r *runner) connection(ctx context.Context, client *httpclient.Client, focus hookstate.Focus) (hookstate.Index, error) {
	index := r.state.Index()
	if focus.ProjectID != nil {
		if index.ProjectID == nil || *index.ProjectID != *focus.ProjectID {
			index = hookstate.Index{Paths: map[string]hookstate.IndexEntry{}}
		}
		index.ProjectID, index.RepositoryID, index.Connected = focus.ProjectID, focus.RepositoryID, true
		return index, nil
	}
	metadata, err := repositorygit.Discover(ctx, r.top)
	if err != nil {
		return hookstate.Index{Paths: map[string]hookstate.IndexEntry{}}, nil
	}
	if r.fresh(index.CheckedAt) && index.CanonicalRemote == metadata.CanonicalRemote {
		return index, nil
	}
	result := hookstate.Index{CanonicalRemote: metadata.CanonicalRemote, CheckedAt: r.cfg.Now().UTC(), Paths: map[string]hookstate.IndexEntry{}}
	candidates := []uuid.UUID{}
	if value, ok := r.cfg.LookupEnv(projectEnv); ok && value != "" {
		id, err := uuid.Parse(value)
		if err != nil {
			return result, fail("project_invalid")
		}
		candidates = append(candidates, id)
	} else {
		projects, err := client.Projects(ctx)
		if err != nil {
			return result, err
		}
		for _, entry := range projects.Projects {
			if len(candidates) == maxProjectsScanned {
				break
			}
			candidates = append(candidates, entry.Project.ProjectID)
		}
	}
	for _, projectID := range candidates {
		repositories, err := client.RepositoryConnections(ctx, projectID)
		if err != nil {
			return result, err
		}
		for _, repository := range repositories.Data.Repositories {
			if repository.CanonicalRemote == metadata.CanonicalRemote {
				project, repositoryID := projectID, repository.RepositoryID
				result.ProjectID, result.RepositoryID, result.Connected = &project, &repositoryID, true
				_ = r.state.WriteIndex(result)
				return result, nil
			}
		}
	}
	_ = r.state.WriteIndex(result)
	return result, nil
}

func (r *runner) fresh(at time.Time) bool {
	return !at.IsZero() && r.cfg.Now().Sub(at) < indexFreshness
}

// refreshIndex maps claim evidence paths to active claims and to open or
// under-review collisions from one Radar snapshot.
func (r *runner) refreshIndex(ctx context.Context, client *httpclient.Client, index hookstate.Index) (hookstate.Index, error) {
	radar, err := client.Radar(ctx, *index.ProjectID)
	if err != nil {
		return index, err
	}
	participants := make(map[uuid.UUID]uuid.UUID, len(radar.Data.ActiveIntents))
	for _, intent := range radar.Data.ActiveIntents {
		participants[intent.IntentID] = intent.ParticipantID
	}
	paths := map[string]hookstate.IndexEntry{}
	claimPaths := map[uuid.UUID][]string{}
	for _, claim := range radar.Data.SemanticClaims {
		for _, evidence := range claim.EvidenceRefs {
			if evidence.Path == "" || strings.Contains(evidence.Path, "..") {
				continue
			}
			path := filepath.ToSlash(evidence.Path)
			claimPaths[claim.ClaimID] = append(claimPaths[claim.ClaimID], path)
			entry := paths[path]
			if len(entry.Claims) < maxClaimsPerPath {
				entry.Claims = append(entry.Claims, hookstate.IndexClaim{
					IntentID: claim.IntentID, IntentRevision: claim.IntentRevision, ParticipantID: participants[claim.IntentID],
					Subject: claim.NormalizedSubject, Relation: claim.Relation,
				})
			}
			paths[path] = entry
		}
	}
	for _, collision := range radar.Data.OpenCollisions {
		seen := map[string]bool{}
		for _, claimID := range collision.MatchingClaimRefs {
			for _, path := range claimPaths[claimID] {
				if seen[path] {
					continue
				}
				seen[path] = true
				entry := paths[path]
				entry.Collisions = append(entry.Collisions, hookstate.IndexCollision{
					CollisionID: collision.CollisionID, RuleID: collision.RuleID, State: collision.State,
					LeftIntentID: collision.LeftIntentID, RightIntentID: collision.RightIntentID,
				})
				paths[path] = entry
			}
		}
	}
	index.Paths, index.AsOfEvent, index.FetchedAt, index.Connected = paths, radar.AsOfEvent, r.cfg.Now().UTC(), true
	_ = r.state.WriteIndex(index)
	return index, nil
}

func (r *runner) sessionStart(ctx context.Context) error {
	focus := r.state.Focus()
	client, err := r.client(focus.ServerURL)
	if err != nil {
		return err
	}
	if focus.HasIntent() {
		work, err := client.WorkContext(ctx, *focus.ProjectID, uuid.UUID{}, *focus.IntentID, nil, 1)
		if err != nil {
			return err
		}
		intent, active := ownedIntent(work.Data, *focus.IntentID)
		if !active {
			return r.print([]string{fmt.Sprintf("Focused Intent %s is no longer active. Run /conn-start before editing.", short(*focus.IntentID))})
		}
		attention, gaps := 0, 0
		for _, collision := range work.Data.RelatedCollisions {
			if involves(collision, intent.IntentID) && needsAttention(collision.State) {
				attention++
			}
		}
		for _, coverage := range work.Data.AcceptanceEvidenceCoverage {
			if coverage.State != "passing" {
				gaps++
			}
		}
		cursor := focus.LastEvent
		if cursor == 0 {
			cursor = work.AsOfEvent
		}
		return r.print([]string{fmt.Sprintf("Intent %s@%d: %d collision(s) need attention, %d evidence gap(s). Details: /conn-sync %d", short(intent.IntentID), intent.Revision, attention, gaps, cursor)})
	}
	index, err := r.connection(ctx, client, focus)
	if err != nil {
		return err
	}
	if index.Connected {
		return r.print([]string{startRecommendation})
	}
	return nil
}

func (r *runner) promptSubmit(ctx context.Context) error {
	focus := r.state.Focus()
	var lines []string
	if focus.HasIntent() {
		client, err := r.client(focus.ServerURL)
		if err != nil {
			return err
		}
		after := focus.LastEvent
		work, err := client.WorkContext(ctx, *focus.ProjectID, uuid.UUID{}, *focus.IntentID, &after, maxUpdatesPerPrompt)
		if err != nil {
			return err
		}
		changes, update := compareFocus(focus, work.Data)
		lines = append(lines, changes...)
		for _, warning := range r.state.TakeWarnings() {
			lines = append(lines, "Before a write: "+warning.Text)
		}
		index := hookstate.Index{ProjectID: focus.ProjectID, RepositoryID: focus.RepositoryID, Connected: true, Paths: map[string]hookstate.IndexEntry{}}
		_, _ = r.refreshIndex(ctx, client, index)
		if err := r.state.UpdateFocus(update); err != nil && !errors.Is(err, hookstate.ErrLocked) {
			return fail("state_unwritable")
		}
		if len(lines) > 0 {
			cursor := work.AsOfEvent
			if work.Data.ContinuationAfter != nil {
				cursor = *work.Data.ContinuationAfter
			}
			lines = append(lines, fmt.Sprintf("Details: /conn-sync %d", max(after, min(cursor, work.AsOfEvent))))
		}
		return r.print(lines)
	}
	for _, warning := range r.state.TakeWarnings() {
		lines = append(lines, "Before a write: "+warning.Text)
	}
	if focus.RecommendedSession == "" || focus.RecommendedSession != r.event.SessionID {
		client, err := r.client(focus.ServerURL)
		if err != nil {
			return errors.Join(err, r.print(lines))
		}
		index, err := r.connection(ctx, client, focus)
		if err != nil {
			return errors.Join(err, r.print(lines))
		}
		if index.Connected {
			lines = append(lines, startRecommendation)
			session := r.event.SessionID
			if session == "" {
				session = "unknown"
			}
			_ = r.state.UpdateFocus(func(focus *hookstate.Focus) { focus.RecommendedSession = session })
		}
	}
	return r.print(lines)
}

// compareFocus reports new collisions needing attention, state changes, and
// counterpart Intent revisions since the stored cursor. It returns the focus
// update that stores the new cursor and collision states. A collision that
// continues a known rule and counterpart under a new identity, after either
// Intent was revised, is not reported again.
func compareFocus(focus hookstate.Focus, work protocol.WorkContext) ([]string, func(*hookstate.Focus)) {
	intentID := *focus.IntentID
	var lines []string
	intent, active := ownedIntent(work, intentID)
	if !active {
		lines = append(lines, fmt.Sprintf("Focused Intent %s is no longer active. Run /conn-start before editing.", short(intentID)))
	}
	states := map[string]string{}
	pairs := map[string]string{}
	counterparts := map[uuid.UUID]bool{}
	for _, collision := range work.RelatedCollisions {
		if !involves(collision, intentID) {
			continue
		}
		counterpart := counterpartOf(collision, intentID)
		counterparts[counterpart] = true
		states[collision.CollisionID.String()] = collision.State
		if key := hookstate.PairKey(collision.RuleID, counterpart); collision.State != protocol.CollisionStale || pairs[key] == "" {
			pairs[key] = collision.State
		}
	}
	for _, collision := range work.RelatedCollisions {
		if !involves(collision, intentID) {
			continue
		}
		counterpart := counterpartOf(collision, intentID)
		key := hookstate.PairKey(collision.RuleID, counterpart)
		previous, seen := focus.KnownCollisions[collision.CollisionID.String()]
		switch {
		case !seen && needsAttention(collision.State) && !needsAttention(focus.KnownPairs[key]):
			lines = append(lines, fmt.Sprintf("Intent %s@%d: new %s collision %s (%s) on %s with Intent %s@%d.",
				short(intentID), ownRevision(collision, intentID), collision.State, short(collision.CollisionID), collision.RuleID,
				subjects(collision), short(counterpart), ownRevision(collision, counterpart)))
		case !seen && !needsAttention(collision.State) && collision.State != protocol.CollisionStale && focus.KnownPairs[key] != collision.State:
			lines = append(lines, fmt.Sprintf("Collision %s (%s) with Intent %s carries the %s decision forward.", short(collision.CollisionID), collision.RuleID, short(counterpart), collision.State))
		case seen && previous != collision.State && !(collision.State == protocol.CollisionStale && pairs[key] != protocol.CollisionStale):
			lines = append(lines, fmt.Sprintf("Collision %s (%s) is now %s%s.", short(collision.CollisionID), collision.RuleID, collision.State, decisionKind(collision.State)))
		}
	}
	for _, event := range work.Updates {
		if !counterparts[event.EntityID] || event.EntityID == intentID {
			continue
		}
		switch payload := event.Payload.(type) {
		case protocol.IntentRevised:
			lines = append(lines, fmt.Sprintf("Counterpart Intent %s is now at revision %d.", short(event.EntityID), payload.Revision))
		case protocol.IntentReleased:
			lines = append(lines, fmt.Sprintf("Counterpart Intent %s was released at revision %d.", short(event.EntityID), payload.Revision))
		}
	}
	cursor := work.AsOfEvent
	if work.ContinuationAfter != nil {
		cursor = *work.ContinuationAfter
	}
	update := func(f *hookstate.Focus) {
		if f.IntentID == nil || *f.IntentID != intentID {
			return
		}
		if !active {
			// The backend confirmed the Intent is no longer active, so the
			// focus is cleared and the message is reported once.
			f.IntentID, f.IntentRevision, f.ParticipantID = nil, 0, nil
			f.KnownCollisions, f.KnownPairs, f.LastEvent = nil, nil, 0
			return
		}
		if cursor > f.LastEvent {
			f.LastEvent = cursor
		}
		f.KnownCollisions, f.KnownPairs = states, pairs
		f.IntentRevision = intent.Revision
	}
	return lines, update
}

func counterpartOf(collision protocol.CollisionAssessment, intentID uuid.UUID) uuid.UUID {
	if collision.LeftIntentID == intentID {
		return collision.RightIntentID
	}
	return collision.LeftIntentID
}

func (r *runner) preWrite(ctx context.Context) (int, error) {
	paths := r.writePaths()
	if len(paths) == 0 {
		return 0, nil
	}
	focus := r.state.Focus()
	index := r.state.Index()
	if !r.fresh(index.FetchedAt) || !index.Connected || (focus.ProjectID != nil && (index.ProjectID == nil || *index.ProjectID != *focus.ProjectID)) {
		client, err := r.client(focus.ServerURL)
		if err != nil {
			return 0, err
		}
		if index, err = r.connection(ctx, client, focus); err != nil {
			return 0, err
		}
		if !index.Connected {
			return 0, nil
		}
		if index, err = r.refreshIndex(ctx, client, index); err != nil {
			return 0, err
		}
	}
	var warnings []string
	add := func(text string) {
		for _, existing := range warnings {
			if existing == text {
				return
			}
		}
		warnings = append(warnings, text)
	}
	if !focus.HasIntent() {
		add(startRecommendation)
	}
	for _, path := range paths {
		entry := index.Paths[path]
		for _, claim := range entry.Claims {
			if focus.ParticipantID != nil && claim.ParticipantID == *focus.ParticipantID {
				continue
			}
			if focus.IntentID != nil && claim.IntentID == *focus.IntentID {
				continue
			}
			if claim.Relation == "changes" || claim.Relation == "removes" || claim.Relation == "consumes" {
				add(fmt.Sprintf("Intent %s@%d %s %s.", short(claim.IntentID), claim.IntentRevision, claim.Relation, claim.Subject))
			}
		}
		if focus.IntentID == nil {
			continue
		}
		for _, collision := range entry.Collisions {
			if (collision.LeftIntentID == *focus.IntentID || collision.RightIntentID == *focus.IntentID) && needsAttention(collision.State) {
				add(fmt.Sprintf("Collision %s (%s) involves your Intent %s.", short(collision.CollisionID), collision.RuleID, short(*focus.IntentID)))
			}
		}
	}
	if len(warnings) == 0 {
		return 0, nil
	}
	queued := make([]hookstate.Warning, 0, len(warnings))
	for _, text := range warnings {
		queued = append(queued, hookstate.Warning{Text: text, CreatedAt: r.cfg.Now().UTC()})
	}
	_ = r.state.QueueWarnings(queued)
	lines := append([]string{}, warnings...)
	lines = append(lines, "This is an advisory warning; the write continues. Review with /conn-start or /conn-resolve.")
	if _, err := io.WriteString(r.cfg.Err, render(lines)); err != nil {
		return 0, fail("output_unwritable")
	}
	return r.cfg.WarnExitCode, nil
}

func (r *runner) postWrite() error {
	paths := r.writePaths()
	entries := make([]hookstate.Written, 0, len(paths))
	for _, path := range paths {
		entries = append(entries, hookstate.Written{Path: path, SessionID: boundedSession(r.event.SessionID), At: r.cfg.Now().UTC()})
	}
	if err := r.state.AppendWritten(entries); err != nil && !errors.Is(err, hookstate.ErrLocked) {
		return fail("state_unwritable")
	}
	return nil
}

// writePaths extracts at most maxWritePaths repository-relative paths from
// the tool input keys path, file_path, and paths. Paths outside the worktree
// are ignored.
func (r *runner) writePaths() []string {
	var raw []string
	collect := func(value any) {
		switch value := value.(type) {
		case string:
			raw = append(raw, value)
		case []any:
			for _, item := range value {
				if text, ok := item.(string); ok {
					raw = append(raw, text)
				}
			}
		}
	}
	for _, key := range []string{"path", "file_path", "paths"} {
		collect(r.event.ToolInput[key])
	}
	cwd := canonicalPath(r.event.Cwd)
	seen := map[string]bool{}
	paths := []string{}
	for _, value := range raw {
		if len(paths) == maxWritePaths {
			break
		}
		if value == "" || len(value) > 4096 || strings.ContainsRune(value, 0) {
			continue
		}
		path := filepath.FromSlash(value)
		if !filepath.IsAbs(path) {
			path = filepath.Join(cwd, path)
		}
		relative, err := filepath.Rel(r.top, canonicalPath(path))
		if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
			continue
		}
		slashed := filepath.ToSlash(relative)
		if !seen[slashed] {
			seen[slashed] = true
			paths = append(paths, slashed)
		}
	}
	return paths
}

func (r *runner) print(lines []string) error {
	if len(lines) == 0 {
		return nil
	}
	if _, err := io.WriteString(r.cfg.Out, render(lines)); err != nil {
		return fail("output_unwritable")
	}
	return nil
}

// render labels hook output as Project data and bounds it to MaxOutputChars.
func render(lines []string) string {
	var out strings.Builder
	out.WriteString(Label + "\n")
	for i, line := range lines {
		next := "- " + safeLine(line) + "\n"
		if out.Len()+len(next) > MaxOutputChars-40 {
			fmt.Fprintf(&out, "- %d more item(s) omitted.\n", len(lines)-i)
			break
		}
		out.WriteString(next)
	}
	return out.String()
}

// safeLine removes control characters. Hook lines are built only from
// identifiers, revisions, rule IDs, subjects, states, counts, and commands.
func safeLine(line string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, line)
}

func ownedIntent(work protocol.WorkContext, intentID uuid.UUID) (protocol.IntentRevision, bool) {
	for _, intent := range work.OwnedActiveIntents {
		if intent.IntentID == intentID {
			return intent, true
		}
	}
	return protocol.IntentRevision{}, false
}

func involves(collision protocol.CollisionAssessment, intentID uuid.UUID) bool {
	return collision.LeftIntentID == intentID || collision.RightIntentID == intentID
}

func ownRevision(collision protocol.CollisionAssessment, intentID uuid.UUID) int {
	if collision.LeftIntentID == intentID {
		return collision.LeftIntentRevision
	}
	return collision.RightIntentRevision
}

func needsAttention(state string) bool {
	return state == protocol.CollisionOpen || state == protocol.CollisionUnderReview
}

// decisionKind names the decision when the state implies exactly one kind.
func decisionKind(state string) string {
	if state == protocol.CollisionDismissed {
		return " (false_positive)"
	}
	return ""
}

func subjects(collision protocol.CollisionAssessment) string {
	if len(collision.MatchedSubjects) == 0 {
		return "matched subjects"
	}
	text := collision.MatchedSubjects[0]
	if extra := len(collision.MatchedSubjects) - 1; extra > 0 {
		text += fmt.Sprintf(" and %d more subject(s)", extra)
	}
	return text
}

func short(id uuid.UUID) string { return id.String()[:8] }

func boundedSession(session string) string {
	session = safeLine(session)
	if len(session) > 128 {
		return session[:128]
	}
	return session
}

// canonicalPath resolves symlinks for the existing part of a path so Git's
// reported top level and Kiro's working directory compare equal.
func canonicalPath(path string) string {
	path = filepath.Clean(path)
	existing, rest := path, ""
	for {
		if resolved, err := filepath.EvalSymlinks(existing); err == nil {
			if absolute, err := filepath.Abs(resolved); err == nil {
				return filepath.Join(absolute, rest)
			}
			return path
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			return path
		}
		rest = filepath.Join(filepath.Base(existing), rest)
		existing = parent
	}
}

// unavailableStore is used when no credential store is configured, so only
// CONN_TOKEN can authenticate.
type unavailableStore struct{}

func (unavailableStore) Get() (secret.String, error) { return secret.String{}, credentials.ErrNotFound }
func (unavailableStore) Set(secret.String) error     { return credentials.ErrUnavailable }
func (unavailableStore) Delete() error               { return nil }
