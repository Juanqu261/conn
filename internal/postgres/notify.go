package postgres

import (
	"context"
	"log/slog"
	"strings"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5/pgxpool"
)

// EventChannel is the PostgreSQL notification channel on which each
// committed event announces "<project_id>:<event_sequence>" (decision 0013).
const EventChannel = "conn_events"

const (
	listenRetryMin = time.Second
	listenRetryMax = 30 * time.Second
)

// EventWaker receives event notifications. Notify wakes the streams of one
// Project; WakeAll wakes every stream after the listener (re)connects, when
// notifications may have been missed.
type EventWaker interface {
	Notify(projectID uuid.UUID)
	WakeAll()
}

// ListenEvents holds one pooled connection on EventChannel until ctx ends
// and hands every notification to waker. On a lost connection it wakes all
// streams so they reread from their cursors, then reconnects with backoff.
// Notifications only wake readers; cursors in the events table remain the
// source of truth, so a missed notification delays and never loses events.
func ListenEvents(ctx context.Context, pool *pgxpool.Pool, waker EventWaker, log *slog.Logger) {
	retry := listenRetryMin
	for ctx.Err() == nil {
		err := listenOnce(ctx, pool, waker, func() { retry = listenRetryMin })
		if ctx.Err() != nil {
			return
		}
		log.Warn("event listener disconnected; retrying", "error", err, "retry_in", retry.String())
		waker.WakeAll()
		timer := time.NewTimer(retry)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		retry = min(retry*2, listenRetryMax)
	}
}

func listenOnce(ctx context.Context, pool *pgxpool.Pool, waker EventWaker, connected func()) error {
	pooled, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	// A LISTEN connection must never return to the pool still listening, so
	// it leaves the pool and is closed when listening ends.
	conn := pooled.Hijack()
	defer conn.Close(context.Background())

	if _, err := conn.Exec(ctx, "LISTEN "+EventChannel); err != nil {
		return err
	}
	connected()
	waker.WakeAll()
	for {
		notification, err := conn.WaitForNotification(ctx)
		if err != nil {
			return err
		}
		project, _, _ := strings.Cut(notification.Payload, ":")
		if projectID, err := uuid.Parse(project); err == nil {
			waker.Notify(projectID)
		}
	}
}
