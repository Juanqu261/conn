// Package protocol holds Conn's v1 protocol types. They are handwritten Go
// mirrors of the canonical JSON Schemas under contracts/v1

package protocol

import (
	"fmt"
	"time"
)

// Timestamp is an RFC 3339 UTC instant with at most microsecond precision,
// which PostgreSQL stores without loss.
type Timestamp struct {
	t time.Time
}

const timestampLayout = "2006-01-02T15:04:05.999999Z07:00"

// NewTimestamp converts t to UTC and truncates it to microseconds.
func NewTimestamp(t time.Time) Timestamp {
	return Timestamp{t: t.UTC().Truncate(time.Microsecond)}
}

// Time returns the instant in UTC.
func (t Timestamp) Time() time.Time { return t.t }

// IsZero reports whether the timestamp is unset.
func (t Timestamp) IsZero() bool { return t.t.IsZero() }

func (t Timestamp) String() string { return t.t.Format(timestampLayout) }

// MarshalText encodes the canonical form, for example 2026-11-20T09:00:00.123Z.
func (t Timestamp) MarshalText() ([]byte, error) {
	if t.t.IsZero() {
		return nil, fmt.Errorf("timestamp is unset")
	}
	return []byte(t.String()), nil
}

// UnmarshalText accepts UTC instants with a Z offset and at most microsecond
// precision. Unlike the schema pattern, it also rejects impossible dates.
func (t *Timestamp) UnmarshalText(text []byte) error {
	parsed, err := time.Parse(time.RFC3339Nano, string(text))
	if err != nil {
		return fmt.Errorf("invalid timestamp %q: %w", text, err)
	}
	if text[len(text)-1] != 'Z' {
		return fmt.Errorf("timestamp %q is not UTC", text)
	}
	if parsed.Nanosecond()%int(time.Microsecond) != 0 {
		return fmt.Errorf("timestamp %q is more precise than microseconds", text)
	}
	t.t = parsed.UTC()
	return nil
}

// Role is a Project membership role.
type Role string

const (
	RoleOwner  Role = "owner"
	RoleMember Role = "member"
)

// KeyPointKind classifies an Objective key point.
type KeyPointKind string

const (
	KeyPointOutcome             KeyPointKind = "outcome"
	KeyPointAcceptanceCriterion KeyPointKind = "acceptance_criterion"
	KeyPointInvariant           KeyPointKind = "invariant"
	KeyPointConstraint          KeyPointKind = "constraint"
	KeyPointQuestion            KeyPointKind = "question"
)

// BoundaryPointKind classifies a CellCharter boundary point.
type BoundaryPointKind string

const (
	BoundaryResponsibility     BoundaryPointKind = "responsibility"
	BoundaryExclusion          BoundaryPointKind = "exclusion"
	BoundaryDeliverable        BoundaryPointKind = "deliverable"
	BoundaryEvidenceObligation BoundaryPointKind = "evidence_obligation"
)

// BlueprintStatus is the lifecycle state served beside a BlueprintRevision.
type BlueprintStatus string

const (
	BlueprintProposed BlueprintStatus = "proposed"
	BlueprintActive   BlueprintStatus = "active"
	BlueprintRejected BlueprintStatus = "rejected"
)

// CharterOutcome is a human's decision on their own CellCharter.
type CharterOutcome string

const (
	CharterAccept CharterOutcome = "accept"
	CharterReject CharterOutcome = "reject"
)
