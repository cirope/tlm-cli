// Package cli holds tlm's commands: their flags, arguments and
// output. It stays thin: the work is done by the packages it wires
// together.
package cli

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/cirope/tlm-cli/internal/auth"
	"github.com/cirope/tlm-cli/internal/update"
	"github.com/spf13/cobra"
)

// Build describes the binary, as stamped in at build time.
type Build struct {
	Version string
	Commit  string
	Date    string
}

// Env is everything a command may touch outside itself. main fills it
// with the real thing; tests with fakes.
type Env struct {
	Stdout io.Writer
	Stderr io.Writer
	Stdin  io.Reader
	Getenv func(string) string
	Build  Build

	ConfigDir   string       // The platform's user config directory; tlm uses ConfigDir/tlm.
	Keyring     auth.Keyring // nil: tokens go to a file.
	HTTPClient  *http.Client
	OpenBrowser func(url string) error
	Hostname    func() (string, error)
	Now         func() time.Time
	Location    *time.Location // Where times are shown for people.
	// Sleep waits between retries and polls; tests make it instant.
	Sleep func(context.Context, time.Duration) error

	StdinIsTerminal  bool
	StdoutIsTerminal bool
	StderrIsTerminal bool

	CacheDir  string // The platform's user cache directory; "" disables the update notice.
	UpdateURL string // Where to ask for the latest release; "" is update.LatestURL.
	// Executable is tlm's own path, symlinks resolved ("" if unknown):
	// the update notice tells how to upgrade from how it was installed.
	Executable string
	GOOS       string // The operating system; "" is runtime.GOOS.
}

// Run executes the command line args (without the program name) and
// returns the process exit code.
func Run(ctx context.Context, args []string, env Env) int {
	a := &app{env: withDefaults(env)}
	root := newRootCmd(a)
	root.SetArgs(args)
	err := root.ExecuteContext(ctx)
	if err != nil {
		a.reportError(err)
	}
	a.updateNotice(ctx)
	return exitCode(err)
}

func withDefaults(env Env) Env {
	if env.Stdout == nil {
		env.Stdout = io.Discard
	}
	if env.Stderr == nil {
		env.Stderr = io.Discard
	}
	if env.Stdin == nil {
		env.Stdin = strings.NewReader("")
	}
	if env.Getenv == nil {
		env.Getenv = func(string) string { return "" }
	}
	if env.HTTPClient == nil {
		env.HTTPClient = &http.Client{}
	}
	if env.OpenBrowser == nil {
		env.OpenBrowser = func(string) error { return nil }
	}
	if env.Hostname == nil {
		env.Hostname = func() (string, error) { return "unknown", nil }
	}
	if env.Now == nil {
		env.Now = time.Now
	}
	if env.Location == nil {
		env.Location = time.Local
	}
	return env
}

const rootLong = `tlm manages Tlm from the terminal: projects and their setup,
issues, speed, and source map uploads. For people, their coding agents
and CI.

Output is for people by default; --json prints Tlm's JSON as is, and
--markdown Tlm's text for an AI, where offered.

Which project: --org and --project (or TLM_ORG, TLM_PROJECT), implied
when there's only one. Credentials: TLM_TOKEN, else the token tlm login
stored.

Exit codes: 0 done, 1 Tlm refused or failed, 2 wrong usage, 3 not
signed in, a revoked token, a read-only token asked to change something,
or an organization asking for a new sign-in.`

