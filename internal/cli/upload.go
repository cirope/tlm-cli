package cli

import (
	"errors"
	"fmt"

	"github.com/cirope/tlm-cli/internal/render"
	"github.com/cirope/tlm-cli/internal/sourcemaps"
	"github.com/spf13/cobra"
)

// failure is an error that exits 1 whatever it is: upload and test use
// only 0, 1 and 2.
type failure struct{ err error }

func (e failure) Error() string { return e.err.Error() }
func (e failure) Unwrap() error { return e.err }

// onlyUsageOrFailure maps err to exit 2 (usage) or 1 (anything else).
func onlyUsageOrFailure(err error) error {
	if err == nil {
		return nil
	}
	var se silentError
	if errors.As(err, &se) {
		return err
	}
	if code := exitCode(err); code == ExitUsage {
		return err
	}
	return failure{err}
}

func newUploadCmd(a *app) *cobra.Command {
	var (
		release, dsn, urlPrefix string
		del, dryRun             bool
	)
	c := &cobra.Command{
		Use:   "upload <dir>",
		Short: "Upload a build's source maps",
		Long: "Upload the source maps under <dir>, so Tlm shows your own code in\n" +
			"stack traces. Upload before you deploy.\n\n" +
			"Credentials: TLM_UPLOAD_TOKEN, an upload token read only from the\n" +
			"environment (create one with tlm upload-tokens create). The DSN says\n" +
			"where Tlm is.\n\n" +
			"Exit codes: 0 everything uploaded, 1 something failed, 2 wrong usage.",
		Example: "  TLM_UPLOAD_TOKEN=tlmu_… tlm upload ./dist --release \"$GIT_SHA\" --dsn \"$TLM_DSN\"\n" +
			"  tlm upload ./dist --release 1.4.2 --url-prefix /assets --delete\n" +
			"  tlm upload .next --release 1.4.2 --dry-run",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return usageError{errors.New("missing the directory to upload, e.g. tlm upload ./dist")}
			}
			return argsBetween(1, 1)(cmd, args)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return onlyUsageOrFailure(a.upload(cmd, args[0], release, dsn, urlPrefix, del, dryRun))
		},
	}
	f := c.Flags()
	f.StringVar(&release, "release", "", "the release your app reports, exactly (or $TLM_RELEASE)")
	f.StringVar(&dsn, "dsn", "", "your project's DSN: where Tlm is (or $TLM_DSN)")
	f.StringVar(&urlPrefix, "url-prefix", "/", "the URL path <dir> is served at")
	f.BoolVar(&del, "delete", false, "delete the .map files after a successful upload")
	f.BoolVar(&dryRun, "dry-run", false, "show what would be uploaded, without sending anything")
	return c
}

func (a *app) upload(cmd *cobra.Command, dir, release, dsn, urlPrefix string, del, dryRun bool) error {
	ctx := cmd.Context()
	release = trimmed(cmpOr(release, a.env.Getenv("TLM_RELEASE")))
	if release == "" {
		return usageError{errors.New("missing --release (or TLM_RELEASE). Use the same value your app reports as its release")}
	}
	// CI secrets are often pasted with a trailing newline.
	token := sourcemaps.Token(trimmed(a.env.Getenv("TLM_UPLOAD_TOKEN")))
	if token == "" && !dryRun {
		return usageError{errors.New("missing TLM_UPLOAD_TOKEN. Create an upload token with tlm upload-tokens create, or in your project's settings")}
	}
	dsn = trimmed(cmpOr(dsn, a.env.Getenv("TLM_DSN")))
	base := ""
	if dsn != "" || !dryRun {
		var err error
		if base, err = sourcemaps.BaseFromDSN(dsn); err != nil {
			return usageError{err}
		}
	}

	found, err := sourcemaps.Find(dir, sourcemaps.FindOptions{URLPrefix: urlPrefix})
	if err != nil {
		return err
	}
	for _, w := range found.Warnings {
		a.warn("%s", render.Safe(w))
	}
	if len(found.Maps) == 0 {
		return fmt.Errorf("no source maps found in %s. Turn them on in your bundler (e.g. build.sourcemap in Vite) and build again", dir)
	}
	if dryRun {
		for _, m := range found.Maps {
			a.out("would upload %s", render.Safe(m.Path))
		}
		where := ""
		if base != "" {
			where = " to " + render.Safe(base)
		}
		a.out("%d map(s) for release %s%s (dry run: nothing sent)", len(found.Maps), render.Safe(release), where)
		return nil
	}

	res := sourcemaps.Upload(ctx, found.Maps, sourcemaps.Options{
		Release:    release,
		Host:       base,
		Token:      token,
		HTTPClient: a.env.HTTPClient,
		Log:        func(line string) { a.out("%s", render.Safe(line)) },
		Sleep:      a.env.Sleep,
		UserAgent:  a.userAgent(),
	})
	for _, e := range res.Errors {
		fmt.Fprintf(a.env.Stderr, "error: %s\n", render.Safe(e))
	}
	if !res.OK {
		fmt.Fprintf(a.env.Stderr, "Uploaded %d of %d map(s) for release %s.\n", len(res.Uploaded), len(found.Maps), render.Safe(release))
		return silentError{ExitError}
	}
	a.out("Uploaded %d map(s) for release %s.", len(found.Maps), render.Safe(release))
	if del {
		if err := sourcemaps.Delete(found.Maps); err != nil {
			return fmt.Errorf("uploaded, but couldn't delete the .map files: %w", err)
		}
		a.out("Deleted the .map files.")
	}
	return nil
}

func newTestCmd(a *app) *cobra.Command {
	var dsn string
	c := &cobra.Command{
		Use:   "test",
		Short: "Send one test event and show Tlm's answer",
		Long: "Send one test event, the way a server SDK does, to check that the DSN\n" +
			"works. TLM_ENVIRONMENT and TLM_RELEASE go with it when set.\n\n" +
			"Exit codes: 0 accepted, 1 refused or unreachable, 2 wrong usage.",
		Example: "  tlm test --dsn \"$(tlm dsn)\"\n  TLM_DSN=https://tlm_…@tlm.hullop.com tlm test",
		Args:    noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			host, err := a.env.Hostname()
			if err != nil {
				host = ""
			}
			res, err := sourcemaps.SendTestEvent(cmd.Context(), sourcemaps.TestEventOptions{
				DSN:         trimmed(cmpOr(dsn, a.env.Getenv("TLM_DSN"))),
				Environment: a.env.Getenv("TLM_ENVIRONMENT"),
				Release:     a.env.Getenv("TLM_RELEASE"),
				Version:     a.env.Build.Version,
				Hostname:    host,
				HTTPClient:  a.env.HTTPClient,
				UserAgent:   a.userAgent(),
			})
			if errors.Is(err, sourcemaps.ErrUsage) {
				return usageError{err}
			}
			if err != nil {
				return failure{err}
			}
			a.out("%s", res.String())
			return nil
		},
	}
	c.Flags().StringVar(&dsn, "dsn", "", "your project's DSN (or $TLM_DSN)")
	return c
}
