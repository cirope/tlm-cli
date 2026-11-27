package cli

import (
	"net/http"
	"strings"

	"github.com/cirope/tlm-cli/internal/api"
	"github.com/cirope/tlm-cli/internal/render"
	"github.com/spf13/cobra"
)

const originsLong = `The web page origins a project's browser events come from, and what
Tlm does with them. Admins.

Events from an origin nobody answered yet are kept, and its admins are
asked, by email and on the project's page, to allow or block it. An
allowed origin's events are kept for good; a blocked origin's events are
refused (403). Allow an app's domains ahead of time, so it never waits.

An origin is a scheme and a host, with an optional port; a bare host
means https://. A wildcard host, '*.example.com', matches every subdomain,
with the same scheme and port, not example.com itself, which needs its own
origin.`

func newOriginsCmd(a *app) *cobra.Command {
	c := group(a, "origins", "Allowed domains of browser events (admins)",
		"  tlm origins list\n  tlm origins allow shop.example.com\n  tlm origins allow '*.example.com'",
		newOriginsListCmd(a), newOriginsPutCmd(a, "allowed"), newOriginsPutCmd(a, "blocked"), newOriginsForgetCmd(a))
	c.Long = originsLong
	return c
}

// originArg is what the person typed as an origin: a bare host means
// https://, since Tlm is strict and never guesses.
func originArg(s string) string {
	if strings.Contains(s, "://") {
		return s
	}
	return "https://" + s
}

func newOriginsListCmd(a *app) *cobra.Command {
	var l listFlags
	c := &cobra.Command{
		Use:     "list",
		Short:   "Allowed domains and their status",
		Example: "  tlm origins list\n  tlm origins list --json",
		Args:    noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			cl, org, project, err := a.project(ctx, "")
			if err != nil {
				return err
			}
			now := a.env.Now()
			return printList(ctx, a, cl, &l, api.ProjectPath(org, project)+"/origins", nil, func(os []api.Origin) string {
				rows := make([][]string, 0, len(os))
				for _, o := range os {
					rows = append(rows, []string{o.Origin, o.Status, render.Ago(o.CreatedAt, now)})
				}
				return render.Table([]string{"ORIGIN", "STATUS", "ADDED"}, rows)
			}, "No allowed domains yet. Browser events ask for theirs; or add one with: tlm origins allow <domain>")
		},
	}
	l.add(c)
	return c
}

func newOriginsPutCmd(a *app, status string) *cobra.Command {
	use, short := "allow", "Accept browser events from it (a bare host gets https://)"
	if status == "blocked" {
		use, short = "block", "Drop browser events from it"
	}
	return &cobra.Command{
		Use:   use + " <origin>",
		Short: short,
		Long:  originsLong,
		Example: "  tlm origins " + use + " shop.example.com\n" +
			"  tlm origins " + use + " http://localhost:5173\n" +
			"  tlm origins " + use + " '*.example.com'",
		Args: argsBetween(1, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			cl, org, project, err := a.project(ctx, "")
			if err != nil {
				return err
			}
			var o api.Origin
			raw, err := cl.Send(ctx, http.MethodPut, api.ProjectPath(org, project)+"/origins", nil,
				map[string]string{"origin": originArg(args[0]), "status": status}, &o)
			if err != nil {
				return err
			}
			if a.flags.json {
				return a.printJSON(raw)
			}
			a.out("%s: %s", render.Safe(o.Origin), render.Safe(o.Status))
			return nil
		},
	}
}

func newOriginsForgetCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "forget <origin>",
		Short: "Forget it: its next event asks again",
		Long: "Forget an allowed or blocked origin, so its next event waits for the\n" +
			"admins' answer again, as a new one does.",
		Example: "  tlm origins forget shop.example.com",
		Args:    argsBetween(1, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			cl, org, project, err := a.project(ctx, "")
			if err != nil {
				return err
			}
			origin := originArg(args[0])
			if _, err := cl.Send(ctx, http.MethodDelete, api.ProjectPath(org, project)+"/origins", api.Query{"origin": origin}, nil, nil); err != nil {
				return err
			}
			a.out("Forgot %s; its next event will ask again.", render.Safe(origin))
			return nil
		},
	}
}
