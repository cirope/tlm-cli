package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/cirope/tlm-cli/internal/api"
	"github.com/cirope/tlm-cli/internal/auth"
	"github.com/cirope/tlm-cli/internal/render"
	"github.com/cirope/tlm-cli/internal/target"
)

// DefaultURL is the Tlm used when nothing says which.
const DefaultURL = "https://tlm.hullop.com"

// app is one run of tlm: its environment, global flags, and what
// it worked out along the way.
type app struct {
	env    Env
	flags  globalFlags
	logger *slog.Logger

	cfg     *auth.Config
	store   auth.TokenStore
	session *session
	org     string // Once known, for error hints.
	// repo is what the repository's .tlm.toml names, read by client.
	repo     target.Given
	repoPath string
	repoRead bool
	// noNotice: the command said whether there's a newer tlm already.
	noNotice bool
}

type globalFlags struct {
	url, org, project, lang       string
	json, quiet, verbose, noColor bool
}

// setup applies the global flags, once they are parsed.
func (a *app) setup() error {
	if a.env.Getenv("TLM_DEBUG") == "1" {
		a.flags.verbose = true
	}
	if a.env.Getenv("NO_COLOR") != "" {
		a.flags.noColor = true
	}
	level := slog.Level(100) // Off.
	if a.flags.verbose {
		level = slog.LevelDebug
	}
	a.logger = slog.New(slog.NewTextHandler(a.env.Stderr, &slog.HandlerOptions{Level: level}))
	return nil
}

func (a *app) dir() string { return filepath.Join(a.env.ConfigDir, "tlm") }

func (a *app) configPath() string { return filepath.Join(a.dir(), "config.json") }

// config is tlm's settings and where tokens are kept.
func (a *app) config() (*auth.Config, auth.TokenStore, error) {
	if a.cfg != nil {
		return a.cfg, a.store, nil
	}
	if a.env.ConfigDir == "" {
		return nil, nil, errors.New("can't find your config directory to keep the token in")
	}
	cfg, err := auth.LoadConfig(a.configPath())
	if err != nil {
		return nil, nil, err
	}
	warn := func(s string) { fmt.Fprintf(a.env.Stderr, "warning: %s\n", s) }
	file := &auth.FileStore{Path: filepath.Join(a.dir(), "tokens.json"), Warn: warn}
	store := &auth.Store{
		Keyring: a.env.Keyring,
		File:    file,
		Notice: func(path string) {
			fmt.Fprintf(a.env.Stderr, "No keychain found: the token is kept in %s, readable only by you.\n", path)
		},
	}
	a.cfg, a.store = &cfg, store
	return a.cfg, a.store, nil
}

// session is the Tlm in use, the token for it and where it came
// from, and the language asked for.
type session struct {
	url     string
	token   api.Token
	fromEnv bool
	lang    string
}

// resolve works out the session: URL from --url, TLM_URL, the last
// login, the default; token from TLM_TOKEN, else the one stored
// for that URL (may be empty).
func (a *app) resolve() (*session, error) {
	if a.session != nil {
		return a.session, nil
	}
	s := &session{}
	lang := strings.TrimSpace(a.flags.lang)
	if lang == "" {
		lang = strings.TrimSpace(a.env.Getenv("TLM_LANG"))
	}
	if lang != "" && lang != "en" && lang != "es" {
		return nil, usageError{fmt.Errorf("unknown language %q: use en or es", lang)}
	}
	s.lang = lang

	cfg, store, err := a.config()
	if err != nil {
		return nil, err
	}
	raw := strings.TrimSpace(a.flags.url)
	if raw == "" {
		raw = strings.TrimSpace(a.env.Getenv("TLM_URL"))
	}
	switch {
	case raw != "":
		if s.url, err = api.OriginOf(raw); err != nil {
			return nil, usageError{err}
		}
	case cfg.URL != "":
		s.url = cfg.URL
	default:
		s.url = DefaultURL
	}

	if t := strings.TrimSpace(a.env.Getenv("TLM_TOKEN")); t != "" {
		s.token, s.fromEnv = api.Token(t), true
	} else {
		tok, err := store.Get(s.url)
		if err != nil && !errors.Is(err, auth.ErrNoToken) {
			return nil, err
		}
		s.token = tok
	}
	a.session = s
	return s, nil
}

