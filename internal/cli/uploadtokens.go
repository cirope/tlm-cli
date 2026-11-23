package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/cirope/tlm-cli/internal/api"
	"github.com/cirope/tlm-cli/internal/render"
	"github.com/spf13/cobra"
)

func newUploadTokensCmd(a *app) *cobra.Command {
	return group(a, "upload-tokens", "Tokens CI uploads source maps with",
		"  tlm upload-tokens list\n  tlm upload-tokens create \"GitHub Actions\"",
		newUploadTokensListCmd(a), newUploadTokensCreateCmd(a), newUploadTokensRevokeCmd(a))
}

func uploadTokensPath(org, project string) string {
	return api.ProjectPath(org, project) + "/upload-tokens"
}

func newUploadTokensListCmd(a *app) *cobra.Command {
	var l listFlags
	c := &cobra.Command{
		Use:     "list",
		Short:   "The project's upload tokens (admins)",
		Example: "  tlm upload-tokens list\n  tlm upload-tokens list --project shop --json",
		Args:    noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			cl, org, project, err := a.project(ctx, "")
			if err != nil {
				return err
			}
			now := a.env.Now()
			return printList(ctx, a, cl, &l, uploadTokensPath(org, project), nil, func(ts []api.UploadToken) string {
				rows := make([][]string, 0, len(ts))
				for _, t := range ts {
					used := "never"
					if t.LastUsedAt != nil {
						used = render.Ago(*t.LastUsedAt, now)
					}
					by := ""
					if t.CreatedBy != nil {
						by = t.CreatedBy.FullName()
					}
					rows = append(rows, []string{t.ID, t.Name, "tlmu_…" + t.Hint, used, by, render.Ago(t.CreatedAt, now)})
				}
				return render.Table([]string{"ID", "NAME", "TOKEN", "LAST USED", "CREATED BY", "CREATED"}, rows)
			}, "No upload tokens. Create one with: tlm upload-tokens create <name>")
		},
	}
	l.add(c)
	return c
}

// ghSecret is where --gh-secret stores a new token: a GitHub Actions
// secret, of an environment and a repository when given.
type ghSecret struct{ name, env, repo string }

func (g ghSecret) args() []string {
	args := []string{"secret", "set", g.name}
	if g.env != "" {
		args = append(args, "--env", g.env)
	}
	if g.repo != "" {
		args = append(args, "--repo", g.repo)
	}
	return args
}

func (g ghSecret) where() string {
	parts := []string{}
	if g.env != "" {
		parts = append(parts, "environment "+g.env)
	}
	if g.repo != "" {
		parts = append(parts, g.repo)
	}
	if len(parts) == 0 {
		return ""
	}
	return " (" + strings.Join(parts, ", ") + ")"
}

func newUploadTokensCreateCmd(a *app) *cobra.Command {
	var gh ghSecret
	c := &cobra.Command{
		Use:   "create <name>",
		Short: "Create one; prints the secret once, or stores it in GitHub",
		Long: "Create an upload token and print its secret, this once. With\n" +
			"--gh-secret, store it instead as that GitHub Actions secret, through the\n" +
			"GitHub CLI (gh secret set, of the repository in this directory unless\n" +
			"--gh-repo says): the secret never shows, nor goes through a shell.",
		Example: "  tlm upload-tokens create \"GitHub Actions\"\n" +
			"  tlm upload-tokens create \"GitHub Actions\" --gh-secret TLM_UPLOAD_TOKEN\n" +
			"  tlm upload-tokens create \"Deploys\" --gh-secret TLM_UPLOAD_TOKEN --gh-env production --gh-repo acme/shop",
		Args: argsBetween(1, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if gh.name == "" && (gh.env != "" || gh.repo != "") {
				return usageError{errors.New("--gh-env and --gh-repo go with --gh-secret")}
			}
			if gh.name != "" && a.flags.json {
				return usageError{errors.New("--gh-secret keeps the secret out of the output; drop --json")}
			}
			ctx := cmd.Context()
			if gh.name != "" {
				if _, err := a.env.Exec(ctx, nil, "gh", "auth", "status"); err != nil {
					return fmt.Errorf("--gh-secret needs the GitHub CLI, signed in: install gh, then run gh auth login (%w)", err)
				}
			}
			cl, org, project, err := a.project(ctx, "")
			if err != nil {
				return err
			}
			var t api.UploadToken
			raw, err := cl.Send(ctx, http.MethodPost, uploadTokensPath(org, project), nil, map[string]string{"name": args[0]}, &t)
			if err != nil {
				return err
			}
			if gh.name != "" {
				return a.storeInGitHub(ctx, cl, uploadTokensPath(org, project), &t, gh)
			}
			if a.flags.json {
				if err := a.printJSON(raw); err != nil {
					return err
				}
			} else {
				a.progress("Upload token %q (%s):", render.Safe(t.Name), render.Safe(t.ID))
				a.out("%s", render.Safe(t.Secret))
			}
			fmt.Fprintln(a.env.Stderr, "This is the only time it's shown. Store it as TLM_UPLOAD_TOKEN in your CI's secrets.")
			return nil
		},
	}
	f := c.Flags()
	f.StringVar(&gh.name, "gh-secret", "", "store it as this GitHub Actions secret, with gh, instead of printing it")
	f.StringVar(&gh.env, "gh-env", "", "with --gh-secret: the GitHub environment's secret")
	f.StringVar(&gh.repo, "gh-repo", "", "with --gh-secret: the repository, owner/name (default: this directory's)")
	return c
}

// storeInGitHub hands the new token's secret to gh secret set on its
// stdin. If that fails, the token is revoked: nobody would have it.
func (a *app) storeInGitHub(ctx context.Context, cl *api.Client, path string, t *api.UploadToken, gh ghSecret) error {
	secret := api.Token(t.Secret)
	out, err := a.env.Exec(ctx, strings.NewReader(t.Secret), "gh", gh.args()...)
	if err == nil {
		a.out("Stored upload token %q (%s) as the GitHub secret %s%s.",
			render.Safe(t.Name), render.Safe(t.ID), render.Safe(gh.name), render.Safe(gh.where()))
		return nil
	}
	why := strings.TrimSpace(secret.Scrub(string(out)))
	if why == "" {
		why = secret.Scrub(err.Error())
	}
	revoked := "revoked it"
	if _, rerr := cl.Send(ctx, http.MethodDelete, path+"/"+api.Seg(t.ID), nil, nil, nil); rerr != nil {
		revoked = "couldn't revoke it, so do: tlm upload-tokens revoke " + t.ID
	}
	return fmt.Errorf("couldn't store it with gh secret set: %s (the new token %s: %s)",
		render.Safe(why), render.Safe(t.ID), revoked)
}

func newUploadTokensRevokeCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:     "revoke <id>",
		Short:   "Revoke it: uploads with it stop at once",
		Example: "  tlm upload-tokens revoke Upt7Kq2XpAb2",
		Args:    argsBetween(1, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			cl, org, project, err := a.project(ctx, "")
			if err != nil {
				return err
			}
			if _, err := cl.Send(ctx, http.MethodDelete, uploadTokensPath(org, project)+"/"+api.Seg(args[0]), nil, nil, nil); err != nil {
				return err
			}
			a.out("Revoked.")
			return nil
		},
	}
}
