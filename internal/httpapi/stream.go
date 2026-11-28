package httpapi

import (
	"bufio"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/Juanqu261/conn/internal/application"
	"github.com/Juanqu261/conn/internal/eventstream"
	"github.com/Juanqu261/conn/internal/protocol"
)

const (
	// streamBacklog bounds the events one connect replays; beyond it the
	// stream sends resync and the browser reloads its read models.
	streamBacklog   = 1000
	streamPage      = 100
	streamHeartbeat = 25 * time.Second
	streamRetryMS   = 5000
)

// registerStreamRoute serves the Conn Lab event stream (decision 0013):
// committed Project events as Server-Sent Events in cursor order, resumed
// after max(after, Last-Event-ID). Only browser sessions may open it; local
// clients keep pull-by-cursor. Every page re-reads through the same
// membership check as GET /events, and the session is re-checked at each
// heartbeat, so sign-out ends the stream within one interval.
func registerStreamRoute(mux *http.ServeMux, cfg Config) {
	log, projects, streams := cfg.Log, cfg.Projects, cfg.Web.Streams
	backlog := cfg.Web.StreamBacklog
	if backlog < 1 {
		backlog = streamBacklog
	}

	mux.HandleFunc("GET /v1/projects/{project_id}/events/stream", authenticated(cfg.Auth, log, func(w http.ResponseWriter, r *http.Request, s application.Session) {
		if s.Kind != application.SessionBrowser {
			writeError(w, log, http.StatusForbidden, "browser_only",
				"The event stream serves Conn Lab browsers. Other clients read GET /events by cursor.")
			return
		}
		if streams == nil {
			writeError(w, log, http.StatusServiceUnavailable, "stream_unavailable",
				"This Conn server does not stream events. Conn Lab falls back to reading them by cursor.")
			return
		}
		projectID, ok := pathID(w, r, log, "project_id")
		if !ok || !allowQueryParameters(w, r, log, "after") {
			return
		}
		zero := int64(0)
		cursor, ok := queryInt(w, r, log, "after", 0, 9007199254740991, &zero)
		if !ok {
			return
		}
		if last, err := strconv.ParseInt(r.Header.Get("Last-Event-ID"), 10, 64); err == nil && last > cursor && last <= 9007199254740991 {
			cursor = last
		}

		// Authorize and measure the backlog before any stream byte is sent,
		// so a non-member gets the ordinary error response.
		head, err := projects.Events(r.Context(), s.Human.HumanID, projectID, cursor, 1)
		if err != nil {
			writeProblem(w, log, err)
			return
		}
		control := http.NewResponseController(w)
		out := bufio.NewWriter(w)
		send := func() bool {
			return out.Flush() == nil && control.Flush() == nil
		}
		begin := func() {
			// The server's write timeout suits ordinary requests, not a stream.
			_ = control.SetWriteDeadline(time.Time{})
			header := w.Header()
			header.Set("Content-Type", "text/event-stream; charset=utf-8")
			header.Set("Cache-Control", "no-store")
			header.Set("X-Accel-Buffering", "no")
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(out, "retry: %d\n\n", streamRetryMS)
		}

		// A resync needs no stream slot: it is one message, then the end.
		if head.AsOfEvent-cursor > backlog {
			data, err := protocol.Encode(protocol.WebStreamResync{AsOfEvent: head.AsOfEvent})
			if err != nil {
				internalError(w, log, "stream resync", err)
				return
			}
			begin()
			fmt.Fprintf(out, "event: resync\ndata: %s\n\n", data)
			send()
			return
		}

		sub, err := streams.Subscribe(projectID, s.SessionID)
		if errors.Is(err, eventstream.ErrSessionLimit) || errors.Is(err, eventstream.ErrInstanceLimit) {
			w.Header().Set("Retry-After", "30")
			writeError(w, log, http.StatusTooManyRequests, "too_many_streams", err.Error()+". Close another Conn Lab tab.")
			return
		}
		if err != nil {
			internalError(w, log, "subscribe", err)
			return
		}
		defer sub.Close()
		begin()
		if !send() {
			return
		}

		token, _ := browserSessionCookie(r)
		heartbeat := time.NewTicker(streamHeartbeat)
		defer heartbeat.Stop()
		expiry := time.NewTimer(time.Until(s.ExpiresAt))
		defer expiry.Stop()
		for {
			for {
				page, err := projects.Events(r.Context(), s.Human.HumanID, projectID, cursor, streamPage)
				if err != nil {
					if r.Context().Err() == nil {
						log.Warn("event stream read failed", "error", err)
					}
					return
				}
				for _, event := range page.Data {
					data, err := protocol.Encode(event)
					if err != nil {
						log.Error("stream event failed schema validation", "error", err)
						return
					}
					// Encoded JSON has no raw newline, so one data line holds it.
					fmt.Fprintf(out, "id: %d\ndata: %s\n\n", event.EventSequence, data)
					cursor = event.EventSequence
				}
				if !send() {
					return
				}
				if len(page.Data) < streamPage {
					break
				}
			}
			select {
			case <-r.Context().Done():
				return
			case <-expiry.C:
				return
			case <-sub.Wake():
			case <-heartbeat.C:
				if _, err := cfg.Auth.AuthenticateBrowser(r.Context(), token); err != nil {
					return
				}
				fmt.Fprint(out, ": heartbeat\n\n")
				if !send() {
					return
				}
			}
		}
	}))
}