// notSignedIn is the error for a Tlm without a token.
type notSignedIn struct{ url string }

func (e notSignedIn) Error() string { return fmt.Sprintf("not signed in to %s.", e.url) }

func (a *app) userAgent() string {
	return fmt.Sprintf("tlm-cli/%s (%s; %s)", a.env.Build.Version, runtime.GOOS, runtime.GOARCH)
}

// newClient returns an API client for url with tok (which may be empty).
func (a *app) newClient(url string, tok api.Token, lang string) (*api.Client, error) {
	c, err := api.New(api.Options{
		URL:        url,
		Token:      tok,
		Lang:       lang,
		UserAgent:  a.userAgent(),
		HTTPClient: a.env.HTTPClient,
		Logger:     a.logger,
		Sleep:      a.env.Sleep,
	})
	if err != nil {
		return nil, usageError{err}
	}
	return c, nil
}

// client is a signed-in client for the session.
func (a *app) client() (*api.Client, *session, error) {
	s, err := a.resolve()
	if err != nil {
		return nil, nil, err
	}
	if s.token == "" {
		return nil, nil, notSignedIn{s.url}
	}
	if err := a.readRepoFile(); err != nil {
		return nil, nil, err
	}
	c, err := a.newClient(s.url, s.token, s.lang)
	return c, s, err
}

// readRepoFile reads the repository's .tlm.toml, once.
func (a *app) readRepoFile() error {
	if a.env.Dir == "" || a.repoRead {
		return nil
	}
	g, path, err := target.FindRepoFile(a.env.Dir)
	if err != nil {
		return err
	}
	a.repo, a.repoPath, a.repoRead = g, path, true
	return nil
}

// resolver chooses the organization and project for c.
func (a *app) resolver(c *api.Client) *target.Resolver {
	r := &target.Resolver{
		API:  c,
		Flag: target.Given{Org: a.flags.org, Project: a.flags.project},
		Env:  target.Given{Org: a.env.Getenv("TLM_ORG"), Project: a.env.Getenv("TLM_PROJECT")},
		Repo: a.repo,
	}
	if d, ok := a.defaultFor(c.Origin()); ok {
		r.Default = target.Given{Org: d.Org, Project: d.Project}
	}
	return r
}

// defaultFor is what tlm use chose for the Tlm at url.
func (a *app) defaultFor(url string) (auth.Default, bool) {
	cfg, _, err := a.config()
	if err != nil || cfg.Defaults == nil {
		return auth.Default{}, false
	}
	d, ok := cfg.Defaults[url]
	return d, ok
}

// project is a signed-in client and the organization and project to work
// on; slug, when given, names the project.
func (a *app) project(ctx context.Context, slug string) (*api.Client, string, string, error) {
	c, _, err := a.client()
	if err != nil {
		return nil, "", "", err
	}
	r := a.resolver(c)
	org, err := r.Org(ctx)
	if err != nil {
		return nil, "", "", err
	}
	a.org = org
	if slug == "" {
		if slug, err = r.Project(ctx, org); err != nil {
			return nil, "", "", err
		}
	}
	return c, org, slug, nil
}

// printJSON writes Tlm's JSON as is.
func (a *app) printJSON(raw []byte) error { return render.JSON(a.env.Stdout, raw) }

// out writes a result line to stdout.
func (a *app) out(format string, args ...any) { fmt.Fprintf(a.env.Stdout, format+"\n", args...) }

// progress writes to stderr unless --quiet.
func (a *app) progress(format string, args ...any) {
	if !a.flags.quiet {
		fmt.Fprintf(a.env.Stderr, format+"\n", args...)
	}
}

// warn writes a warning to stderr, even with --quiet.
func (a *app) warn(format string, args ...any) {
	fmt.Fprintf(a.env.Stderr, "warning: "+format+"\n", args...)
}
