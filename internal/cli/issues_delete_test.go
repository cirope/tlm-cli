package cli

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"testing"
)

const shownIssue = `{"id":"Err7Kq2XpAb2","title":"TypeError","subtitle":"x is not a function","level":"error",` +
	`"status":"unresolved","events_count":3,"users_count":1,"first_seen_at":"2026-11-13T12:00:00Z",` +
	`"last_seen_at":"2026-11-13T12:00:00Z","url":"http://tlm.test/projects/shop/issues/Err7Kq2XpAb2"}`

func deleteAt(t *testing.T, url, answer string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	vars := map[string]string{"TLM_URL": url, "TLM_TOKEN": "tlmp_test_token", "TLM_ORG": "acme", "TLM_PROJECT": "shop"} //nolint:gosec // A fake token.
	code := Run(context.Background(), []string{"issues", "delete", "Err7Kq2XpAb2"}, Env{
		Stdout: &stdout, Stderr: &stderr, ConfigDir: t.TempDir(),
		Getenv: func(k string) string { return vars[k] }, Stdin: strings.NewReader(answer), StdinIsTerminal: true,
	})
	return code, stdout.String(), stderr.String()
}

// At a terminal it asks first, naming the issue and how many events go
// with it; anything but yes deletes nothing.
func TestIssuesDeleteAsksFirst(t *testing.T) {
	routes := map[string]func(string) (int, string){
		"GET /orgs/acme/projects/shop/issues/Err7Kq2XpAb2":    func(string) (int, string) { return http.StatusOK, shownIssue },
		"DELETE /orgs/acme/projects/shop/issues/Err7Kq2XpAb2": func(string) (int, string) { return http.StatusNoContent, "" },
	}
	srv := newTlmServer(t, routes)
	code, stdout, stderr := deleteAt(t, srv.URL, "n\n")
	if code != ExitError || stdout != "" || !strings.Contains(stderr, "Nothing changed.") {
		t.Errorf("no: exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	want := `Delete Err7Kq2XpAb2 (TypeError: x is not a function) and its 3 events, for good? This can't be undone. [y/N] `
	if !strings.Contains(stderr, want) {
		t.Errorf("stderr = %q, want the question %q", stderr, want)
	}
	if len(srv.got) != 1 {
		t.Errorf("requests = %q, want only the GET", srv.got)
	}

	code, stdout, _ = deleteAt(t, srv.URL, "y\n")
	if code != ExitOK || stdout != "Deleted Err7Kq2XpAb2 (TypeError: x is not a function) and its 3 events.\n" {
		t.Errorf("yes: exit %d, stdout %q", code, stdout)
	}
	if last := srv.got[len(srv.got)-1]; !strings.HasPrefix(last, "DELETE /orgs/acme/projects/shop/issues/Err7Kq2XpAb2") {
		t.Errorf("last request = %q", last)
	}
}
