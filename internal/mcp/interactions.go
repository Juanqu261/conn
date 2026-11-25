package mcp

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"sync"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Juanqu261/conn/internal/account"
	"github.com/Juanqu261/conn/internal/protocol"
)

const (
	maxPendingInteractions = 8
	loginInteractionTTL    = 10 * time.Minute
	logoutInteractionTTL   = 5 * time.Minute
)

type interactionKind string

const (
	loginInteraction  interactionKind = "login"
	logoutInteraction interactionKind = "logout"
)

type pendingInteraction struct {
	kind        interactionKind
	session     *sdk.ServerSession
	expiresAt   time.Time
	login       *account.LoginAttempt
	logoutHuman protocol.Human
	timer       *time.Timer
}

type interactionStore struct {
	mu      sync.Mutex
	account *account.Service
	entries map[string]*pendingInteraction
	now     func() time.Time
}

func newInteractionStore(service *account.Service) *interactionStore {
	return &interactionStore{account: service, entries: make(map[string]*pendingInteraction), now: time.Now}
}

func (s *interactionStore) addLogin(session *sdk.ServerSession, attempt *account.LoginAttempt) (string, error) {
	expiresAt := s.now().Add(loginInteractionTTL)
	if authorization := attempt.Authorization(); authorization.ExpiresAt.After(s.now()) && authorization.ExpiresAt.Before(expiresAt) {
		expiresAt = authorization.ExpiresAt
	}
	return s.add(&pendingInteraction{kind: loginInteraction, session: session, expiresAt: expiresAt, login: attempt})
}

func (s *interactionStore) addLogout(session *sdk.ServerSession, human protocol.Human) (string, error) {
	return s.add(&pendingInteraction{kind: logoutInteraction, session: session, expiresAt: s.now().Add(logoutInteractionTTL), logoutHuman: human})
}

func (s *interactionStore) add(pending *pendingInteraction) (string, error) {
	state, err := randomInteractionState()
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.removeExpiredLocked(s.now())
	if len(s.entries) >= maxPendingInteractions {
		return "", errors.New("too many interactive Conn operations are pending; finish or cancel one and retry")
	}
	s.entries[state] = pending
	delay := time.Until(pending.expiresAt)
	if delay < 0 {
		delay = 0
	}
	pending.timer = time.AfterFunc(delay, func() { s.expire(state, pending) })
	return state, nil
}

func (s *interactionStore) take(state string, session *sdk.ServerSession, kind interactionKind) (*pendingInteraction, error) {
	if state == "" {
		return nil, errors.New("interactive request state is missing")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	pending := s.entries[state]
	if pending == nil {
		return nil, errors.New("interactive request expired or is no longer active; start again")
	}
	if pending.session != session || pending.kind != kind {
		return nil, errors.New("interactive request does not belong to this MCP session")
	}
	delete(s.entries, state)
	if pending.timer != nil {
		pending.timer.Stop()
	}
	if !s.now().Before(pending.expiresAt) {
		s.cancelLocked(pending)
		return nil, errors.New("interactive request expired; start again")
	}
	return pending, nil
}

func (s *interactionStore) expire(state string, expected *pendingInteraction) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.entries[state] != expected {
		return
	}
	delete(s.entries, state)
	s.cancelLocked(expected)
}

func (s *interactionStore) removeExpiredLocked(now time.Time) {
	for state, pending := range s.entries {
		if !now.Before(pending.expiresAt) {
			delete(s.entries, state)
			if pending.timer != nil {
				pending.timer.Stop()
			}
			s.cancelLocked(pending)
		}
	}
}

func (s *interactionStore) cancelLocked(pending *pendingInteraction) {
	if pending.login != nil && s.account != nil {
		s.account.CancelLogin(pending.login)
		pending.login = nil
	}
}

func (s *interactionStore) clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for state, pending := range s.entries {
		delete(s.entries, state)
		if pending.timer != nil {
			pending.timer.Stop()
		}
		s.cancelLocked(pending)
	}
}

func randomInteractionState() (string, error) {
	var data [24]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "", errors.New("could not create interactive request state")
	}
	return base64.RawURLEncoding.EncodeToString(data[:]), nil
}
