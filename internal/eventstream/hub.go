// Package eventstream fans committed-event wake-ups out to the Conn Lab
// browser streams of one server instance and bounds how many are open
// (decision 0013). A wake-up carries no event data: each stream rereads
// its Project's events after its own cursor, so cursors stay the source of
// truth and a coalesced or missed wake-up can delay but never lose events.
package eventstream

import (
	"errors"
	"sync"
	"uuid"
)

// Default limits from decision 0013.
const (
	DefaultPerSession = 4
	DefaultTotal      = 500
)

var (
	// ErrSessionLimit means the browser session already holds its maximum
	// number of streams.
	ErrSessionLimit = errors.New("this browser session has too many open streams")
	// ErrInstanceLimit means the server instance holds its maximum.
	ErrInstanceLimit = errors.New("the server has too many open streams")
)

// Hub tracks open streams. The zero value is not usable; call New.
type Hub struct {
	perSession int
	total      int

	mu        sync.Mutex
	open      int
	bySession map[uuid.UUID]int
	byProject map[uuid.UUID]map[*Subscription]struct{}
}

// New returns a hub with the given limits; a value below 1 selects the
// default.
func New(perSession, total int) *Hub {
	if perSession < 1 {
		perSession = DefaultPerSession
	}
	if total < 1 {
		total = DefaultTotal
	}
	return &Hub{
		perSession: perSession,
		total:      total,
		bySession:  map[uuid.UUID]int{},
		byProject:  map[uuid.UUID]map[*Subscription]struct{}{},
	}
}

// Subscription is one open stream.
type Subscription struct {
	hub       *Hub
	projectID uuid.UUID
	sessionID uuid.UUID
	wake      chan struct{}
	once      sync.Once
}

// Subscribe opens a stream for projectID on behalf of sessionID, or
// returns ErrSessionLimit or ErrInstanceLimit.
func (h *Hub) Subscribe(projectID, sessionID uuid.UUID) (*Subscription, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.open >= h.total {
		return nil, ErrInstanceLimit
	}
	if h.bySession[sessionID] >= h.perSession {
		return nil, ErrSessionLimit
	}
	sub := &Subscription{hub: h, projectID: projectID, sessionID: sessionID, wake: make(chan struct{}, 1)}
	h.open++
	h.bySession[sessionID]++
	if h.byProject[projectID] == nil {
		h.byProject[projectID] = map[*Subscription]struct{}{}
	}
	h.byProject[projectID][sub] = struct{}{}
	return sub, nil
}

// Wake returns a channel that receives after new events may exist. Several
// wake-ups before a read coalesce into one.
func (s *Subscription) Wake() <-chan struct{} { return s.wake }

// Close releases the stream's slot. It is safe to call more than once.
func (s *Subscription) Close() {
	s.once.Do(func() {
		h := s.hub
		h.mu.Lock()
		defer h.mu.Unlock()
		h.open--
		if h.bySession[s.sessionID]--; h.bySession[s.sessionID] <= 0 {
			delete(h.bySession, s.sessionID)
		}
		delete(h.byProject[s.projectID], s)
		if len(h.byProject[s.projectID]) == 0 {
			delete(h.byProject, s.projectID)
		}
	})
}

// Notify wakes every stream of projectID.
func (h *Hub) Notify(projectID uuid.UUID) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for sub := range h.byProject[projectID] {
		sub.signal()
	}
}

// WakeAll wakes every open stream.
func (h *Hub) WakeAll() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, subs := range h.byProject {
		for sub := range subs {
			sub.signal()
		}
	}
}

func (s *Subscription) signal() {
	select {
	case s.wake <- struct{}{}:
	default: // a wake-up is already pending
	}
}
