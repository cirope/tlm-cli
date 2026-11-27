package cli

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// guideAsk is one request for the guide a fake Tlm got.
type guideAsk struct {
	Query  string
	Header http.Header
}

// guideServer is a Tlm that only serves its guide for agents, and
// remembers how it was asked for.
func guideServer(t *testing.T) (*httptest.Server, *[]guideAsk) {
	t.Helper()
	var asks []guideAsk
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/agents.md" {
			http.NotFound(w, r)
			return
		}
		asks = append(asks, guideAsk{r.URL.RawQuery, r.Header.Clone()})
		w.Header().Set("Content-Type", "text/markdown")
		body := "# Add Tlm to this app\n\n## 1. Install the CLI\n\x1b[31m\n"
		if r.URL.Query().Get("stack") != "" {
			body = "# Add Tlm to this app\n\n### Ruby on Rails\n"
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, &asks
}

func runAt(t *testing.T, url string, args ...string) (int, string, string) {
	t.Helper()
	return runIn(t, "", url, args...)
}

// runIn runs tlm in dir (where the stack is detected).
func runIn(t *testing.T, dir, url string, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	env := Env{
		Stdout:    &stdout,
		Stderr:    &stderr,
		Getenv:    func(k string) string { return map[string]string{"TLM_URL": url}[k] },
		ConfigDir: t.TempDir(),
		Dir:       dir,
	}
	code := Run(context.Background(), args, env)
	return code, stdout.String(), stderr.String()
}

// The guide needs no sign-in: it's how an agent starts.
func TestGuidePrintsTlmsGuideForAgents(t *testing.T) {
	srv, asks := guideServer(t)
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
	if len(*asks) != 1 || (*asks)[0].Query != "" {
		t.Errorf("asks = %+v, want one without a stack", *asks)
	}
	seen := (*asks)[0].Header
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

// --stack asks Tlm for those stacks only; unknown ones are a usage
// mistake naming them.
func TestGuideStackFlag(t *testing.T) {
	srv, asks := guideServer(t)
	code, stdout, stderr := runAt(t, srv.URL, "guide", "--stack", "node,javascript")
	if code != ExitOK || !strings.Contains(stdout, "### Ruby on Rails") {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	if len(*asks) != 1 || (*asks)[0].Query != "stack=node%2Cjavascript" {
		t.Errorf("asks = %+v, want stack=node,javascript", *asks)
	}
	code, _, stderr = runAt(t, srv.URL, "guide", "--stack", "rails")
	if code != ExitUsage || !strings.Contains(stderr, `unknown stack "rails": use elixir, node, javascript, ruby, other`) {
		t.Errorf("exit %d, stderr %q", code, stderr)
	}
}

// Without --stack, the stack is read from the directory: a Ruby app
// asks for ruby, an Elixir one for elixir, one with npm for node and
// the browser.
func TestGuideDetectsTheStack(t *testing.T) {
	for name, tc := range map[string]struct {
		files []string
		want  string
	}{
		"rails":              {[]string{"Gemfile"}, "stack=ruby"},
		"phoenix":            {[]string{"mix.exs"}, "stack=elixir"},
		"node":               {[]string{`package.json {"dependencies":{"express":"^5"}}`}, "stack=node%2Cjavascript"},
		"empty package.json": {[]string{"package.json {}"}, "stack=node%2Cjavascript"},
	} {
		t.Run(name, func(t *testing.T) {
			srv, asks := guideServer(t)
			if code, _, stderr := runIn(t, fileTree(t, tc.files...), srv.URL, "guide"); code != ExitOK {
				t.Fatalf("exit %d, stderr %q", code, stderr)
			}
			if len(*asks) != 1 || (*asks)[0].Query != tc.want {
				t.Errorf("asks = %+v, want %s", *asks, tc.want)
			}
		})
	}
}

// Several stacks or none: the whole guide, which asks for nothing.
func TestGuideWithoutOneStackAsksForTheWholeGuide(t *testing.T) {
	for name, files := range map[string][]string{
		"ruby and elixir":       {"Gemfile", "mix.exs"},
		"ruby and npm":          {"Gemfile", "package.json {}"},
		"elixir and npm":        {"mix.exs", "package.json {}"},
		"a file that says none": {"README.md"},
		"a broken package.json": {"package.json {"},
		"empty":                 nil,
	} {
		t.Run(name, func(t *testing.T) {
			srv, asks := guideServer(t)
			if code, _, stderr := runIn(t, fileTree(t, files...), srv.URL, "guide"); code != ExitOK {
				t.Fatalf("exit %d, stderr %q", code, stderr)
			}
			if len(*asks) != 1 || (*asks)[0].Query != "" {
				t.Errorf("asks = %+v, want the whole guide", *asks)
			}
		})
	}
	t.Run("a directory that isn't there", func(t *testing.T) {
		srv, asks := guideServer(t)
		if code, _, stderr := runIn(t, filepath.Join(t.TempDir(), "nowhere"), srv.URL, "guide"); code != ExitOK {
			t.Fatalf("exit %d, stderr %q", code, stderr)
		}
		if len(*asks) != 1 || (*asks)[0].Query != "" {
			t.Errorf("asks = %+v, want the whole guide", *asks)
		}
	})
}

// fileTree writes files, each "name content" with a single space between
// them, into a fresh directory.
func fileTree(t *testing.T, files ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, f := range files {
		name, content, _ := strings.Cut(f, " ")
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// tlm origins --help says wildcards work and what happens to an origin
// nobody allowed yet.
func TestOriginsHelpDocumentsWildcardsAndPending(t *testing.T) {
	_, stdout, stderr := run(t, "origins", "--help")
	if stderr != "" {
		t.Fatalf("stderr = %q", stderr)
	}
	// Help is wrapped to the terminal's width: compare it unwrapped.
	stdout = strings.Join(strings.Fields(stdout), " ")
	for _, want := range []string{
		"tlm origins allow '*.example.com'",
		"matches every subdomain, with the same scheme and port, not example.com itself, which needs its own origin",
		"Events from an origin nobody answered yet are kept, and its admins are asked",
		"a blocked origin's events are refused (403)",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("--help lacks %q:\n%s", want, stdout)
		}
	}
}
