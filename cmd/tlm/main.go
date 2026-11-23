// Command tlm is the command-line client for Tlm. It only
// wires the real environment into internal/cli and exits with the code it
// returns.
package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/cirope/tlm-cli/internal/auth"
	"github.com/cirope/tlm-cli/internal/cli"
	"golang.org/x/term"
)

// Stamped at build time with -ldflags "-X main.version=...".
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// deps are the parts of the machine that tests replace.
type deps struct {
	keyring     auth.Keyring
	openBrowser func(url string) error
	hostname    func() (string, error)
	configDir   func() (string, error)
	cacheDir    func() (string, error)
	now         func() time.Time
	location    *time.Location
	sleep       func(context.Context, time.Duration) error
}

func main() {
	os.Exit(run(os.Args[1:], deps{
		keyring:     auth.SystemKeyring{},
		openBrowser: cli.OpenBrowser,
		hostname:    os.Hostname,
		configDir:   os.UserConfigDir,
		cacheDir:    os.UserCacheDir,
		now:         time.Now,
		location:    time.Local,
	}))
}

func run(args []string, d deps) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	configDir, err := d.configDir()
	if err != nil {
		configDir = "" // Commands that need it say so.
	}
	dir, err := os.Getwd()
	if err != nil {
		dir = "" // No .tlm.toml.
	}
	cacheDir, err := d.cacheDir()
	if err != nil {
		cacheDir = "" // No update notice.
	}
	return cli.Run(ctx, args, cli.Env{
		Stdout:           os.Stdout,
		Stderr:           os.Stderr,
		Stdin:            os.Stdin,
		Getenv:           os.Getenv,
		Build:            cli.Build{Version: version, Commit: commit, Date: date},
		ConfigDir:        configDir,
		Dir:              dir,
		Keyring:          d.keyring,
		HTTPClient:       &http.Client{},
		OpenBrowser:      d.openBrowser,
		Hostname:         d.hostname,
		Now:              d.now,
		Location:         d.location,
		Sleep:            d.sleep,
		StdinIsTerminal:  term.IsTerminal(int(os.Stdin.Fd())),  //nolint:gosec // Fd fits in an int.
		StdoutIsTerminal: term.IsTerminal(int(os.Stdout.Fd())), //nolint:gosec // Fd fits in an int.
		StderrIsTerminal: term.IsTerminal(int(os.Stderr.Fd())), //nolint:gosec // Fd fits in an int.
		CacheDir:         cacheDir,
		Executable:       executable(),
	})
}

// executable is tlm's own path with symlinks resolved, or "".
func executable() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		return resolved
	}
	return exe
}
