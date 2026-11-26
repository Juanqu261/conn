// Package hookstate holds the small, non-authoritative local state that the
// Conn hooks and the MCP adapter share. It lives under the worktree's Git
// directory, so Git never tracks it and each linked worktree has its own
// copy. It stores identifiers, cursors, subjects, and repository-relative
// paths only: never a credential, prompt, source, or Project prose. Every
// reader treats a missing or corrupt file as empty, and the backend remains
// the authority for every reported fact.
package hookstate

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"uuid"

	"github.com/Juanqu261/conn/internal/repositorygit"
)

// File names and bounds.
const (
	FocusFile       = "focus.json"
	IndexFile       = "index.json"
	PendingFile     = "pending-warnings.json"
	WrittenFile     = "written.jsonl"
	DiagnosticsFile = "diagnostics.jsonl"
	lockFile        = ".lock"

	FocusVersion       = 1
	MaxKnownCollisions = 100
	MaxIndexPaths      = 10_000
	MaxPendingWarnings = 20
	MaxWrittenEntries  = 5_000
	MaxDiagnostics     = 100

	lockTimeout   = 200 * time.Millisecond
	staleLockAge  = 10 * time.Second
	maxStateBytes = 16 << 20
)

// ErrLocked means another process held the state lock for longer than the
// lock timeout. Callers skip the write.
var ErrLocked = errors.New("the Conn hook state is locked")

// Focus names the Intent that belongs to this worktree. It grants no access.
type Focus struct {
	Version         int               `json:"version"`
	ServerURL       string            `json:"server_url,omitempty"`
	ProjectID       *uuid.UUID        `json:"project_id,omitempty"`
	RepositoryID    *uuid.UUID        `json:"repository_id,omitempty"`
	IntentID        *uuid.UUID        `json:"intent_id,omitempty"`
	IntentRevision  int               `json:"intent_revision,omitempty"`
	ParticipantID   *uuid.UUID        `json:"participant_id,omitempty"`
	LastEvent       int64             `json:"last_event,omitempty"`
	KnownCollisions map[string]string `json:"known_collisions,omitempty"`
	// KnownPairs maps "rule|counterpart Intent ID" to the last known state,
	// so a collision that continues under a new identity after a revision is
	// not reported as new.
	KnownPairs map[string]string `json:"known_pairs,omitempty"`
	// RecommendedSession is the last Kiro session that received the
	// /conn-start recommendation, so it is printed once per session.
	RecommendedSession string    `json:"recommended_session,omitempty"`
	UpdatedAt          time.Time `json:"updated_at"`
}

// HasIntent reports whether the focus names an Intent.
func (f Focus) HasIntent() bool { return f.IntentID != nil && f.ProjectID != nil }

// Index maps repository-relative paths to active claims and collisions. It
// is refreshed from Radar and used only to decide whether to warn.
type Index struct {
	ProjectID       *uuid.UUID `json:"project_id,omitempty"`
	RepositoryID    *uuid.UUID `json:"repository_id,omitempty"`
	CanonicalRemote string     `json:"canonical_remote,omitempty"`
	Connected       bool       `json:"connected"`
	// CheckedAt is when the repository connection was last checked;
	// FetchedAt is when Paths were last refreshed from Radar.
	CheckedAt time.Time             `json:"checked_at"`
	AsOfEvent int64                 `json:"as_of_event"`
	FetchedAt time.Time             `json:"fetched_at"`
	Paths     map[string]IndexEntry `json:"paths"`
}

// IndexEntry lists the claims and collisions whose evidence cites a path.
type IndexEntry struct {
	Claims     []IndexClaim     `json:"claims,omitempty"`
	Collisions []IndexCollision `json:"collisions,omitempty"`
}

// IndexClaim is one active claim with evidence at a path.
type IndexClaim struct {
	IntentID       uuid.UUID `json:"intent_id"`
	IntentRevision int       `json:"intent_revision"`
	ParticipantID  uuid.UUID `json:"participant_id"`
	Subject        string    `json:"subject"`
	Relation       string    `json:"relation"`
}

// IndexCollision is one open or under-review collision whose matching
// claims cite a path.
type IndexCollision struct {
	CollisionID   uuid.UUID `json:"collision_id"`
	RuleID        string    `json:"rule_id"`
	State         string    `json:"state"`
	LeftIntentID  uuid.UUID `json:"left_intent_id"`
	RightIntentID uuid.UUID `json:"right_intent_id"`
}

