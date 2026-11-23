package cli

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/cirope/tlm-cli/internal/api"
	"github.com/cirope/tlm-cli/internal/auth"
	"github.com/cirope/tlm-cli/internal/render"
	"github.com/spf13/cobra"
)

func newLoginCmd(a *app) *cobra.Command {
	var noBrowser bool
	c := &cobra.Command{
		Use:   "login",
		Short: "Sign in through the browser",
		Long: "Sign in by approving a code in the browser, on this machine or any other\n" +
			"(over SSH, in a container). The token is kept in the keychain, or in a\n" +
			"file only you can read where there's none.",
		Example: "  tlm login\n" +
			"  tlm login --url http://localhost:4000 --no-browser",
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			s, err := a.resolve()
			if err != nil {
				return err
			}
			anon, err := api.New(api.Options{
				URL: s.url, Lang: s.lang, UserAgent: a.userAgent(), HTTPClient: a.env.HTTPClient,
				Logger: a.logger, NoRetry: true,
			})
			if err != nil {
				return usageError{err}
			}
			host, err := a.env.Hostname()
			if err != nil || host == "" {
				host = "unknown"
			}
			open := a.env.StdoutIsTerminal && !noBrowser
			grant, err := auth.Login(ctx, anon, "tlm-cli on "+host,
				func(d *api.DeviceCode) { a.showCode(d, s.url, open) }, a.env.Sleep)
			if err != nil {
				return err
			}

			c, err := a.newClient(s.url, grant.Token, s.lang)
			if err != nil {
				return err
			}
			cfg, store, err := a.config()
			if err != nil {
				return err
			}
			previous, _ := store.Get(s.url)
			me, _, err := c.Me(ctx)
			if err == nil {
				err = store.Set(s.url, grant.Token)
			}
			if err == nil {
				cfg.URL = s.url
				err = cfg.Save(a.configPath())
			}
			if err != nil {
				// Not stored: the new token mustn't stay valid on the server either.
				_ = c.RevokeToken(ctx)
				return err
			}
			if previous != "" && previous != grant.Token {
				// Best effort: the old token is no longer kept here, so it
				// shouldn't stay valid either.
				if old, err := a.newClient(s.url, previous, s.lang); err == nil {
					_ = old.RevokeToken(ctx)
				}
			}
			a.out("Signed in to %s as %s (token %q, %s).", s.url, render.Safe(cmpOr(me.User.Email, grant.User.Email)),
				render.Safe(me.Token.Name), accessText(me.Token.Access))
			if s.fromEnv {
				a.warn("TLM_TOKEN is set, and it wins over the token just stored")
			}
			a.nextSteps(me)
			return nil
		},
	}
	c.Flags().BoolVar(&noBrowser, "no-browser", false, "print the URL without opening a browser")
	return c
}

// showCode tells the person what to approve. The device code itself is a
// secret and is never printed. Only a page on the Tlm being signed
// in to is opened.
func (a *app) showCode(d *api.DeviceCode, tlm string, open bool) {
	w := a.env.Stderr
	fmt.Fprintf(w, "First copy your one-time code: %s\n", render.Safe(d.UserCode))
	uri := render.Safe(d.VerificationURI)
	if open && d.VerificationURIComplete != "" && sameOrigin(d.VerificationURIComplete, tlm) {
		fmt.Fprintf(w, "Then open %s (opening it for you…)\n", uri)
		_ = a.env.OpenBrowser(d.VerificationURIComplete)
	} else {
		fmt.Fprintf(w, "Then open %s and enter it.\n", uri)
	}
	fmt.Fprintln(w, "Waiting for you to approve…")
}

func sameOrigin(raw, tlm string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	o, err := api.OriginOf(u.Scheme + "://" + u.Host)
	return err == nil && o == tlm
}

func accessText(access string) string {
	if access == "read" {
		return "read-only access"
	}
	if access == "" {
		access = "write"
	}
	return access + " access"
}

func cmpOr(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func newLogoutCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:     "logout",
		Short:   "Revoke this machine's token and forget it",
		Example: "  tlm logout\n  tlm logout --url http://localhost:4000",
		Args:    noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := a.resolve()
			if err != nil {
				return err
			}
			if s.fromEnv {
				return usageError{errors.New("TLM_TOKEN is set: unset it to sign out (revoke the token in Tlm's settings, under Access tokens)")}
			}
			if s.token == "" {
				a.out("Not signed in to %s.", s.url)
				return nil
			}
			c, err := a.newClient(s.url, s.token, s.lang)
			if err != nil {
				return err
			}
			if err := c.RevokeToken(cmd.Context()); err != nil && !errors.Is(err, api.ErrUnauthorized) {
				a.warn("couldn't revoke the token on %s (%s); it may still be valid: revoke it in Tlm's settings, under Access tokens.",
					s.url, render.Safe(err.Error()))
			}
			if err := a.store.Delete(s.url); err != nil {
				return err
			}
			a.out("Signed out of %s.", s.url)
			return nil
		},
	}
}

func newWhoamiCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:     "whoami",
		Short:   "Who the token is, its access, organizations",
		Example: "  tlm whoami\n  tlm whoami --json",
		Args:    noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, s, err := a.client()
			if err != nil {
				return err
			}
			me, raw, err := c.Me(cmd.Context())
			if err != nil {
				return err
			}
			if a.flags.json {
				return a.printJSON(raw)
			}
			name := ""
			if n := me.User.FullName(); n != "" {
				name = " (" + n + ")"
			}
			a.out("%s", render.Safe(fmt.Sprintf("%s%s on %s", me.User.Email, name, s.url)))
			only := ""
			if me.Token.Organization != "" {
				only = ", only " + me.Token.Organization
			}
			from := ""
			if s.fromEnv {
				from = " from TLM_TOKEN"
			}
			a.out("%s", render.Safe(fmt.Sprintf("Token %q%s: %s%s", me.Token.Name, from, accessText(me.Token.Access), only)))
			if a.repoPath != "" {
				a.out("Repository: %s (%s)", render.Safe(givenText(a.repo)), a.repoPath)
			}
			if d, ok := a.defaultFor(s.url); ok {
				a.out("Default: %s", render.Safe(defaultText(d)))
			}
			if len(me.Organizations) > 0 {
				rows := make([][]string, 0, len(me.Organizations))
				for _, o := range me.Organizations {
					rows = append(rows, []string{o.Slug, o.Name, o.Role})
				}
				a.out("\n%s", render.Table([]string{"ORGANIZATION", "NAME", "ROLE"}, rows))
			}
			return nil
		},
	}
}

// trimmed is s without surrounding space; CI secrets are often pasted
// with a trailing newline.
func trimmed(s string) string { return strings.TrimSpace(s) }

// nextSteps tells someone who just signed in where to go from here.
func (a *app) nextSteps(me *api.Me) {
	switch orgs := me.Organizations; len(orgs) {
	case 0:
		a.progress("You aren't in any organization yet: ask an admin to add you.")
	case 1:
		a.progress("You're in %s (%s). Next: tlm issues list", render.Safe(orgs[0].Name), render.Safe(orgs[0].Role))
	default:
		a.progress("You're in %d organizations. Pick one: tlm use <org>[/<project>]", len(orgs))
	}
	a.progress("Adding Tlm to an app? tlm guide says how, step by step.")
}
