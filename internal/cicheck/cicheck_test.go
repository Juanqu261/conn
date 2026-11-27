package cicheck

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"uuid"

	"github.com/Juanqu261/conn/internal/protocol"
	"github.com/Juanqu261/conn/internal/review"
	"github.com/Juanqu261/conn/internal/secret"
)

// TestCommentIsFoundByMarkerAndReportsStayBounded protects requirement 5:
// the check updates the one comment that carries its marker instead of
// adding another, keeps the comment under 65,536 characters and the summary
// under 1 MiB by omitting whole packets, and skips cleanly without a token.
//
// It also reproduces a defect: the comment was found by its marker alone, so
// a participant who posted the marker first received the report in their own
// editable comment. Only the workflow token's comments are updated now.
func TestCommentIsFoundByMarkerAndReportsStayBounded(t *testing.T) {
	var mu sync.Mutex
	decoy := "Planted by a participant " + Marker
	comments := map[int64]string{1: "A human comment.", 2: decoy}
	authors := map[int64]string{1: "alice", 2: "mallory"}
	next := int64(3)
	var authorizations []string
	github := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		authorizations = append(authorizations, r.Header.Get("Authorization"))
		var body struct {
			Body string `json:"body"`
		}
		data, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(data, &body)
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/shop/issues/7/comments":
			list := []map[string]any{}
			for id, text := range comments {
				list = append(list, map[string]any{"id": id, "body": text, "user": map[string]any{"login": authors[id]}})
			}
			_ = json.NewEncoder(w).Encode(list)
		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/shop/issues/7/comments":
			comments[next], authors[next] = body.Body, CommentAuthor
			next++
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/repos/acme/shop/issues/comments/"):
			for id := range comments {
				if r.URL.Path == "/repos/acme/shop/issues/comments/"+itoa(id) {
					comments[id] = body.Body
				}
			}
			_, _ = w.Write([]byte(`{}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer github.Close()
	client := &GitHubClient{BaseURL: github.URL}
	token := secret.New("gh-comment-token")
	for i, body := range []string{Marker + "\nfirst", Marker + "\nsecond"} {
		if err := client.UpsertComment(t.Context(), token, "acme", "shop", 7, body); err != nil {
			t.Fatalf("upsert %d: %v", i, err)
		}
	}
	if len(comments) != 3 || comments[1] != "A human comment." || comments[2] != decoy || comments[3] != Marker+"\nsecond" {
		t.Fatalf("comments = %v", comments)
	}
	for _, header := range authorizations {
		if header != "Bearer gh-comment-token" {
			t.Fatalf("authorization = %q", header)
		}
	}

	// Three maximal packets exceed the comment limit; whole packets are
	// omitted and the report says so.
	b := &builder{event: Event{Number: 7}, base: strings.Repeat("a", 40)}
	for range 3 {
		packet := protocol.ReviewPacket{Intent: protocol.ReviewIntent{ID: uuid.NewV7(), Revision: 1}}
		b.named = append(b.named, &namedIntent{trailer: review.IntentTrailers{IntentID: packet.Intent.ID}, packet: &packet, markdown: strings.Repeat("x", review.MaxMarkdownChars-1)})
	}
	comment := b.render(commentBudget)
	if utf8.RuneCountInString(comment) >= MaxCommentChars || !strings.Contains(comment, "review packet(s) were omitted") || !strings.HasPrefix(comment, Marker) {
		t.Fatalf("comment has %d characters", utf8.RuneCountInString(comment))
	}
	if summary := b.render(summaryBudget); len(summary) >= MaxSummaryBytes || strings.Contains(summary, "omitted to stay within") {
		t.Fatalf("summary has %d bytes", len(summary))
	}

	outcome := Run(t.Context(), Config{LookupEnv: func(string) (string, bool) { return "", false }, Log: io.Discard})
	if !outcome.Skipped || !strings.Contains(outcome.Markdown, SkippedMessage) {
		t.Fatalf("run without a token = %+v", outcome)
	}
}

func itoa(n int64) string {
	data, _ := json.Marshal(n)
	return string(data)
}
