package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// versionEnv is a Homebrew install of version, with GitHub answering
// status and body for the latest release.
func versionEnv(t *testing.T, version string, status int, body string, terminal bool) (Env, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	var stdout, stderr bytes.Buffer
	return Env{
		Stdout:           &stdout,
		Stderr:           &stderr,
		Getenv:           func(string) string { return "" },
		Build:            Build{Version: version, Commit: "abc1234", Date: "2026-11-18T00:00:00Z"},
		HTTPClient:       srv.Client(),
		StdoutIsTerminal: terminal,
		StderrIsTerminal: terminal,
		CacheDir:         t.TempDir(),
		UpdateURL:        srv.URL,
		Executable:       "/opt/homebrew/Caskroom/tlm/" + version + "/tlm",
		GOOS:             "darwin",
	}, &stdout, &stderr
}

// tlm version always asks, in a pipe too, and says how to upgrade.
func TestVersionCommandNamesTheUpgrade(t *testing.T) {
	env, stdout, stderr := versionEnv(t, "0.1.0", http.StatusOK, `{"tag_name":"v0.2.0"}`, false)
	if code := Run(context.Background(), []string{"version"}, env); code != ExitOK {
		t.Fatalf("exit = %d; stderr: %s", code, stderr)
	}
	want := "tlm 0.1.0 (commit abc1234, built 2026-11-18T00:00:00Z)\n" +
		"A new version of tlm is available: 0.2.0.\n" +
		"Update with: brew upgrade tlm\n"
	if stdout.String() != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestVersionCommandUpToDate(t *testing.T) {
	env, stdout, _ := versionEnv(t, "0.2.0", http.StatusOK, `{"tag_name":"v0.2.0"}`, false)
	if code := Run(context.Background(), []string{"version"}, env); code != ExitOK {
		t.Fatalf("exit = %d", code)
	}
	want := "tlm 0.2.0 (commit abc1234, built 2026-11-18T00:00:00Z)\nThis is the latest version.\n"
	if stdout.String() != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
}

// At a terminal, the once-a-day notice doesn't repeat what it just said.
func TestVersionCommandSkipsTheDailyNotice(t *testing.T) {
	env, _, stderr := versionEnv(t, "0.1.0", http.StatusOK, `{"tag_name":"v0.2.0"}`, true)
	if code := Run(context.Background(), []string{"version"}, env); code != ExitOK {
		t.Fatalf("exit = %d", code)
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want nothing", stderr)
	}
}

// A failed check still prints the version, warns, and exits 0.
func TestVersionCommandCheckFails(t *testing.T) {
	env, stdout, stderr := versionEnv(t, "0.1.0", http.StatusInternalServerError, ``, false)
	if code := Run(context.Background(), []string{"version"}, env); code != ExitOK {
		t.Fatalf("exit = %d", code)
	}
	if want := "tlm 0.1.0 (commit abc1234, built 2026-11-18T00:00:00Z)\n"; stdout.String() != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
	if want := "warning: couldn't check for a newer version: status 500\n"; stderr.String() != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
}

func TestVersionCommandJSON(t *testing.T) {
	for name, tc := range map[string]struct {
		status int
		body   string
		want   map[string]any
	}{
		"newer": {http.StatusOK, `{"tag_name":"v0.2.0"}`, map[string]any{
			"version": "0.1.0", "commit": "abc1234", "date": "2026-11-18T00:00:00Z",
			"latest": "0.2.0", "update_available": true, "upgrade": "brew upgrade tlm",
		}},
		"check failed": {http.StatusInternalServerError, ``, map[string]any{
			"version": "0.1.0", "commit": "abc1234", "date": "2026-11-18T00:00:00Z",
			"latest": nil, "update_available": false, "upgrade": nil,
		}},
	} {
		t.Run(name, func(t *testing.T) {
			env, stdout, _ := versionEnv(t, "0.1.0", tc.status, tc.body, false)
			if code := Run(context.Background(), []string{"version", "--json"}, env); code != ExitOK {
				t.Fatalf("exit = %d", code)
			}
			var got map[string]any
			if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
				t.Fatalf("stdout %q: %v", stdout, err)
			}
			for k, v := range tc.want {
				if got[k] != v {
					t.Errorf("%s = %v, want %v", k, got[k], v)
				}
			}
			if len(got) != len(tc.want) {
				t.Errorf("keys = %v", got)
			}
		})
	}
}
