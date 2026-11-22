package api

import (
	"fmt"
	"log/slog"
	"strings"
)

// Redacted is what a token prints as.
const Redacted = "[redacted]"

// Token is a secret: a personal token (tlmp_…) or an upload token. It
// redacts itself wherever it is printed, logged or encoded; only Reveal
// gives the value, to send or store it.
type Token string

// Reveal returns the secret value. Call it only to send or store it.
func (t Token) Reveal() string { return string(t) }

// Scrub replaces the token in s with [redacted].
func (t Token) Scrub(s string) string {
	if t == "" {
		return s
	}
	return strings.ReplaceAll(s, string(t), Redacted)
}

// String implements fmt.Stringer.
func (Token) String() string { return Redacted }

// GoString implements fmt.GoStringer.
func (Token) GoString() string { return Redacted }

// Format implements fmt.Formatter, so no verb (%x, %q...) shows the value.
func (Token) Format(f fmt.State, _ rune) { _, _ = f.Write([]byte(Redacted)) }

// LogValue implements slog.LogValuer.
func (Token) LogValue() slog.Value { return slog.StringValue(Redacted) }

// MarshalJSON implements json.Marshaler.
func (Token) MarshalJSON() ([]byte, error) { return []byte(`"` + Redacted + `"`), nil }
