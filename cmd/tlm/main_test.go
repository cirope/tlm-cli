package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cirope/tlm-cli/internal/api/apitest"
	"github.com/rogpeppe/go-internal/testscript"
)

// TestMain makes the test binary also act as tlm for the scripts.
// It is main's own wiring with the parts of the machine that tests must
// never touch, or that would make output vary, replaced: no keychain (so
// tokens land in a file under $WORK), no browser (the URL is logged to
// $WORK/browser.log), a fixed hostname, clock and time zone, no waiting,
// and a config directory under $WORK.
func TestMain(m *testing.M) {
	testscript.Main(m, map[string]func(){
		"gh": fakeGH,
		"tlm": func() {
			work := os.Getenv("WORK")
			os.Exit(run(os.Args[1:], deps{
				keyring:     noKeyring{},
				openBrowser: logBrowser(filepath.Join(work, "browser.log")),
				hostname:    func() (string, error) { return "test-host", nil },
				configDir:   func() (string, error) { return filepath.Join(work, "config"), nil },
				cacheDir:    func() (string, error) { return filepath.Join(work, "cache"), nil },
				now:         func() time.Time { return scriptNow },
				location:    time.UTC,
				sleep:       func(ctx context.Context, _ time.Duration) error { return ctx.Err() },
			}))
		},
	})
}

// fakeGH stands in for GitHub's gh: it appends its arguments and stdin
// to $WORK/gh.log, and fails its FAKE_GH_FAIL command (auth, secret)
// like a token without access.
func fakeGH() {
	in, _ := io.ReadAll(os.Stdin)
	f, err := os.OpenFile(filepath.Join(os.Getenv("WORK"), "gh.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err == nil {
		_, _ = fmt.Fprintf(f, "args: %s\nstdin: %s\n", strings.Join(os.Args[1:], " "), in)
		_ = f.Close()
	}
	if len(os.Args) > 1 && os.Getenv("FAKE_GH_FAIL") == os.Args[1] {
		fmt.Fprintln(os.Stderr, "failed to set secret: HTTP 403: Resource not accessible by integration")
		os.Exit(1)
	}
	os.Exit(0)
}

// Scripts run on Friday, 13 November 2026, at 12:05 UTC, five minutes
// after the fixtures were recorded.
var scriptNow = time.Date(2026, 11, 13, 12, 5, 0, 0, time.UTC)

// TestScripts runs tlm through every script in testdata/script
// against a fake Tlm built from the API fixtures, asserting output,
// exit codes and files.
func TestScripts(t *testing.T) {
	testscript.Run(t, testscript.Params{
		Dir: "testdata/script",
		Setup: func(env *testscript.Env) error {
			env.Setenv("HOME", filepath.Join(env.WorkDir, "home"))
			env.Setenv("XDG_CONFIG_HOME", "")
			env.Setenv("TLM_NO_UPDATE_NOTIFIER", "1")
			return nil
		},
		Cmds: map[string]func(ts *testscript.TestScript, neg bool, args []string){
			"exits":        exits,
			"fake-tlm":     fakeTlm,
			"fixture-body": fixtureBody,
		},
	})
}

// exits runs a program and asserts its exit code:
//
//	exits 2 tlm --bogus
//
// stdout and stderr are kept for the stdout and stderr commands.
func exits(ts *testscript.TestScript, neg bool, args []string) {
	if neg || len(args) < 2 {
		ts.Fatalf("usage: exits <code> <program> [args...]")
	}
	want, err := strconv.Atoi(args[0])
	if err != nil {
		ts.Fatalf("exits: bad code %q", args[0])
	}
	got := 0
	if err := ts.Exec(args[1], args[2:]...); err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			ts.Fatalf("exits: running %s: %v", args[1], err)
		}
		got = ee.ExitCode()
	}
	if got != want {
		ts.Fatalf("exit code = %d, want %d", got, want)
	}
}

// fixtureBody writes a fixture's response body to a file, so a script
// compares output with what Tlm really sends:
//
//	fixture-body issue.get.markdown issue.md
func fixtureBody(ts *testscript.TestScript, neg bool, args []string) {
	if neg || len(args) != 2 {
		ts.Fatalf("usage: fixture-body <fixture> <file>")
	}
	f := apitest.Load(&scriptT{ts: ts}, args[0])
	body := []byte(f.Response.Body)
	var s string
	if json.Unmarshal(f.Response.Body, &s) == nil {
		body = []byte(s) // A text body, as the server sends it.
	}
	if err := os.WriteFile(ts.MkAbs(args[1]), body, 0o600); err != nil {
		ts.Fatalf("fixture-body: %v", err)
	}
}

// fakeTlm starts a fake Tlm answering with the named fixtures and
// points TLM_URL at it ($URL in scripts, and $DSN a project's DSN on it):
//
//	fake-tlm me.ok projects.list.ok
func fakeTlm(ts *testscript.TestScript, neg bool, args []string) {
	if neg || len(args) == 0 {
		ts.Fatalf("usage: fake-tlm <fixture>...")
	}
	st := &scriptT{ts: ts}
	srv := apitest.NewServer(st, args...)
	ts.Setenv("TLM_URL", srv.URL)
	ts.Setenv("URL", srv.URL)
	ts.Setenv("DSN", strings.Replace(srv.URL, "://", "://tlm_test_key_shop@", 1))
	ts.Defer(st.cleanup)
}

// scriptT adapts a script to what apitest needs. The server's complaints
// arrive from other goroutines, so they are kept and reported at the end.
type scriptT struct {
	ts       *testscript.TestScript
	mu       sync.Mutex
	errs     []string
	cleanups []func()
}

func (s *scriptT) Helper() {}

func (s *scriptT) Errorf(format string, args ...any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.errs = append(s.errs, fmt.Sprintf(format, args...))
}

func (s *scriptT) Fatalf(format string, args ...any) { s.ts.Fatalf(format, args...) }

func (s *scriptT) Cleanup(f func()) { s.cleanups = append(s.cleanups, f) }

func (s *scriptT) cleanup() {
	for _, f := range s.cleanups {
		f()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.errs {
		s.ts.Logf("fake tlm: %s", e)
	}
}

// noKeyring is a machine without a keychain.
type noKeyring struct{}

var errNoKeychain = errors.New("no keychain in tests")

func (noKeyring) Get(string, string) (string, error) { return "", errNoKeychain }
func (noKeyring) Set(string, string, string) error   { return errNoKeychain }
func (noKeyring) Delete(string, string) error        { return errNoKeychain }

// logBrowser "opens" a URL by appending it to a file.
func logBrowser(path string) func(string) error {
	return func(url string) error {
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(f, url)
		return errors.Join(err, f.Close())
	}
}
