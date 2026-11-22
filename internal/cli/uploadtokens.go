package cli

import (
	"fmt"
	"net/http"

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

func newUploadTokensCreateCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:     "create <name>",
		Short:   "Create one; prints the secret once",
		Example: "  tlm upload-tokens create \"GitHub Actions\"",
		Args:    argsBetween(1, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			cl, org, project, err := a.project(ctx, "")
			if err != nil {
				return err
			}
			var t api.UploadToken
			raw, err := cl.Send(ctx, http.MethodPost, uploadTokensPath(org, project), nil, map[string]string{"name": args[0]}, &t)
			if err != nil {
				return err
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
