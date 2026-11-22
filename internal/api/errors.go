package api

import (
	"errors"
	"net/http"
	"time"
)

// Errors to test for with errors.Is. An *Error matches the one for its
// status or code.
var (
	ErrUnauthorized = errors.New("not signed in, or the token was revoked")
	ErrReadOnly     = errors.New("the token is read-only")
	ErrForbidden    = errors.New("not allowed")
	ErrNotFound     = errors.New("not found")
	ErrNetwork      = errors.New("couldn't reach Tlm")
)

// Error is an error answer from Tlm: its HTTP status and the
// envelope {"error": {"code", "message", "fields"}}. Status is 0 for a
// network failure (Code "network"); Code is "http_error" when the body
// wasn't the envelope.
type Error struct {
	Status     int
	Code       string
	Message    string              // For people, shown as given.
	Fields     map[string][]string // Per-field messages on 422.
	RetryAfter time.Duration       // From Retry-After, when given.
}

func (e *Error) Error() string { return e.Message }

// Is matches the sentinel for the error's status or code.
func (e *Error) Is(target error) bool {
	switch target {
	case ErrUnauthorized:
		return e.Status == http.StatusUnauthorized
	case ErrReadOnly:
		return e.Code == "read_only"
	case ErrForbidden:
		return e.Status == http.StatusForbidden
	case ErrNotFound:
		return e.Status == http.StatusNotFound
	case ErrNetwork:
		return e.Code == "network"
	}
	return false
}
