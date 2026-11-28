// Package cicheck is the Conn pull-request check. It reads Conn through a CI
// credential, reports the Intents a pull request names, its previewed
// collisions, and one review packet per Intent, and writes the report to the
// job summary and, when enabled, one marker-identified pull-request comment.
// It never writes to Conn, never runs project code, and never fails the job
// for a finding or an error.
package cicheck

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"uuid"

	"github.com/Juanqu261/conn/internal/analyzer"
	"github.com/Juanqu261/conn/internal/envvalue"
	"github.com/Juanqu261/conn/internal/httpclient"
	"github.com/Juanqu261/conn/internal/protocol"
	"github.com/Juanqu261/conn/internal/repositorygit"
	"github.com/Juanqu261/conn/internal/review"
	"github.com/Juanqu261/conn/internal/secret"
)

// Report bounds and markers.
const (
	Marker          = "<!-- conn-check v1 -->"
	MaxSummaryBytes = 1 << 20
	MaxCommentChars = 65_536
	// commentBudget leaves room below the comment limit for the closing note.
	commentBudget  = MaxCommentChars - 2_000
	summaryBudget  = MaxSummaryBytes - 8_000
	maxEventBytes  = 25 << 20
	maxNamed       = protocol.MaxPreviewExclusions
	TokenEnv       = "CONN_CI_TOKEN"
	ServerEnv      = "CONN_SERVER_URL"
	GitHubTokenEnv = "GITHUB_TOKEN"
	SkippedMessage = "Conn check skipped: CI credential unavailable (fork pull request or missing secret)"
)

// Config is one check run.
type Config struct {
	Worktree    string
	EventPath   string
	SummaryPath string
	Comment     bool
	LookupEnv   func(string) (string, bool)
	// HTTP calls Conn; GitHub calls the GitHub API. Nil uses defaults.
	HTTP   *http.Client
	GitHub *GitHubClient
	// Analyzer runs observed analysis of base..head.
	Analyzer          analyzer.Config
	AnalyzerAvailable bool
	// Log receives progress and diagnostics; it never receives a token.
	Log io.Writer
	Now func() time.Time
}

// Outcome is what the check produced. The process always exits 0.
type Outcome struct {
	Markdown string
	Comment  string
	Skipped  bool
	Errors   []string
}

// Event is the part of a pull_request event the check reads.
type Event struct {
	Number int
	Base   string
	Head   string
	// HeadRef is the pull request's head branch, or empty when the event
	// names no valid branch.
	HeadRef string
	Owner   string
	Name    string
}

