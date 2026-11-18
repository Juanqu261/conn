// Package secret wraps bearer credentials so they cannot leak through
// formatting, logging, or error messages.
package secret

import (
	"fmt"
	"log/slog"
)

const redacted = "[redacted]"

// String holds a credential. fmt verbs, slog, String, and JSON encoding all
// print a redaction marker. Only Reveal returns the value, at the point it
// is sent or stored, so a struct holding a String is safe to log.
type String struct {
	value string
}

// New wraps value.
func New(value string) String { return String{value: value} }

// Reveal returns the credential.
func (s String) Reveal() string { return s.value }

// IsZero reports whether the credential is empty.
func (s String) IsZero() bool { return s.value == "" }

func (s String) String() string   { return redacted }
func (s String) GoString() string { return redacted }

// Format redacts every fmt verb, including %#v and %+v.
func (s String) Format(f fmt.State, _ rune) { _, _ = f.Write([]byte(redacted)) }

// LogValue redacts the value in structured logs.
func (s String) LogValue() slog.Value { return slog.StringValue(redacted) }

// MarshalText redacts. Wire formats that must carry the credential call
// Reveal explicitly.
func (s String) MarshalText() ([]byte, error) { return []byte(redacted), nil }

// UnmarshalText accepts a credential from a trusted wire format.
func (s *String) UnmarshalText(text []byte) error {
	s.value = string(text)
	return nil
}
