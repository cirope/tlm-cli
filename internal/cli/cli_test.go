package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// run executes the CLI with args and returns its exit code, stdout and
// stderr. It never touches the real environment.
func run(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	env := Env{
		Stdout: &stdout,
		Stderr: &stderr,
		Getenv: func(string) string { return "" },
		Build:  Build{Version: "1.2.3", Commit: "abc1234", Date: "2026-11-13T00:00:00Z"},
	}
	code := Run(context.Background(), args, env)
	return code, stdout.String(), stderr.String()
}

func TestVersion(t *testing.T) {
	code, stdout, stderr := run(t, "--version")
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, ExitOK, stderr)
	}
	want := "tlm 1.2.3 (commit abc1234, built 2026-11-13T00:00:00Z)\n"
	if stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}
}

func TestUsageErrorsExitTwo(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"unknown flag", []string{"--bogus"}, "error: unknown flag: --bogus\n"},
		{"unknown command", []string{"frobnicate"}, "error: unknown command \"frobnicate\"\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, stdout, stderr := run(t, tt.args...)
			if code != ExitUsage {
				t.Errorf("exit code = %d, want %d", code, ExitUsage)
			}
			if stdout != "" {
				t.Errorf("stdout = %q, want empty", stdout)
			}
			want := tt.want + "Run tlm --help for usage.\n"
			if stderr != want {
				t.Errorf("stderr = %q, want %q", stderr, want)
			}
		})
	}
}

// With --json, an error is one JSON object on stderr.
func TestUsageErrorUnderJSON(t *testing.T) {
	code, stdout, stderr := run(t, "frobnicate", "--json")
	if code != ExitUsage {
		t.Errorf("exit code = %d, want %d", code, ExitUsage)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	var got struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(stderr), &got); err != nil {
		t.Fatalf("stderr is not JSON: %v\n%s", err, stderr)
	}
	if got.Error.Code != "usage" || got.Error.Message != `unknown command "frobnicate"` {
		t.Errorf("error = %+v", got.Error)
	}
}

// Without a command, tlm shows its usage on stderr and exits 2.
func TestNoCommand(t *testing.T) {
	code, stdout, stderr := run(t)
	if code != ExitUsage {
		t.Errorf("exit code = %d, want %d", code, ExitUsage)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, "Usage:") {
		t.Errorf("stderr = %q, want the usage", stderr)
	}
}

func TestHelpGoesToStdout(t *testing.T) {
	code, stdout, stderr := run(t, "--help")
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, ExitOK, stderr)
	}
	for _, want := range []string{"Usage:", "Examples:", "Exit codes:"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout = %q, want it to contain %q", stdout, want)
		}
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}
}

func TestEveryCommandHasAnExample(t *testing.T) {
	root := newRootCmd(&app{env: withDefaults(Env{})})
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		if c.Hidden || c.Name() == "help" || c.Name() == "completion" {
			return
		}
		if strings.TrimSpace(c.Example) == "" {
			t.Errorf("command %q has no example", c.CommandPath())
		}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(root)
}

func TestUpdateNoticeNamesTheUpgrade(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"tag_name":"v0.2.0"}`)
	}))
	t.Cleanup(srv.Close)
	var stdout, stderr bytes.Buffer
	env := Env{
		Stdout:           &stdout,
		Stderr:           &stderr,
		Getenv:           func(string) string { return "" },
		Build:            Build{Version: "0.1.0"},
		HTTPClient:       srv.Client(),
		StdoutIsTerminal: true,
		StderrIsTerminal: true,
		CacheDir:         t.TempDir(),
		UpdateURL:        srv.URL,
		Executable:       "/opt/homebrew/Caskroom/tlm/0.1.0/tlm",
		GOOS:             "darwin",
	}
	if code := Run(context.Background(), []string{"--help"}, env); code != ExitOK {
		t.Fatalf("exit code = %d; stderr: %s", code, stderr.String())
	}
	want := "\nA new version of tlm is available: 0.2.0 (you have 0.1.0).\nUpdate with: brew upgrade tlm\n"
	if stderr.String() != want {
		t.Errorf("stderr = %q, want %q", stderr.String(), want)
	}
}