var commitPattern = regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`)

// Run executes the check and writes its outputs. Errors are described in the
// report and never returned.
func Run(ctx context.Context, cfg Config) Outcome {
	log := cfg.Log
	if log == nil {
		log = io.Discard
	}
	token, ok := lookup(cfg, TokenEnv)
	if !ok {
		outcome := Outcome{Markdown: Marker + "\n## Conn check\n\n" + SkippedMessage + ".\n", Skipped: true}
		writeSummary(cfg, outcome.Markdown, log)
		fmt.Fprintln(log, SkippedMessage)
		return outcome
	}
	report := &builder{cfg: cfg, token: secret.New(token)}
	report.run(ctx)
	outcome := Outcome{Markdown: report.render(summaryBudget), Errors: report.errors}
	writeSummary(cfg, outcome.Markdown, log)
	if cfg.Comment {
		outcome.Comment = report.render(commentBudget)
		if err := report.comment(ctx, outcome.Comment); err != nil {
			fmt.Fprintf(log, "Conn check could not update the pull-request comment: %v\n", err)
			outcome.Errors = append(outcome.Errors, err.Error())
		}
	}
	for _, message := range outcome.Errors {
		fmt.Fprintf(log, "Conn check: %s\n", message)
	}
	return outcome
}

func lookup(cfg Config, name string) (string, bool) {
	get := cfg.LookupEnv
	if get == nil {
		get = os.LookupEnv
	}
	return envvalue.Lookup(get, name)
}

func writeSummary(cfg Config, markdown string, log io.Writer) {
	if cfg.SummaryPath == "" {
		fmt.Fprint(log, markdown)
		return
	}
	file, err := os.OpenFile(cfg.SummaryPath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		fmt.Fprintf(log, "Conn check could not write the job summary: %v\n", err)
		return
	}
	defer file.Close()
	if _, err := io.WriteString(file, markdown); err != nil {
		fmt.Fprintf(log, "Conn check could not write the job summary: %v\n", err)
	}
}

// ReadEvent reads the pull request number, base and head commits, and the
// repository from a GitHub pull_request event file.
func ReadEvent(path string) (Event, error) {
	if path == "" {
		return Event{}, errors.New("no event file was given; pass --event $GITHUB_EVENT_PATH")
	}
	file, err := os.Open(path)
	if err != nil {
		return Event{}, fmt.Errorf("read the event file: %w", err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxEventBytes+1))
	if err != nil || len(data) > maxEventBytes {
		return Event{}, errors.New("the event file is unreadable or too large")
	}
	var document struct {
		PullRequest *struct {
			Number int `json:"number"`
			Base   struct {
				SHA string `json:"sha"`
			} `json:"base"`
			Head struct {
				SHA string `json:"sha"`
				Ref string `json:"ref"`
			} `json:"head"`
		} `json:"pull_request"`
		Repository struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		return Event{}, errors.New("the event file is not JSON")
	}
	if document.PullRequest == nil {
		return Event{}, errors.New("the event is not a pull_request event")
	}
	event := Event{Number: document.PullRequest.Number, Base: document.PullRequest.Base.SHA, Head: document.PullRequest.Head.SHA}
	if event.Number < 1 || !commitPattern.MatchString(event.Base) || !commitPattern.MatchString(event.Head) {
		return Event{}, errors.New("the pull_request event has no valid number, base SHA, or head SHA")
	}
	if ref := document.PullRequest.Head.Ref; repositorygit.ValidBranchName(ref) {
		event.HeadRef = ref
	}
	owner, name, found := strings.Cut(document.Repository.FullName, "/")
	if found && validRepositoryPart(owner) && validRepositoryPart(name) {
		event.Owner, event.Name = owner, name
	}
	return event, nil
}

var repositoryPart = regexp.MustCompile(`^[A-Za-z0-9._-]{1,100}$`)

func validRepositoryPart(value string) bool {
	return repositoryPart.MatchString(value) && value != "." && value != ".."
}

// namedIntent is one Intent a trailer names, as Conn reports it.
type namedIntent struct {
	trailer  review.IntentTrailers
	view     *protocol.IntentView
	packet   *protocol.ReviewPacket
	markdown string
	note     string
	// verified means the Intent declares this pull request's head branch.
	// A trailer is typed by the commit author, so only a verified Intent is
	// treated as this pull request's: it is excluded from the collision
	// preview and gets a review packet. Any other named Intent keeps its
	// collisions visible and lends this pull request none of its evidence.
	verified bool
}

type builder struct {
	cfg        Config
	token      secret.String
	event      Event
	context    protocol.CICredentialContext
	base       string
	named      []*namedIntent
	preview    *protocol.CollisionPreview
	observed   protocol.LocalAnalyzerReport
	analyzed   bool
	notes      []string
	errors     []string
	trailerLog review.TrailerLog
}

func (b *builder) fail(format string, values ...any) {
	b.errors = append(b.errors, fmt.Sprintf(format, values...))
}

func (b *builder) run(ctx context.Context) {
	server, ok := lookup(b.cfg, ServerEnv)
	if !ok {
		b.fail("CONN_SERVER_URL is not set; set the repository variable with gh variable set CONN_SERVER_URL.")
		return
	}
	httpClient := b.cfg.HTTP
	if httpClient == nil {
		httpClient = httpclient.NewHTTPClient()
	}
	base, err := httpclient.New(server, httpClient)
	if err != nil {
		b.fail("The Conn server URL is invalid: %v", err)
		return
	}
	client := base.WithToken(b.token)
	credential, err := client.CICredentialContext(ctx)
	if err != nil {
		b.fail("Conn could not authorize the CI credential: %s", describe(err))
		return
	}
	b.context = credential.Data
	metadata, err := repositorygit.Discover(ctx, b.cfg.Worktree)
	if err != nil || metadata.CanonicalRemote != b.context.CanonicalRemote {
		b.fail("The checked-out repository is not %s, the repository this CI credential is bound to.", b.context.CanonicalRemote)
		return
	}
	if b.event, err = ReadEvent(b.cfg.EventPath); err != nil {
		b.fail("%v", err)
		return
	}
	mergeBase, err := repositorygit.Output(ctx, b.cfg.Worktree, 1<<10, "merge-base", b.event.Base, b.event.Head)
	if err != nil || !commitPattern.MatchString(strings.TrimSpace(mergeBase)) {
		b.fail("Git could not compute the merge base of %s and %s; check out with fetch-depth: 0.", short(b.event.Base), short(b.event.Head))
		return
	}
	b.base = strings.TrimSpace(mergeBase)
	if b.trailerLog, err = review.ReadTrailers(ctx, b.cfg.Worktree, b.base, b.event.Head); err != nil {
		b.fail("Git could not read the commit trailers of the pull request.")
		return
	}
	b.analyze(ctx)
	b.readIntents(ctx, client)
	b.previewCollisions(ctx, client)
	b.packets(ctx, client)
}

func (b *builder) analyze(ctx context.Context) {
	if !b.cfg.AnalyzerAvailable {
		b.notes = append(b.notes, "The TypeScript analyzer is unavailable in this job, so no changes were observed and no collisions were previewed.")
		return
	}
	report, err := analyzer.Run(ctx, b.cfg.Analyzer, analyzer.Request{
		Mode: protocol.AnalyzerModeObserved, Worktree: b.cfg.Worktree, CanonicalRemote: b.context.CanonicalRemote, BaseRevisionRef: b.base,
	})
	if err != nil {
		b.fail("Observed analysis of the pull request did not complete: %v", err)
		return
	}
	b.observed, b.analyzed = report, true
}

func (b *builder) readIntents(ctx context.Context, client *httpclient.Client) {
	ids := make([]uuid.UUID, 0, len(b.trailerLog.ByIntent))
	for id := range b.trailerLog.ByIntent {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i].String() < ids[j].String() })
	if len(ids) > maxNamed {
		b.notes = append(b.notes, fmt.Sprintf("The pull request names %d Intents; only the first %d are reported.", len(ids), maxNamed))
		ids = ids[:maxNamed]
	}
	for _, id := range ids {
		named := &namedIntent{trailer: b.trailerLog.ByIntent[id]}
		b.named = append(b.named, named)
		view, err := client.Intent(ctx, b.context.ProjectID, id)
		if err != nil {
			named.note = fmt.Sprintf("Trailer names Intent `%s`, which this CI credential cannot read: it does not exist in this Project or Conn is unavailable.", id)
			continue
		}
		named.view = &view.Data
		revision := view.Data.IntentRevision.Revision
		branch := view.Data.IntentRevision.BranchRef
		named.verified = branch != "" && branch == b.event.HeadRef
		switch {
		case !named.verified && branch == "":
			named.note = fmt.Sprintf("Trailer names Intent `%s`, which declares no branch, so Conn cannot confirm it belongs to this pull request; its review packet is not shown and collisions with it stay in the preview.", id)
		case !named.verified && b.event.HeadRef == "":
			named.note = fmt.Sprintf("Trailer names Intent `%s` on branch `%s`, but the event names no head branch to compare; its review packet is not shown and collisions with it stay in the preview.", id, text(branch))
		case !named.verified:
			named.note = fmt.Sprintf("Trailer names Intent `%s` on branch `%s`, not this pull request's head branch `%s`; its review packet is not shown and collisions with it stay in the preview.", id, text(branch), text(b.event.HeadRef))
		}
		if named.note != "" {
			continue
		}
		switch {
		case view.Data.Status != "active":
			named.note = fmt.Sprintf("Trailer names Intent `%s`, which is %s; its coordination no longer applies.", id, view.Data.Status)
		case named.trailer.LatestRevision < revision:
			named.note = fmt.Sprintf("Trailers name Intent `%s` revision %d, but its current revision is %d.", id, named.trailer.LatestRevision, revision)
		case named.trailer.LatestRevision > revision:
			named.note = fmt.Sprintf("Trailers name Intent `%s` revision %d, which does not exist; its current revision is %d.", id, named.trailer.LatestRevision, revision)
		}
	}
}

func (b *builder) previewCollisions(ctx context.Context, client *httpclient.Client) {
	if !b.analyzed {
		return
	}
	input := protocol.CollisionPreviewInput{Claims: b.observed.Claims, ExcludeIntentIDs: []uuid.UUID{}}
	var active []uuid.UUID
	for _, named := range b.named {
		if !named.verified {
			continue
		}
		input.ExcludeIntentIDs = append(input.ExcludeIntentIDs, named.trailer.IntentID)
		if named.view != nil && named.view.Status == "active" {
			active = append(active, named.trailer.IntentID)
		}
	}
	if len(active) == 1 {
		input.AsIntentID = &active[0]
	}
	if len(input.Claims) > protocol.MaxPreviewClaims {
		input.Claims = input.Claims[:protocol.MaxPreviewClaims]
		b.notes = append(b.notes, fmt.Sprintf("Only %d observed claims were previewed.", protocol.MaxPreviewClaims))
	}
	preview, err := client.PreviewCollisions(ctx, b.context.ProjectID, input)
	if err != nil {
		b.fail("Conn could not preview collisions: %s", describe(err))
		return
	}
	b.preview = &preview.Data
}

func (b *builder) packets(ctx context.Context, client *httpclient.Client) {
	now := time.Now
	if b.cfg.Now != nil {
		now = b.cfg.Now
	}
	git := review.ReadGit(ctx, b.cfg.Worktree, b.base)
	for _, named := range b.named {
		if named.view == nil || !named.verified {
			continue
		}
		records, err := review.LoadIndependent(ctx, client, b.context.ProjectID, named.trailer.IntentID)
		if err != nil {
			named.note = strings.TrimSpace(named.note + " " + fmt.Sprintf("Conn could not load its review packet: %s", describe(err)))
			continue
		}
		revision := records.Records.Intent()
		var observed []protocol.SemanticClaim
		if b.analyzed {
			status := protocol.AnalysisComplete
			codes := []string{}
			for _, diagnostic := range b.observed.Diagnostics {
				status = protocol.AnalysisIncomplete
				if len(codes) < 20 && !contains(codes, diagnostic.Code) {
					codes = append(codes, diagnostic.Code)
				}
			}
			analysis := protocol.WorkContextAnalysis{IntentRevision: revision.Revision, Status: status, ObservedAt: protocol.NewTimestamp(now()), DiagnosticCodes: codes}
			records.Records.Work.Focus.Analysis = &analysis
			observed = ciClaims(b.observed.Claims, b.context.ProjectID, revision, analysis.ObservedAt)
			records.Records.Work.KnownUnknowns = append(records.Records.Work.KnownUnknowns,
				fmt.Sprintf("Drift compares declared claims with analysis observed in CI on %s..%s, not with stored analysis.", short(b.base), short(b.event.Head)))
		} else {
			observed = []protocol.SemanticClaim{}
		}
		packet, err := review.BuildPacket(review.PacketInput{Records: records.Records, Git: git, Written: map[string]bool{}, Observed: observed, OwnerLogin: records.OwnerLogin})
		if err != nil {
			named.note = strings.TrimSpace(named.note + " Conn could not build its review packet.")
			continue
		}
		named.packet, named.markdown = &packet, review.RenderMarkdown(packet)
	}
}

// ciClaims turns CI-observed candidates into claims of the Intent revision
// for drift only. They are never stored.
func ciClaims(candidates []protocol.SemanticClaimCandidate, projectID uuid.UUID, revision protocol.IntentRevision, at protocol.Timestamp) []protocol.SemanticClaim {
	claims := make([]protocol.SemanticClaim, 0, len(candidates))
	for _, candidate := range candidates {
		claims = append(claims, protocol.SemanticClaim{
			ClaimID: uuid.NewV7(), ProjectID: projectID, IntentID: revision.IntentID, IntentRevision: revision.Revision,
			SubjectKind: candidate.SubjectKind, NormalizedSubject: candidate.NormalizedSubject, DisplayName: candidate.DisplayName,
			Relation: candidate.Relation, ProvenanceKind: "static_analysis", ObservedAt: at,
		})
	}
	return claims
}

// render builds the report within budget characters. Packets that do not
// fit are replaced by a note, last first.
func (b *builder) render(budget int) string {
	head := b.renderHead()
	var details []string
	for _, named := range b.named {
		if named.packet == nil {
			continue
		}
		details = append(details, fmt.Sprintf("<details>\n<summary>Review packet: Intent <code>%s</code> revision %d</summary>\n\n%s\n</details>\n",
			named.packet.Intent.ID, named.packet.Intent.Revision, named.markdown))
	}
	footer := "\n---\n\nConn reports findings and never fails this check for them. It wrote nothing to Conn and ran no project code.\n"
	for omitted := 0; ; omitted++ {
		var out strings.Builder
		out.WriteString(head)
		kept := details[:len(details)-omitted]
		for _, detail := range kept {
			out.WriteString("\n" + detail)
		}
		if omitted > 0 {
			fmt.Fprintf(&out, "\n%d review packet(s) were omitted to stay within the size limit; run `conn review` locally for them.\n", omitted)
		}
		out.WriteString(footer)
		if utf8.RuneCountInString(out.String()) <= budget || omitted == len(details) {
			text := out.String()
			if utf8.RuneCountInString(text) > budget {
				runes := []rune(text)
				text = string(runes[:budget-100]) + "\n\n[Conn truncated this report at its size limit.]\n"
			}
			return text
		}
	}
}

func (b *builder) renderHead() string {
	var out strings.Builder
	line := func(format string, values ...any) { fmt.Fprintf(&out, format+"\n", values...) }
	line("%s", Marker)
	line("## Conn check")
	line("")
	needs, failing := 0, 0
	for _, named := range b.named {
		if named.packet == nil {
			continue
		}
		for _, collision := range named.packet.Collisions {
			if collision.State == protocol.CollisionOpen || collision.State == protocol.CollisionUnderReview {
				needs++
			}
			if collision.LatestCombinationOutcome == protocol.CombinationFailing || collision.LatestCombinationOutcome == protocol.CombinationMergeConflict {
				failing++
			}
		}
	}
	previewed := 0
	if b.preview != nil {
		previewed = len(b.preview.Collisions)
	}
	line("| Intents | Collisions needing attention | Previewed collisions | Failing combination checks |")
	line("| --- | --- | --- | --- |")
	line("| %d | %d | %d | %d |", len(b.named), needs, previewed, failing)
	line("")
	if b.context.ProjectID != (uuid.UUID{}) {
		line("Project %s (`%s`), repository `%s`.", text(b.context.ProjectName), b.context.ProjectID, b.context.CanonicalRemote)
	}
	if b.base != "" {
		line("Pull request #%d: base `%s`, head `%s`.", b.event.Number, short(b.base), short(b.event.Head))
	}
	if len(b.errors) > 0 {
		line("")
		line("### Conn could not complete the check")
		line("")
		for _, message := range b.errors {
			line("- %s", text(message))
		}
	}
	var notes []string
	if b.base != "" && len(b.named) == 0 && len(b.errors) == 0 {
		notes = append(notes, "Unregistered work: no commit in this pull request names a Conn Intent with a `Conn-Intent: <intent-id>@<revision>` trailer.")
	}
	for _, named := range b.named {
		if named.note != "" {
			notes = append(notes, named.note)
		}
	}
	if b.trailerLog.Commits != nil && len(b.named) > 0 {
		unnamed := 0
		for _, commit := range b.trailerLog.Commits {
			found := false
			for _, named := range b.named {
				if contains(named.trailer.Commits, commit) {
					found = true
				}
			}
			if !found {
				unnamed++
			}
		}
		if unnamed > 0 {
			notes = append(notes, fmt.Sprintf("%d of %d commit(s) carry no Conn-Intent trailer.", unnamed, len(b.trailerLog.Commits)))
		}
	}
	notes = append(notes, b.notes...)
	if len(notes) > 0 {
		line("")
		line("### Notes")
		line("")
		for _, note := range notes {
			line("- %s", note)
		}
	}
	if b.preview != nil {
		line("")
		line("### Previewed collisions")
		line("")
		if len(b.preview.Collisions) == 0 {
			line("The changes observed in this pull request open no collision with other active Intents.")
		}
		for _, collision := range b.preview.Collisions {
			subjects := make([]string, 0, len(collision.MatchedSubjects))
			for _, subject := range collision.MatchedSubjects {
				subjects = append(subjects, "`"+subject+"`")
			}
			state := ""
			if collision.State != protocol.CollisionOpen {
				state = fmt.Sprintf(" (would inherit %s from decision `%s`)", collision.State, collision.DecisionRef)
			}
			line("- **%s** `%s` with Intent `%s` revision %d on %s%s", strings.ToUpper(collision.Severity), collision.RuleID,
				collision.Counterpart.IntentID, collision.Counterpart.Revision, strings.Join(subjects, ", "), state)
		}
		if b.preview.Truncated {
			line("- Additional previewed collisions were omitted.")
		}
		line("")
		line("Previewed collisions are advisory and were not recorded in Conn.")
	}
	return out.String()
}

func describe(err error) string {
	var apiErr *httpclient.APIError
	switch {
	case errors.Is(err, httpclient.ErrUnreachable):
		return "the Conn server is unreachable from this runner; GitHub-hosted runners need an HTTPS backend"
	case errors.As(err, &apiErr) && apiErr.Code == "unauthenticated":
		return "the CI credential is expired or revoked; ask a Project owner to create a new one"
	case errors.As(err, &apiErr):
		return apiErr.Message
	}
	return err.Error()
}

// text keeps untrusted text on one Markdown line.
func text(value string) string {
	return strings.NewReplacer("\n", " ", "\r", " ", "|", "\\|", "<", "&lt;", ">", "&gt;").Replace(value)
}

func short(commit string) string {
	if len(commit) > 12 {
		return commit[:12]
	}
	return commit
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
