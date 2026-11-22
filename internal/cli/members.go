package cli

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/cirope/tlm-cli/internal/api"
	"github.com/cirope/tlm-cli/internal/render"
	"github.com/spf13/cobra"
)

func newMembersCmd(a *app) *cobra.Command {
	return group(a, "members", "People in the organization (admins)",
		"  tlm members list\n  tlm members role bob@example.com admin\n  tlm members remove bob@example.com",
		newMembersListCmd(a), newMembersRoleCmd(a), newMembersRemoveCmd(a))
}

// orgClient is a signed-in client and the organization (--org, or the
// only one).
func (a *app) orgClient(ctx context.Context) (*api.Client, string, error) {
	return a.orgArg(ctx, nil)
}

func newMembersListCmd(a *app) *cobra.Command {
	var l listFlags
	c := &cobra.Command{
		Use:     "list",
		Short:   "Members, their roles, and who can enter without single sign-on",
		Example: "  tlm members list\n  tlm members list --org acme --json",
		Args:    noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			cl, org, err := a.orgClient(ctx)
			if err != nil {
				return err
			}
			return printList(ctx, a, cl, &l, api.OrgPath(org)+"/members", nil, func(ms []api.Member) string {
				rows := make([][]string, 0, len(ms))
				for _, m := range ms {
					local := "no"
					if m.LocalAccess {
						local = "yes"
					}
					rows = append(rows, []string{m.Email, m.FullName(), m.Role, local})
				}
				return render.Table([]string{"EMAIL", "NAME", "ROLE", "WITHOUT SSO"}, rows)
			}, "No members in "+render.Safe(org)+".")
		},
	}
	l.add(c)
	return c
}

// member finds someone in org by email (any case) or user id.
func (a *app) member(ctx context.Context, cl *api.Client, org, who string) (*api.Member, error) {
	page, err := api.List[api.Member](ctx, cl, api.OrgPath(org)+"/members", nil, true)
	if err != nil {
		return nil, err
	}
	for _, m := range page.Items {
		if strings.EqualFold(m.Email, who) || m.ID == who {
			return &m, nil
		}
	}
	return nil, &api.Error{Status: http.StatusNotFound, Code: "not_found",
		Message: fmt.Sprintf("%s isn't a member of %s", render.Safe(who), render.Safe(org))}
}

func checkRole(role string) error {
	if role != "member" && role != "admin" {
		return usageError{fmt.Errorf("unknown role %q: use member or admin", role)}
	}
	return nil
}

func newMembersRoleCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "role <email> <member|admin>",
		Short: "Change someone's role",
		Long: "Change someone's role. Not your own, and not the last admin's (nor,\n" +
			"with single sign-on on, the last who can enter without it).",
		Example: "  tlm members role bob@example.com admin",
		Args:    argsBetween(2, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := checkRole(args[1]); err != nil {
				return err
			}
			ctx := cmd.Context()
			cl, org, err := a.orgClient(ctx)
			if err != nil {
				return err
			}
			m, err := a.member(ctx, cl, org, args[0])
			if err != nil {
				return err
			}
			var out api.Member
			raw, err := cl.Send(ctx, http.MethodPatch, api.OrgPath(org)+"/members/"+api.Seg(m.ID), nil, map[string]string{"role": args[1]}, &out)
			if err != nil {
				return err
			}
			if a.flags.json {
				return a.printJSON(raw)
			}
			a.out("%s is now %s in %s.", render.Safe(out.Email), render.Safe(out.Role), render.Safe(org))
			return nil
		},
	}
}

func newMembersRemoveCmd(a *app) *cobra.Command {
	var yes bool
	c := &cobra.Command{
		Use:   "remove <email>",
		Short: "Remove someone from the organization",
		Long: "Remove someone from the organization. Not yourself, the last admin or,\n" +
			"with single sign-on on, the last who can enter without it. People are\n" +
			"added in the web app.",
		Example: "  tlm members remove bob@example.com\n  tlm members remove bob@example.com --yes",
		Args:    argsBetween(1, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			cl, org, err := a.orgClient(ctx)
			if err != nil {
				return err
			}
			m, err := a.member(ctx, cl, org, args[0])
			if err != nil {
				return err
			}
			if err := a.confirm(yes, fmt.Sprintf("Remove %s from %s?", render.Safe(m.Email), render.Safe(org))); err != nil {
				return err
			}
			if _, err := cl.Send(ctx, http.MethodDelete, api.OrgPath(org)+"/members/"+api.Seg(m.ID), nil, nil, nil); err != nil {
				return err
			}
			a.out("Removed %s from %s.", render.Safe(m.Email), render.Safe(org))
			return nil
		},
	}
	addYes(c, &yes)
	return c
}
