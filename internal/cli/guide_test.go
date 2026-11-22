package cli

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// guideServer is a Tlm that only serves its guide for agents, and
// remembers how it was asked for.
func guideServer(t *testing.T) (*httptest.Server, *http.Header) {
	t.Helper()
	var seen http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/agents.md" {
			http.NotFound(w, r)
			return
		}
		seen = r.Header.Clone()
		w.Header().Set("Content-Type", "text/markdown")
		_, _ = w.Write([]byte("# Add Tlm to this app\n\n## 1. Install the CLI\n\x1b[31m\n"))
	}))
	t.Cleanup(srv.Close)
	return srv, &seen
}

func runAt(t *testing.T, url string, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	env := Env{
		Stdout:    &stdout,
		Stderr:    &stderr,
		Getenv:    func(k string) string { return map[string]string{"TLM_URL": url}[k] },
		ConfigDir: t.TempDir(),
	}
	code := Run(context.Background(), args, env)
	return code, stdout.String(), stderr.String()
}

// The guide needs no sign-in: it's how an agent starts.
func TestGuidePrintsTlmsGuideForAgents(t *testing.T) {
	srv, seen := guideServer(t)
	code, stdout, stderr := runAt(t, srv.URL, "guide", "--lang", "es")
	if code != ExitOK {
		t.Fatalf("exit code = %d; stderr: %s", code, stderr)
	}
	if !strings.HasPrefix(stdout, "# Add Tlm to this app\n\n## 1. Install the CLI\n") {
		t.Errorf("stdout = %q", stdout)
	}
	if strings.Contains(stdout, "\x1b") {
		t.Errorf("stdout = %q, want control characters neutralized", stdout)
	}
	if got := seen.Get("Accept-Language"); got != "es" {
		t.Errorf("Accept-Language = %q, want es", got)
	}
	if got := seen.Get("Authorization"); got != "" {
		t.Errorf("Authorization = %q, want none", got)
	}
}

func TestGuideIsMarkdownOnly(t *testing.T) {
	srv, _ := guideServer(t)
	code, _, stderr := runAt(t, srv.URL, "guide", "--json")
	if code != ExitUsage || !strings.Contains(stderr, "the guide is Markdown") {
		t.Errorf("exit code = %d, stderr = %q", code, stderr)
	}
}

func TestHelpPointsAgentsToTheGuide(t *testing.T) {
	_, stdout, _ := run(t, "--help")
	if !strings.Contains(stdout, "Adding Tlm to an app? Start with: tlm guide") {
		t.Errorf("--help doesn't point to tlm guide:\n%s", stdout)
	}
}
