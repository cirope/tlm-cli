package cli

import (
	"context"
	"errors"
	"net/http"

	"github.com/cirope/tlm-cli/internal/api"
	"github.com/cirope/tlm-cli/internal/render"
	"github.com/spf13/cobra"
)

func newOrgsCmd(a *app) *cobra.Command {
	return group(a, "orgs", "Your organizations",
		"  tlm orgs list\n  tlm orgs show acme\n  tlm orgs edit acme --name \"Acme Labs\"",
		newOrgsListCmd(a), newOrgsShowCmd(a), newOrgsEditCmd(a))
}

func newOrgsListCmd(a *app) *cobra.Command {
	var l listFlags
	c := &cobra.Command{
		Use:     "list",
		Short:   "Your organizations and your role in each",
		Example: "  tlm orgs list\n  tlm orgs list --json",
		Args:    noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cl, _, err := a.client()
			if err != nil {
				return err
			}
			now := a.env.Now()
			return printList(cmd.Context(), a, cl, &l, "/orgs", nil, func(os []api.Org) string {
				rows := make([][]string, 0, len(os))
				for _, o := range os {
					rows = append(rows, []string{o.Slug, o.Name, o.Role, render.Ago(o.CreatedAt, now)})
				}
				return render.Table([]string{"SLUG", "NAME", "ROLE", "CREATED"}, rows)
			}, "You're in no organizations.")
		},
	}
	l.add(c)
	return c
}

// orgArg is the organization named by args, or the one implied.
func (a *app) orgArg(ctx context.Context, args []string) (*api.Client, string, error) {
	cl, _, err := a.client()
	if err != nil {
		return nil, "", err
	}
	org := ""
	if len(args) > 0 {
		org = args[0]
	} else if org, err = a.resolver(cl).Org(ctx); err != nil {
		return nil, "", err
	}
	a.org = org
	return cl, org, nil
}

func (a *app) printOrg(o *api.Org) {
	a.out("%s (%s)", render.Safe(o.Name), render.Safe(o.Slug))
	a.out("Your role: %s", render.Safe(o.Role))
}

func newOrgsShowCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:     "show [<org>]",
		Short:   "One organization",
		Example: "  tlm orgs show\n  tlm orgs show acme --json",
		Args:    argsBetween(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, org, err := a.orgArg(cmd.Context(), args)
			if err != nil {
				return err
			}
			var o api.Org
			raw, err := cl.Get(cmd.Context(), api.OrgPath(org), nil, &o)
			if err != nil {
				return err
			}
			if a.flags.json {
				return a.printJSON(raw)
			}
			a.printOrg(&o)
			return nil
		},
	}
}

func newOrgsEditCmd(a *app) *cobra.Command {
	var name string
	c := &cobra.Command{
		Use:   "edit [<org>] --name <name>",
		Short: "Rename it (admins); the slug stays",
		Long: "Rename the organization. Its slug never changes: identity providers\n" +
			"are set up with it.",
		Example: "  tlm orgs edit acme --name \"Acme Labs\"",
		Args:    argsBetween(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !cmd.Flags().Changed("name") {
				return usageError{errors.New("say the new name with --name")}
			}
			cl, org, err := a.orgArg(cmd.Context(), args)
			if err != nil {
				return err
			}
			var o api.Org
			raw, err := cl.Send(cmd.Context(), http.MethodPatch, api.OrgPath(org), nil, map[string]string{"name": name}, &o)
			if err != nil {
				return err
			}
			if a.flags.json {
				return a.printJSON(raw)
			}
			a.printOrg(&o)
			return nil
		},
	}
	c.Flags().StringVar(&name, "name", "", "the new name")
	return c
}
