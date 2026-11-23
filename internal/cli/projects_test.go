package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// tlmServer answers "METHOD /path" under /api/v1 with a status and a
// JSON body, and keeps the bodies it got.
type tlmServer struct {
	*httptest.Server
	mu  sync.Mutex
	got []string
}

func newTlmServer(t *testing.T, routes map[string]func(body string) (int, string)) *tlmServer {
	t.Helper()
	s := &tlmServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		key := r.Method + " " + strings.TrimPrefix(r.URL.Path, "/api/v1")
		s.mu.Lock()
		s.got = append(s.got, key+" "+string(b))
		s.mu.Unlock()
		answer, ok := routes[key]
		if !ok {
			t.Errorf("unexpected %s", key)
			http.Error(w, "no route", http.StatusTeapot)
			return
		}
		status, body := answer(string(b))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(s.Close)
	return s
}

func runWith(t *testing.T, url string, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	vars := map[string]string{"TLM_URL": url, "TLM_TOKEN": "tlmp_test_token", "TLM_ORG": "acme"} //nolint:gosec // A fake token.
	code := Run(context.Background(), args, Env{
		Stdout: &stdout, Stderr: &stderr, ConfigDir: t.TempDir(),
		Getenv: func(k string) string { return vars[k] },
	})
	return code, stdout.String(), stderr.String()
}

const docsProject = `{"id":"PrjDocs7Kq2X","slug":"docs","name":"Docs","platforms":[],"unresolved_count":0,` +
	`"created_at":"2026-11-13T12:00:00Z","url":"http://tlm.test/projects/docs?org=Acm7Kq2XpAb2",` +
	`"dsn":"http://tlm_test_key_docs@tlm.test","muted":false}`

func allowOrigin(body string) (int, string) {
	var in struct{ Origin, Status string }
	_ = json.Unmarshal([]byte(body), &in)
	if !strings.Contains(in.Origin, "://") {
		return http.StatusUnprocessableEntity, `{"error":{"code":"invalid","message":"Some values aren't valid.","fields":{"origin":["must be a scheme and a host"]}}}`
	}
	return http.StatusOK, `{"origin":"` + in.Origin + `","status":"` + in.Status + `","created_at":"2026-11-13T12:00:00Z"}`
}

// --origin allows the app's domains as the project is created, a bare
// host meaning https://.
func TestProjectsCreateAllowsOrigins(t *testing.T) {
	srv := newTlmServer(t, map[string]func(string) (int, string){
		"POST /orgs/acme/projects":             func(string) (int, string) { return http.StatusCreated, docsProject },
		"PUT /orgs/acme/projects/docs/origins": allowOrigin,
	})
	code, stdout, stderr := runWith(t, srv.URL, "projects", "create", "Docs",
		"--origin", "docs.example.com", "--origin", "http://localhost:3000")
	if code != ExitOK {
		t.Fatalf("exit code = %d; stderr: %s", code, stderr)
	}
	for _, want := range []string{
		"DSN: http://tlm_test_key_docs@tlm.test\n",
		"Allowed https://docs.example.com\n",
		"Allowed http://localhost:3000\n",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout = %q, want %q", stdout, want)
		}
	}
	if len(srv.got) != 3 || !strings.Contains(srv.got[1], `"origin":"https://docs.example.com"`) ||
		!strings.Contains(srv.got[1], `"status":"allowed"`) {
		t.Errorf("requests = %q", srv.got)
	}
}

// A refused origin doesn't undo the project: it says so, and exits 1.
func TestProjectsCreateWithARefusedOrigin(t *testing.T) {
	srv := newTlmServer(t, map[string]func(string) (int, string){
		"POST /orgs/acme/projects": func(string) (int, string) { return http.StatusCreated, docsProject },
		"PUT /orgs/acme/projects/docs/origins": func(string) (int, string) {
			return http.StatusUnprocessableEntity, `{"error":{"code":"invalid","message":"Some values aren't valid.","fields":{"origin":["must be a scheme and a host"]}}}`
		},
	})
	code, stdout, stderr := runWith(t, srv.URL, "projects", "create", "Docs", "--origin", "docs.example.com")
	if code != ExitError {
		t.Errorf("exit code = %d, want %d", code, ExitError)
	}
	if !strings.Contains(stdout, "Docs (docs)") {
		t.Errorf("stdout = %q, want the project", stdout)
	}
	if !strings.Contains(stderr, "error: https://docs.example.com: Some values aren't valid.") ||
		!strings.Contains(stderr, "tlm origins allow <origin> --project docs") {
		t.Errorf("stderr = %q", stderr)
	}
}
