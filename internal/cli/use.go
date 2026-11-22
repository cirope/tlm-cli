package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/cirope/tlm-cli/internal/api"
	"github.com/cirope/tlm-cli/internal/auth"
	"github.com/cirope/tlm-cli/internal/render"
	"github.com/spf13/cobra"
)

func newUseCmd(a *app) *cobra.Command {
	var clear bool
	c := &cobra.Command{
		Use:   "use [<org>[/<project>]]",
		Short: "Set the organization and project commands use by default",
		Long: "Set the organization, and optionally the project, that commands use on\n" +
			"this Tlm when --org/--project and TLM_ORG/TLM_PROJECT\n" +
			"don't say. Without an argument, show it.",
		Example: "  tlm use acme/shop\n  tlm use acme\n  tlm use\n  tlm use --clear",
		Args:    argsBetween(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := a.resolve()
			if err != nil {
				return err
			}
			cfg, _, err := a.config()
			if err != nil {
				return err
			}
			if clear {
				if len(args) > 0 {
					return usageError{errors.New("--clear takes no argument")}
				}
				delete(cfg.Defaults, s.url)
				if err := cfg.Save(a.configPath()); err != nil {
					return err
				}
				a.out("No default on %s.", s.url)
				return nil
			}
			if len(args) == 0 {
				d, ok := cfg.Defaults[s.url]
				if !ok {
					a.out("No default on %s.", s.url)
					a.progress("Set one with: tlm use <org>[/<project>]")
					return nil
				}
				a.out("%s", render.Safe(defaultText(d)))
				return nil
			}
			org, project, _ := strings.Cut(args[0], "/")
			if org == "" || strings.Contains(project, "/") {
				return usageError{fmt.Errorf("%q: expected <org> or <org>/<project>", args[0])}
			}
			// Check it exists, and that this token can see it.
			c, _, err := a.client()
			if err != nil {
				return err
			}
			path := api.OrgPath(org)
			if project != "" {
				path = api.ProjectPath(org, project)
			}
			if _, err := c.Get(cmd.Context(), path, nil, nil); err != nil {
				return err
			}
			if cfg.Defaults == nil {
				cfg.Defaults = map[string]auth.Default{}
			}
			d := auth.Default{Org: org, Project: project}
			cfg.Defaults[s.url] = d
			if err := cfg.Save(a.configPath()); err != nil {
				return err
			}
			a.out("Commands on %s now use %s.", s.url, render.Safe(defaultText(d)))
			a.progress("--org and --project, and TLM_ORG and TLM_PROJECT, still win.")
			return nil
		},
	}
	c.Flags().BoolVar(&clear, "clear", false, "forget the default")
	return c
}

func defaultText(d auth.Default) string {
	if d.Project == "" {
		return d.Org
	}
	return d.Org + "/" + d.Project
}
