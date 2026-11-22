package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/cirope/tlm-cli/internal/api"
	"github.com/cirope/tlm-cli/internal/render"
	"github.com/spf13/cobra"
)

func newProjectsCmd(a *app) *cobra.Command {
	return group(a, "projects", "List, create and show projects",
		"  tlm projects list\n  tlm projects create \"Web shop\"",
		newProjectsListCmd(a), newProjectsCreateCmd(a), newProjectsShowCmd(a),
		newProjectsEditCmd(a), newProjectsMuteCmd(a, true), newProjectsMuteCmd(a, false))
}

func newProjectsListCmd(a *app) *cobra.Command {
	var l listFlags
	c := &cobra.Command{
		Use:     "list",
		Short:   "Projects of the organization",
		Example: "  tlm projects list\n  tlm projects list --org acme --all --json",
		Args:    noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			cl, _, err := a.client()
			if err != nil {
				return err
			}
			org, err := a.resolver(cl).Org(ctx)
			if err != nil {
				return err
			}
			a.org = org
			now := a.env.Now()
			return printList(ctx, a, cl, &l, api.OrgPath(org)+"/projects", nil, func(ps []api.Project) string {
				rows := make([][]string, 0, len(ps))
				for _, p := range ps {
					rows = append(rows, []string{p.Slug, p.Name, strings.Join(p.Platforms, ", "),
						strconv.Itoa(p.UnresolvedCount), render.Ago(p.CreatedAt, now)})
				}
				return render.Table([]string{"SLUG", "NAME", "PLATFORMS", "UNRESOLVED", "CREATED"}, rows)
			}, "No projects in "+render.Safe(org)+".")
		},
	}
	l.add(c)
	return c
}

func (a *app) printProject(p *api.Project) {
	a.out("%s (%s)", render.Safe(p.Name), render.Safe(p.Slug))
	a.out("%s", render.Safe(p.URL))
	if p.DSN != nil {
		a.out("DSN: %s", render.Safe(*p.DSN))
	}
	if p.Muted != nil && *p.Muted {
		a.out("Issue emails: muted for you")
	}
}

func newProjectsCreateCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:     "create <name>",
		Short:   "Create one (admins); prints its DSN",
		Example: "  tlm projects create \"Web shop\"\n  tlm projects create Shop --org acme --json",
		Args:    argsBetween(1, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			cl, _, err := a.client()
			if err != nil {
				return err
			}
			org, err := a.resolver(cl).Org(ctx)
			if err != nil {
				return err
			}
			a.org = org
			p, raw, err := cl.CreateProject(ctx, org, args[0])
			if err != nil {
				return err
			}
			if a.flags.json {
				return a.printJSON(raw)
			}
			a.printProject(p)
			a.progress("Next, add it to the app: tlm guide")
			return nil
		},
	}
}

// fetchProject gets the project named by args (or implied).
func (a *app) fetchProject(ctx context.Context, args []string) (*api.Project, []byte, error) {
	slug := ""
	if len(args) > 0 {
		slug = args[0]
	}
	c, org, project, err := a.project(ctx, slug)
	if err != nil {
		return nil, nil, err
	}
	return c.Project(ctx, org, project)
}

func newProjectsShowCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:     "show [<project>]",
		Short:   "One project, with its DSN",
		Example: "  tlm projects show\n  tlm projects show shop --json",
		Args:    argsBetween(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, raw, err := a.fetchProject(cmd.Context(), args)
			if err != nil {
				return err
			}
			if a.flags.json {
				return a.printJSON(raw)
			}
			a.printProject(p)
			return nil
		},
	}
}

func newDSNCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:     "dsn [<project>]",
		Short:   "Only the DSN, for scripts: $(tlm dsn)",
		Example: "  tlm dsn\n  export TLM_DSN=$(tlm dsn shop)",
		Args:    argsBetween(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, raw, err := a.fetchProject(cmd.Context(), args)
			if err != nil {
				return err
			}
			if p.DSN == nil {
				return fmt.Errorf("the answer for %s has no DSN", render.Safe(p.Slug))
			}
			if a.flags.json {
				return a.printJSON(raw)
			}
			a.out("%s", render.Safe(*p.DSN))
			return nil
		},
	}
}

func newProjectsEditCmd(a *app) *cobra.Command {
	var name string
	c := &cobra.Command{
		Use:     "edit [<project>] --name <name>",
		Short:   "Rename it (admins); the slug stays",
		Example: "  tlm projects edit shop --name Storefront",
		Args:    argsBetween(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !cmd.Flags().Changed("name") {
				return usageError{errors.New("say the new name with --name")}
			}
			ctx := cmd.Context()
			slug := ""
			if len(args) > 0 {
				slug = args[0]
			}
			cl, org, project, err := a.project(ctx, slug)
			if err != nil {
				return err
			}
			var p api.Project
			raw, err := cl.Send(ctx, http.MethodPatch, api.ProjectPath(org, project), nil, map[string]string{"name": name}, &p)
			if err != nil {
				return err
			}
			if a.flags.json {
				return a.printJSON(raw)
			}
			a.printProject(&p)
			return nil
		},
	}
	c.Flags().StringVar(&name, "name", "", "the new name")
	return c
}

func newProjectsMuteCmd(a *app, mute bool) *cobra.Command {
	use, short, method, done := "mute", "Turn its issue emails off, for you", http.MethodPut, "off"
	if !mute {
		use, short, method, done = "unmute", "Turn its issue emails back on, for you", http.MethodDelete, "on"
	}
	return &cobra.Command{
		Use:     use + " [<project>]",
		Short:   short,
		Example: "  tlm projects " + use + " shop",
		Args:    argsBetween(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			slug := ""
			if len(args) > 0 {
				slug = args[0]
			}
			cl, org, project, err := a.project(ctx, slug)
			if err != nil {
				return err
			}
			if _, err := cl.Send(ctx, method, api.ProjectPath(org, project)+"/mute", nil, nil, nil); err != nil {
				return err
			}
			a.out("%s's issue emails are %s for you.", render.Safe(project), done)
			return nil
		},
	}
}
