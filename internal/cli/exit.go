package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"

	"github.com/cirope/tlm-cli/internal/api"
	"github.com/cirope/tlm-cli/internal/auth"
	"github.com/cirope/tlm-cli/internal/render"
	"github.com/cirope/tlm-cli/internal/target"
)

// Exit codes. They are a public contract: scripts and agents branch on
// them.
const (
	ExitOK    = 0 // Done.
	ExitError = 1 // Tlm refused or failed: not found, not allowed, validation, network, server error.
	ExitUsage = 2 // Wrong usage (unknown flag, missing argument).
	ExitAuth  = 3 // Not signed in, a rejected or revoked token, a read-only token asked to change something, or an organization asking for a new sign-in.
)

// usageError marks an error caused by how the command was invoked.
type usageError struct{ err error }

func (e usageError) Error() string { return e.err.Error() }
func (e usageError) Unwrap() error { return e.err }

// authError marks an error that means the token can't be used: exit 3.
type authError struct{ err error }

func (e authError) Error() string { return e.err.Error() }
func (e authError) Unwrap() error { return e.err }

// silentError has been reported already; it only carries the exit code.
type silentError struct{ code int }

func (e silentError) Error() string { return fmt.Sprintf("exit %d", e.code) }

// classify picks the exit code and the JSON error code for err.
func classify(err error) (int, string) {
	var (
		ue     usageError
		ae     authError
		se     silentError
		fe     failure
		ns     notSignedIn
		apiErr *api.Error
	)
	switch {
	case err == nil:
		return ExitOK, ""
	case errors.As(err, &se):
		return se.code, ""
	case errors.As(err, &fe):
		_, kind := classify(fe.err)
		if kind == "usage" {
			kind = "error"
		}
		return ExitError, kind
	case errors.As(err, &ue), errors.Is(err, target.ErrAmbiguous), errors.Is(err, target.ErrBadIssue), errors.Is(err, target.ErrBadRepoFile):
		return ExitUsage, "usage"
	case errors.As(err, &ns):
		return ExitAuth, "unauthorized"
	case errors.As(err, &ae):
		return ExitAuth, "unauthorized"
	case errors.Is(err, auth.ErrDenied):
		return ExitAuth, "access_denied"
	case errors.Is(err, auth.ErrExpired):
		return ExitAuth, "expired_token"
	case errors.As(err, &apiErr):
		switch {
		case apiErr.Status == http.StatusUnauthorized,
			apiErr.Code == "read_only", apiErr.Code == "sign_in_again",
			apiErr.Code == "access_denied", apiErr.Code == "expired_token":
			return ExitAuth, apiErr.Code
		case apiErr.Status == http.StatusBadRequest:
			return ExitUsage, apiErr.Code
		}
		return ExitError, apiErr.Code
	case errors.Is(err, context.Canceled):
		return ExitError, "interrupted"
	}
	return ExitError, "error"
}

func exitCode(err error) int {
	code, _ := classify(err)
	return code
}

// hint is what to do next, when there's something to say.
func (a *app) hint(err error) string {
	var (
		ns     notSignedIn
		apiErr *api.Error
	)
	switch {
	case errors.As(err, &ns):
		return "run: tlm login (or set TLM_TOKEN)"
	case errors.Is(err, api.ErrUnauthorized), errors.As(err, new(authError)),
		errors.As(err, &apiErr) && apiErr.Code == "sign_in_again":
		return "run: tlm login"
	case errors.Is(err, api.ErrReadOnly):
		return "this token is read-only; create one with write access, or run: tlm login"
	case errors.Is(err, target.ErrAmbiguous):
		return "or set a default for this Tlm: tlm use <org>[/<project>]"
	case errors.As(err, &apiErr) && apiErr.Code == "forbidden":
		if a.org != "" {
			return "ask an admin of " + a.org
		}
		return "ask an admin of the organization"
	}
	return ""
}

// reportError writes err to stderr: "error:" lines, or one JSON object
// under --json.
func (a *app) reportError(err error) {
	code, kind := classify(err)
	if kind == "" || errors.Is(err, context.Canceled) {
		return
	}
	var apiErr *api.Error
	fields := map[string][]string{}
	if errors.As(err, &apiErr) {
		fields = apiErr.Fields
	}
	msg := render.Safe(err.Error())
	w := a.env.Stderr
	if a.flags.json {
		e := map[string]any{"code": kind, "message": msg}
		if len(fields) > 0 {
			e["fields"] = fields
		}
		b, _ := json.Marshal(map[string]any{"error": e})
		fmt.Fprintf(w, "%s\n", b)
		return
	}
	fmt.Fprintf(w, "error: %s\n", msg)
	for _, name := range slices.Sorted(maps.Keys(fields)) {
		fmt.Fprintf(w, "  %s: %s\n", render.Safe(name), render.Safe(strings.Join(fields[name], ", ")))
	}
	if h := a.hint(err); h != "" {
		fmt.Fprintln(w, h)
	}
	var ue usageError
	if code == ExitUsage && (errors.As(err, &ue) || errors.Is(err, target.ErrAmbiguous) || errors.Is(err, target.ErrBadIssue)) {
		fmt.Fprintln(w, "Run tlm --help for usage.")
	}
}
