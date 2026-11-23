// Package sourcemaps finds a build's source maps, flattens index maps,
// uploads the maps to Tlm and deletes them afterwards. It also sends
// the one test event behind `tlm test`.
//
// It knows nothing about flags or terminals: callers pass options in and
// get warnings, log lines and errors back as text. Every text that came
// from the server has had its control characters neutralized, and no text
// ever carries the token.
package sourcemaps

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
)

// ErrUsage matches (with errors.Is) every *UsageError: a mistake in how the
// tool was called, which the command reports with exit code 2.
var ErrUsage = errors.New("usage error")

// UsageError is a mistake in how the tool was called (exit code 2), not a
// failed upload. Its message is ready to show after "error: ".
type UsageError struct{ msg string }

func (e *UsageError) Error() string { return e.msg }

// Is makes errors.Is(err, ErrUsage) true.
func (e *UsageError) Is(target error) bool { return target == ErrUsage }

func newUsageError(msg string) error { return &UsageError{msg: msg} }

// Token is a secret: an upload token. Every way of
// printing or encoding it shows [redacted] instead; convert it with string()
// only to send it.
type Token string

const redacted = "[redacted]"

// String hides the token.
func (Token) String() string { return redacted }

// GoString hides the token from %#v.
func (Token) GoString() string { return redacted }

// Format hides the token from every fmt verb.
func (Token) Format(f fmt.State, _ rune) { _, _ = io.WriteString(f, redacted) }

// MarshalText hides the token from encoders such as encoding/json.
func (Token) MarshalText() ([]byte, error) { return []byte(redacted), nil }

// LogValue hides the token from log/slog.
func (Token) LogValue() slog.Value { return slog.StringValue(redacted) }

// scrub removes secret from text, whatever its source.
func scrub(text, secret string) string {
	if secret == "" {
		return text
	}
	return strings.ReplaceAll(text, secret, redacted)
}

// neutralize makes text from the server safe to print to a terminal:
// newlines become ⏎ and other C0 and C1 control characters �.
func neutralize(text string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n':
			return '⏎'
		case r < 0x20, r == 0x7f, r >= 0x80 && r <= 0x9f:
			return '�'
		}
		return r
	}, text)
}
