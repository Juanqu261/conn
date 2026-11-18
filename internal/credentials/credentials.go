// Package credentials resolves the local Conn session token.
// The token comes from CONN_TOKEN when set, otherwise from the
// operating-system credential store. There is no plaintext fallback.
package credentials

import (
	"errors"
	"fmt"
	"regexp"
	"sync"

	"github.com/zalando/go-keyring"

	"github.com/Juanqu261/conn/internal/secret"
)

// TokenEnv overrides the stored credential without persisting it.
const TokenEnv = "CONN_TOKEN"

var (
	ErrNotFound    = errors.New("no Conn credential is stored")
	ErrUnavailable = errors.New("the operating-system credential store is unavailable")
	ErrNotLoggedIn = errors.New("not logged in")
	ErrInvalid     = errors.New("credential is not a Conn session token")
)

var tokenPattern = regexp.MustCompile(`^conn_sess_[A-Za-z0-9_-]{43}$`)

// ValidToken reports whether token has the Conn session token format.
func ValidToken(token secret.String) bool { return tokenPattern.MatchString(token.Reveal()) }

// Store holds at most one Conn session token for one server.
type Store interface {
	Get() (secret.String, error) // ErrNotFound when empty
	Set(token secret.String) error
	Delete() error // nil when already empty
}

// Source names where a resolved credential came from.
type Source string

const (
	SourceEnv   Source = "CONN_TOKEN environment variable"
	SourceStore Source = "operating-system credential store"
)

// Resolve returns CONN_TOKEN when set, then the stored token. It never
// writes CONN_TOKEN to the store.
func Resolve(lookupEnv func(string) (string, bool), store Store) (secret.String, Source, error) {
	if value, ok := lookupEnv(TokenEnv); ok && value != "" {
		token := secret.New(value)
		if !ValidToken(token) {
			return secret.String{}, SourceEnv, fmt.Errorf("%s: %w", TokenEnv, ErrInvalid)
		}
		return token, SourceEnv, nil
	}
	token, err := store.Get()
	if errors.Is(err, ErrNotFound) {
		return secret.String{}, "", ErrNotLoggedIn
	}
	if err != nil {
		return secret.String{}, SourceStore, err
	}
	if !ValidToken(token) {
		return secret.String{}, SourceStore, fmt.Errorf("stored credential: %w", ErrInvalid)
	}
	return token, SourceStore, nil
}

// Keyring stores the token in the operating-system credential store: Windows
// Credential Manager, macOS Keychain, or Linux Secret Service. Each server
// URL has its own entry, so development and production sessions never mix.
type Keyring struct {
	account string
}

const keyringService = "conn"

// NewKeyring returns the store entry for serverURL.
func NewKeyring(serverURL string) Keyring { return Keyring{account: serverURL} }

func (k Keyring) Get() (secret.String, error) {
	value, err := keyring.Get(keyringService, k.account)
	if errors.Is(err, keyring.ErrNotFound) {
		return secret.String{}, ErrNotFound
	}
	if err != nil {
		return secret.String{}, unavailable(err)
	}
	return secret.New(value), nil
}

func (k Keyring) Set(token secret.String) error {
	if err := keyring.Set(keyringService, k.account, token.Reveal()); err != nil {
		return unavailable(err)
	}
	return nil
}

func (k Keyring) Delete() error {
	err := keyring.Delete(keyringService, k.account)
	if err == nil || errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return unavailable(err)
}

// unavailable keeps the library error for diagnostics. Library errors never
// contain the stored value.
func unavailable(err error) error { return fmt.Errorf("%w: %v", ErrUnavailable, err) }

// Memory is an in-process Store for tests.
type Memory struct {
	mu    sync.Mutex
	token *secret.String
	// Err, when set, makes every operation fail as an unavailable store.
	Err error
}

func (m *Memory) Get() (secret.String, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return secret.String{}, unavailable(m.Err)
	}
	if m.token == nil {
		return secret.String{}, ErrNotFound
	}
	return *m.token, nil
}

func (m *Memory) Set(token secret.String) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return unavailable(m.Err)
	}
	m.token = &token
	return nil
}

func (m *Memory) Delete() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return unavailable(m.Err)
	}
	m.token = nil
	return nil
}