func newRootCmd(a *app) *cobra.Command {
	root := &cobra.Command{
		Use:   "tlm <command>",
		Short: "Manage Tlm from the terminal",
		Long:  rootLong,
		Example: "  tlm login\n" +
			"  tlm issues list --env production\n" +
			"  tlm issues show 2C7ZffP787kc --markdown > issue.md\n" +
			"  tlm upload ./dist --release \"$GIT_SHA\"",
		Version: fmt.Sprintf("%s (commit %s, built %s)",
			a.env.Build.Version, a.env.Build.Commit, a.env.Build.Date),
		SilenceErrors: true,
		SilenceUsage:  true,
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) > 0 {
				return usageError{fmt.Errorf("unknown command %q", args[0])}
			}
			return nil
		},
		// No command: the usage, on stderr, as a usage mistake.
		RunE: func(cmd *cobra.Command, _ []string) error {
			cmd.SetOut(a.env.Stderr)
			if err := cmd.Usage(); err != nil {
				return err
			}
			return silentError{ExitUsage}
		},
		PersistentPreRunE: func(*cobra.Command, []string) error {
			return a.setup()
		},
	}
	root.SetVersionTemplate("tlm {{.Version}}\n")
	root.SetOut(a.env.Stdout)
	root.SetErr(a.env.Stderr)
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return usageError{err}
	})

	f := root.PersistentFlags()
	f.StringVar(&a.flags.url, "url", "", "the Tlm to use (or $TLM_URL; default: the last login)")
	f.StringVar(&a.flags.org, "org", "", "the organization (or $TLM_ORG; implied when you have one)")
	f.StringVar(&a.flags.project, "project", "", "the project (or $TLM_PROJECT; implied when it's the only one)")
	f.BoolVar(&a.flags.json, "json", false, "print Tlm's JSON as is")
	f.StringVar(&a.flags.lang, "lang", "", "language of Tlm's messages: en or es (or $TLM_LANG)")
	f.BoolVarP(&a.flags.quiet, "quiet", "q", false, "only errors and the essential result")
	f.BoolVarP(&a.flags.verbose, "verbose", "v", false, "requests and timings on stderr (or $TLM_DEBUG=1)")
	f.BoolVar(&a.flags.noColor, "no-color", false, "no colours (or $NO_COLOR)")

	root.AddGroup(
		&cobra.Group{ID: "start", Title: "Signing in:"},
		&cobra.Group{ID: "setup", Title: "Setting a project up:"},
		&cobra.Group{ID: "work", Title: "What broke, how fast, who reports:"},
		&cobra.Group{ID: "ci", Title: "Source maps and setup checks:"},
		&cobra.Group{ID: "more", Title: "More:"},
	)
	add := func(group string, cmds ...*cobra.Command) {
		for _, c := range cmds {
			c.GroupID = group
			root.AddCommand(c)
		}
	}
	add("start", newLoginCmd(a), newLogoutCmd(a), newWhoamiCmd(a), newUseCmd(a))
	add("setup", newProjectsCmd(a), newDSNCmd(a), newUploadTokensCmd(a), newOriginsCmd(a))
	add("work", newIssuesCmd(a), newSpeedCmd(a), newSignalsCmd(a))
	add("ci", newUploadCmd(a), newTestCmd(a))
	add("more", newOrgsCmd(a), newMembersCmd(a), newAuditCmd(a), newOpenCmd(a), newAPICmd(a), newVersionCmd(a))
	root.SetHelpCommandGroupID("more")
	root.SetCompletionCommandGroupID("more")

	return root
}

// group is a command that only holds others: run alone, it shows its
// help on stderr as a usage mistake.
func group(a *app, use, short, example string, subs ...*cobra.Command) *cobra.Command {
	c := &cobra.Command{
		Use:     use,
		Short:   short,
		Example: example,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return usageError{fmt.Errorf("unknown action %q for %s (use %s)", args[0], cmd.Name(), actions(cmd))}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			return usageError{fmt.Errorf("%s needs an action: %s", cmd.Name(), actions(cmd))}
		},
	}
	c.AddCommand(subs...)
	_ = a
	return c
}

func actions(cmd *cobra.Command) string {
	var names []string
	for _, s := range cmd.Commands() {
		if s.IsAvailableCommand() {
			names = append(names, s.Name())
		}
	}
	return strings.Join(names, ", ")
}

// noArgs is cobra.NoArgs as a usage error.
func noArgs(cmd *cobra.Command, args []string) error {
	if len(args) > 0 {
		return usageError{fmt.Errorf("unexpected arguments: %s", strings.Join(args, " "))}
	}
	return nil
}

// argsBetween wants min to max arguments, named in usage.
func argsBetween(minN, maxN int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) < minN {
			return usageError{fmt.Errorf("missing arguments: tlm %s %s", parentPath(cmd), strings.TrimPrefix(cmd.Use, cmd.Name()+" "))}
		}
		if len(args) > maxN {
			return usageError{fmt.Errorf("unexpected arguments: %s", strings.Join(args[maxN:], " "))}
		}
		return nil
	}
}

// parentPath is the command's path without "tlm".
func parentPath(cmd *cobra.Command) string {
	return strings.TrimPrefix(cmd.CommandPath(), cmd.Root().Name()+" ")
}

// updateNotice says, at most once a day, when a newer tlm exists:
// only to a person at a terminal, never under --json or in a pipe.
func (a *app) updateNotice(ctx context.Context) {
	if a.noNotice || a.flags.json || !a.env.StdoutIsTerminal || !a.env.StderrIsTerminal ||
		a.env.CacheDir == "" || a.env.Getenv("TLM_NO_UPDATE_NOTIFIER") != "" {
		return
	}
	if n := a.updateChecker().Notice(ctx); n != "" {
		fmt.Fprintf(a.env.Stderr, "\n%s\nUpdate with: %s\n", n, a.upgradeHint())
	}
}

func (a *app) updateChecker() *update.Checker {
	state := ""
	if a.env.CacheDir != "" {
		state = filepath.Join(a.env.CacheDir, "tlm", "update.json")
	}
	return &update.Checker{
		Current:   a.env.Build.Version,
		StatePath: state,
		URL:       cmp.Or(a.env.UpdateURL, update.LatestURL),
		HTTP:      a.env.HTTPClient,
		Now:       a.env.Now,
	}
}

// upgradeHint is the command that upgrades tlm, by how it was
// installed.
func (a *app) upgradeHint() string {
	return update.UpgradeHint(update.Install{
		Exe:    a.env.Executable,
		GOOS:   cmp.Or(a.env.GOOS, runtime.GOOS),
		Home:   a.env.Getenv("HOME"),
		GOPATH: a.env.Getenv("GOPATH"),
		GOBIN:  a.env.Getenv("GOBIN"),
		Has: func(p string) bool {
			_, err := os.Stat(p)
			return err == nil
		},
	})
}