// Warning is one pre-write warning not yet delivered to the agent.
type Warning struct {
	Path      string    `json:"path,omitempty"`
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"created_at"`
}

// Written records one path a Kiro session wrote. It never records content.
type Written struct {
	Path      string    `json:"path"`
	SessionID string    `json:"session_id,omitempty"`
	At        time.Time `json:"at"`
}

// Diagnostic records one silent hook failure for conn doctor.
type Diagnostic struct {
	Code string    `json:"code"`
	Hook string    `json:"hook"`
	At   time.Time `json:"at"`
}

// Store reads and writes one worktree's hook state directory.
type Store struct {
	Dir string
	Now func() time.Time
}

// Open resolves the hook state directory under the Git directory of the
// worktree that contains dir. It creates nothing.
func Open(ctx context.Context, dir string) (Store, error) {
	gitDir, err := repositorygit.GitDir(ctx, dir)
	if err != nil {
		return Store{}, err
	}
	return Store{Dir: filepath.Join(filepath.Clean(filepath.FromSlash(gitDir)), "conn")}, nil
}

func (s Store) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

// Focus reads the focus record, or an empty one.
func (s Store) Focus() Focus {
	var focus Focus
	if !s.readJSON(FocusFile, &focus) || focus.Version != FocusVersion {
		return Focus{Version: FocusVersion}
	}
	return focus
}

// UpdateFocus applies change to the current focus record and writes it.
func (s Store) UpdateFocus(change func(*Focus)) error {
	return s.locked(func() error {
		focus := s.Focus()
		change(&focus)
		focus.Version, focus.UpdatedAt = FocusVersion, s.now()
		focus.KnownCollisions = boundedMap(focus.KnownCollisions)
		focus.KnownPairs = boundedMap(focus.KnownPairs)
		return s.writeJSON(FocusFile, focus)
	})
}

func boundedMap(values map[string]string) map[string]string {
	if len(values) <= MaxKnownCollisions {
		return values
	}
	trimmed := make(map[string]string, MaxKnownCollisions)
	for key, value := range values {
		if len(trimmed) == MaxKnownCollisions {
			break
		}
		trimmed[key] = value
	}
	return trimmed
}

// PairKey identifies a collision by rule and counterpart Intent.
func PairKey(ruleID string, counterpart uuid.UUID) string { return ruleID + "|" + counterpart.String() }

// ClearIntent removes the Intent fields when the focus names intentID.
func (s Store) ClearIntent(intentID uuid.UUID) error {
	if focus := s.Focus(); focus.IntentID == nil || *focus.IntentID != intentID {
		return nil
	}
	return s.UpdateFocus(func(f *Focus) {
		if f.IntentID != nil && *f.IntentID == intentID {
			f.IntentID, f.IntentRevision, f.ParticipantID = nil, 0, nil
			f.KnownCollisions, f.KnownPairs, f.LastEvent = nil, nil, 0
		}
	})
}

// Index reads the path index, or an empty one.
func (s Store) Index() Index {
	var index Index
	if !s.readJSON(IndexFile, &index) || index.Paths == nil {
		return Index{Paths: map[string]IndexEntry{}}
	}
	return index
}

// WriteIndex replaces the path index, bounded to MaxIndexPaths paths.
func (s Store) WriteIndex(index Index) error {
	if len(index.Paths) > MaxIndexPaths {
		bounded := make(map[string]IndexEntry, MaxIndexPaths)
		for path, entry := range index.Paths {
			if len(bounded) == MaxIndexPaths {
				break
			}
			bounded[path] = entry
		}
		index.Paths = bounded
	}
	return s.locked(func() error { return s.writeJSON(IndexFile, index) })
}

// QueueWarnings appends undelivered warnings, keeping the newest
// MaxPendingWarnings and dropping exact duplicates.
func (s Store) QueueWarnings(warnings []Warning) error {
	if len(warnings) == 0 {
		return nil
	}
	return s.locked(func() error {
		var pending []Warning
		s.readJSON(PendingFile, &pending)
		seen := make(map[string]bool, len(pending))
		for _, warning := range pending {
			seen[warning.Text] = true
		}
		for _, warning := range warnings {
			if !seen[warning.Text] {
				seen[warning.Text] = true
				pending = append(pending, warning)
			}
		}
		if len(pending) > MaxPendingWarnings {
			pending = pending[len(pending)-MaxPendingWarnings:]
		}
		return s.writeJSON(PendingFile, pending)
	})
}

// TakeWarnings returns and clears the pending warnings. When the lock is not
// acquired it returns nothing, so a warning is never delivered twice.
func (s Store) TakeWarnings() []Warning {
	var pending []Warning
	err := s.locked(func() error {
		s.readJSON(PendingFile, &pending)
		if len(pending) == 0 {
			return nil
		}
		return s.writeJSON(PendingFile, []Warning{})
	})
	if err != nil {
		return nil
	}
	return pending
}

// AppendWritten records written paths, keeping the newest MaxWrittenEntries.
func (s Store) AppendWritten(entries []Written) error {
	if len(entries) == 0 {
		return nil
	}
	return s.locked(func() error { return s.appendLines(WrittenFile, entries, MaxWrittenEntries) })
}

// WrittenPaths returns the recorded written paths, oldest first.
func (s Store) WrittenPaths() []Written {
	return readLines[Written](s.path(WrittenFile))
}

// RecordDiagnostic appends one diagnostic code, keeping the newest
// MaxDiagnostics. It never fails the caller.
func (s Store) RecordDiagnostic(hook, code string) {
	_ = s.locked(func() error {
		return s.appendLines(DiagnosticsFile, []Diagnostic{{Code: code, Hook: hook, At: s.now()}}, MaxDiagnostics)
	})
}

// Diagnostics returns recorded diagnostics, oldest first.
func (s Store) Diagnostics() []Diagnostic {
	return readLines[Diagnostic](s.path(DiagnosticsFile))
}

func (s Store) path(name string) string { return filepath.Join(s.Dir, name) }

func (s Store) readJSON(name string, target any) bool {
	data, err := readBounded(s.path(name))
	if err != nil {
		return false
	}
	return json.Unmarshal(data, target) == nil
}

// writeJSON writes a temporary file and renames it over the target, so a
// reader sees either the old or the new document.
func (s Store) writeJSON(name string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return s.replace(name, data)
}

func (s Store) replace(name string, data []byte) error {
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(s.Dir, name+".*.tmp")
	if err != nil {
		return err
	}
	_, writeErr := temporary.Write(data)
	closeErr := temporary.Close()
	if writeErr != nil || closeErr != nil {
		_ = os.Remove(temporary.Name())
		return errors.Join(writeErr, closeErr)
	}
	if err := os.Rename(temporary.Name(), s.path(name)); err != nil {
		_ = os.Remove(temporary.Name())
		return err
	}
	return nil
}

func (s Store) appendLines(name string, entries any, limit int) error {
	var existing []json.RawMessage
	for _, line := range readLines[json.RawMessage](s.path(name)) {
		existing = append(existing, line)
	}
	data, err := json.Marshal(entries)
	if err != nil {
		return err
	}
	var added []json.RawMessage
	if err := json.Unmarshal(data, &added); err != nil {
		return err
	}
	lines := append(existing, added...)
	if len(lines) > limit {
		lines = lines[len(lines)-limit:]
	}
	var buffer bytes.Buffer
	for _, line := range lines {
		buffer.Write(line)
		buffer.WriteByte('\n')
	}
	return s.replace(name, buffer.Bytes())
}

func readLines[T any](path string) []T {
	data, err := readBounded(path)
	if err != nil {
		return nil
	}
	var result []T
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for scanner.Scan() {
		var item T
		if json.Unmarshal(scanner.Bytes(), &item) == nil {
			result = append(result, item)
		}
	}
	return result
}

func readBounded(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxStateBytes {
		return nil, fs.ErrInvalid
	}
	return os.ReadFile(path)
}

// locked runs fn while holding the state lock file. The lock is acquired by
// exclusive creation within lockTimeout; a lock older than staleLockAge is
// treated as abandoned by a killed process.
func (s Store) locked(fn func() error) error {
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	lock := s.path(lockFile)
	deadline := time.Now().Add(lockTimeout)
	for {
		file, err := os.OpenFile(lock, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			_ = file.Close()
			break
		}
		if !errors.Is(err, fs.ErrExist) {
			return err
		}
		if info, statErr := os.Stat(lock); statErr == nil && time.Since(info.ModTime()) > staleLockAge {
			_ = os.Remove(lock)
			continue
		}
		if time.Now().After(deadline) {
			return ErrLocked
		}
		time.Sleep(10 * time.Millisecond)
	}
	defer os.Remove(lock)
	return fn()
}
